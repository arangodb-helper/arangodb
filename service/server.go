package service

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
)

type HelloRequest struct {
	SlaveID      string // Unique ID of the slave
	SlaveAddress string // IP address used to reach the slave (if empty, this will be derived from the request)
	SlavePort    int    // Port used to reach the slave
	DataDir      string // Directory used for data by this slave
	IsSecure     bool   // If set, servers started by this peer are using an SSL connection
}

type GoodbyeRequest struct {
	SlaveID string // Unique ID of the slave that should be removed.
}

type ProcessListResponse struct {
	ServersStarted bool            `json:"servers-started,omitempty"` // True if the server have all been started
	Servers        []ServerProcess `json:"servers,omitempty"`         // List of servers started by ArangoDB
}

type VersionResponse struct {
	Version string `json:"version"`
	Build   string `json:"build"`
}

type ServerProcess struct {
	Type        string `json:"type"`                   // agent | coordinator | dbserver
	IP          string `json:"ip"`                     // IP address needed to reach the server
	Port        int    `json:"port"`                   // Port needed to reach the server
	ProcessID   int    `json:"pid,omitempty"`          // PID of the process (0 when running in docker)
	ContainerID string `json:"container-id,omitempty"` // ID of docker container running the server
	ContainerIP string `json:"container-ip,omitempty"` // IP address of docker container running the server
	IsSecure    bool   `json:"is-secure,omitempty"`    // If set, this server is using an SSL connection
}

// startHTTPServer initializes and runs the HTTP server.
// If will return directly after starting it.
func (s *Service) startHTTPServer() {
	http.HandleFunc("/hello", s.helloHandler)
	http.HandleFunc("/goodbye", s.goodbyeHandler)
	http.HandleFunc("/process", s.processListHandler)
	http.HandleFunc("/logs/agent", s.agentLogsHandler)
	http.HandleFunc("/logs/dbserver", s.dbserverLogsHandler)
	http.HandleFunc("/logs/coordinator", s.coordinatorLogsHandler)
	http.HandleFunc("/version", s.versionHandler)
	http.HandleFunc("/shutdown", s.shutdownHandler)

	go func() {
		containerPort, hostPort, err := s.getHTTPServerPort()
		if err != nil {
			s.log.Fatalf("Failed to get HTTP port info: %#v", err)
		}
		addr := fmt.Sprintf("0.0.0.0:%d", containerPort)
		s.log.Infof("Listening on %s (%s)", addr, net.JoinHostPort(s.OwnAddress, strconv.Itoa(hostPort)))
		if err := http.ListenAndServe(addr, nil); err != nil {
			s.log.Errorf("Failed to listen on %s: %v", addr, err)
		}
	}()
}

// HTTP service function:

func (s *Service) helloHandler(w http.ResponseWriter, r *http.Request) {
	// Claim exclusive access to our data structures
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.log.Debugf("Received request from %s", r.RemoteAddr)
	if s.state == stateSlave {
		header := w.Header()
		if len(s.myPeers.Peers) > 0 {
			master := s.myPeers.Peers[0]
			header.Add("Location", master.CreateStarterURL("/hello"))
			w.WriteHeader(http.StatusTemporaryRedirect)
		} else {
			writeError(w, http.StatusBadRequest, "No master known.")
		}
		return
	}

	// Learn my own address (if needed)
	if len(s.myPeers.Peers) == 0 {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("Cannot derive own host address: %v", err))
			return
		}
		myself := normalizeHostName(host)
		_, hostPort, _ := s.getHTTPServerPort()
		s.myPeers.Peers = []Peer{
			Peer{
				ID:         s.ID,
				Address:    myself,
				Port:       hostPort,
				PortOffset: 0,
				DataDir:    s.DataDir,
				HasAgent:   true,
				IsSecure:   s.IsSecure(),
			},
		}
		s.log.Infof("Added master '%s': %s, portOffset: %d", s.myPeers.Peers[0].ID, s.myPeers.Peers[0].Address, s.myPeers.Peers[0].PortOffset)
		s.myPeers.AgencySize = s.AgencySize
	}

	if r.Method == "POST" {
		var req HelloRequest
		defer r.Body.Close()
		body, err := ioutil.ReadAll(r.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("Cannot read request body: %v", err.Error()))
			return
		}
		if err := json.Unmarshal(body, &req); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("Cannot parse request body: %v", err.Error()))
			return
		}

		slaveAddr := req.SlaveAddress
		if slaveAddr == "" {
			host, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				writeError(w, http.StatusBadRequest, "SlaveAddress must be set.")
				return
			}
			slaveAddr = normalizeHostName(host)
		} else {
			slaveAddr = normalizeHostName(slaveAddr)
		}
		slavePort := req.SlavePort

		// Check request
		if req.SlaveID == "" {
			writeError(w, http.StatusBadRequest, "SlaveID must be set.")
			return
		}

		// Check datadir
		if !s.allowSameDataDir {
			for _, p := range s.myPeers.Peers {
				if p.Address == slaveAddr && p.DataDir == req.DataDir && p.ID != req.SlaveID {
					writeError(w, http.StatusBadRequest, "Cannot use same directory as peer.")
					return
				}
			}
		}

		// Check IsSecure, cannot mix secure / non-secure
		if req.IsSecure != s.IsSecure() {
			writeError(w, http.StatusBadRequest, "Cannot mix secure / non-secure peers.")
			return
		}

		// If slaveID already known, then return data right away.
		_, idFound := s.myPeers.PeerByID(req.SlaveID)
		if idFound {
			// ID already found, update peer data
			for i, p := range s.myPeers.Peers {
				if p.ID == req.SlaveID {
					s.myPeers.Peers[i].Port = req.SlavePort
					if s.AllPortOffsetsUnique {
						s.myPeers.Peers[i].Address = slaveAddr
					} else {
						// Slave address may not change
						if p.Address != slaveAddr {
							writeError(w, http.StatusBadRequest, "Cannot change slave address while using an existing ID.")
							return
						}
					}
					s.myPeers.Peers[i].DataDir = req.DataDir
				}
			}
		} else {
			// ID not yet found, add it
			newPeer := Peer{
				ID:         req.SlaveID,
				Address:    slaveAddr,
				Port:       slavePort,
				PortOffset: s.myPeers.GetFreePortOffset(slaveAddr, s.AllPortOffsetsUnique),
				DataDir:    req.DataDir,
				HasAgent:   len(s.myPeers.Peers) < s.AgencySize,
				IsSecure:   req.IsSecure,
			}
			s.myPeers.Peers = append(s.myPeers.Peers, newPeer)
			s.log.Infof("Added new peer '%s': %s, portOffset: %d", newPeer.ID, newPeer.Address, newPeer.PortOffset)
			if len(s.myPeers.Peers) == s.AgencySize {
				s.startRunningTrigger()
			}
		}
	}
	b, err := json.Marshal(s.myPeers)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
	} else {
		w.Write(b)
	}
}

// goodbyeHandler handles a `/goodbye` request that removes a peer from the list of peers.
func (s *Service) goodbyeHandler(w http.ResponseWriter, r *http.Request) {
	// Claim exclusive access to our data structures
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if r.Method != "POST" {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	var req GoodbyeRequest
	defer r.Body.Close()
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Cannot read request body: %v", err.Error()))
		return
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Cannot parse request body: %v", err.Error()))
		return
	}

	// Check request
	if req.SlaveID == "" {
		writeError(w, http.StatusBadRequest, "SlaveID must be set.")
		return
	}

	// Remove the peer
	s.log.Infof("Removing peer %s", req.SlaveID)
	if removed := s.myPeers.RemovePeerByID(req.SlaveID); !removed {
		// ID not found
		writeError(w, http.StatusNotFound, "Unknown ID")
		return
	}

	// Peer has been removed, update stored config
	s.log.Info("Saving setup")
	if err := s.saveSetup(); err != nil {
		s.log.Errorf("Failed to save setup: %#v", err)
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("BYE"))
}

func (s *Service) processListHandler(w http.ResponseWriter, r *http.Request) {
	// Gather processes
	resp := ProcessListResponse{}
	expectedServers := 2
	myPeer, found := s.myPeers.PeerByID(s.ID)
	if found {
		portOffset := myPeer.PortOffset
		ip := myPeer.Address
		if myPeer.HasAgent {
			expectedServers = 3
		}

		createServerProcess := func(serverType ServerType, p Process) ServerProcess {
			return ServerProcess{
				Type:        serverType.String(),
				IP:          ip,
				Port:        s.MasterPort + portOffset + serverType.PortOffset(),
				ProcessID:   p.ProcessID(),
				ContainerID: p.ContainerID(),
				ContainerIP: p.ContainerIP(),
				IsSecure:    s.IsSecure(),
			}
		}

		if p := s.servers.agentProc; p != nil {
			resp.Servers = append(resp.Servers, createServerProcess(ServerTypeAgent, p))
		}
		if p := s.servers.coordinatorProc; p != nil {
			resp.Servers = append(resp.Servers, createServerProcess(ServerTypeCoordinator, p))
		}
		if p := s.servers.dbserverProc; p != nil {
			resp.Servers = append(resp.Servers, createServerProcess(ServerTypeDBServer, p))
		}
	}
	resp.ServersStarted = len(resp.Servers) == expectedServers
	b, err := json.Marshal(resp)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
	} else {
		w.Write(b)
	}
}

// agentLogsHandler servers the entire agent log (if any).
// If there is no agent running a 404 is returned.
func (s *Service) agentLogsHandler(w http.ResponseWriter, r *http.Request) {
	if s.needsAgent() {
		s.logsHandler(w, r, ServerTypeAgent)
	} else {
		w.WriteHeader(http.StatusNotFound)
	}
}

// dbserverLogsHandler servers the entire dbserver log.
func (s *Service) dbserverLogsHandler(w http.ResponseWriter, r *http.Request) {
	s.logsHandler(w, r, ServerTypeDBServer)
}

// coordinatorLogsHandler servers the entire coordinator log.
func (s *Service) coordinatorLogsHandler(w http.ResponseWriter, r *http.Request) {
	s.logsHandler(w, r, ServerTypeCoordinator)
}

func (s *Service) logsHandler(w http.ResponseWriter, r *http.Request, serverType ServerType) {
	// Find log path
	myHostDir, err := s.serverHostDir(serverType)
	if err != nil {
		// Not ready yet
		w.WriteHeader(http.StatusPreconditionFailed)
		return
	}
	logPath := filepath.Join(myHostDir, logFileName)
	s.log.Debugf("Fetching logs in %s", logPath)
	rd, err := os.Open(logPath)
	if os.IsNotExist(err) {
		// Log file not there (yet), we allow this
		w.WriteHeader(http.StatusOK)
	} else if err != nil {
		s.log.Errorf("Failed to open log file '%s': %#v", logPath, err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(err.Error()))
	} else {
		// Log open
		defer rd.Close()
		w.WriteHeader(http.StatusOK)
		io.Copy(w, rd)
	}
}

// versionHandler returns a JSON object containing the current version & build number.
func (s *Service) versionHandler(w http.ResponseWriter, r *http.Request) {
	v := VersionResponse{
		Version: s.ProjectVersion,
		Build:   s.ProjectBuild,
	}
	data, err := json.Marshal(v)
	if err != nil {
		s.log.Errorf("Failed to marshal version response: %#v", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(err.Error()))
	} else {
		w.WriteHeader(http.StatusOK)
		w.Write(data)
	}
}

// shutdownHandler initiates a shutdown of this process and all servers started by it.
func (s *Service) shutdownHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	if r.FormValue("mode") == "goodbye" {
		// Inform the master we're leaving for good
		if err := s.sendMasterGoodbye(); err != nil {
			s.log.Errorf("Failed to send master goodbye: %#v", err)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	// Stop my services
	s.cancel()
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func writeError(w http.ResponseWriter, status int, message string) {
	if message == "" {
		message = "Unknown error"
	}
	resp := ErrorResponse{Error: message}
	b, _ := json.Marshal(resp)
	w.WriteHeader(status)
	w.Write(b)
}

func (s *Service) getHTTPServerPort() (containerPort, hostPort int, err error) {
	containerPort = s.MasterPort
	hostPort = s.announcePort
	if s.announcePort == s.MasterPort && len(s.myPeers.Peers) > 0 {
		if myPeer, ok := s.myPeers.PeerByID(s.ID); ok {
			containerPort += myPeer.PortOffset
		} else {
			return 0, 0, maskAny(fmt.Errorf("No peer information found for ID '%s'", s.ID))
		}
	}
	if s.isNetHost {
		hostPort = containerPort
	}
	return containerPort, hostPort, nil
}
