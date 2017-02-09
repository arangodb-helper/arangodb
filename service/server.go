package service

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
)

type SlaveRequest struct {
	SlaveAddress string // Address used to reach the slave (if empty, this will be derived from the request)
	SlavePort    int    // Port used to reach the slave
	DataDir      string // Directory used for data by this slave
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
}

// startHTTPServer initializes and runs the HTTP server.
// If will return directly after starting it.
func (s *Service) startHTTPServer() {
	http.HandleFunc("/hello", s.helloHandler)
	http.HandleFunc("/process", s.processListHandler)
	http.HandleFunc("/logs/agent", s.agentLogsHandler)
	http.HandleFunc("/logs/dbserver", s.dbserverLogsHandler)
	http.HandleFunc("/logs/coordinator", s.coordinatorLogsHandler)
	http.HandleFunc("/version", s.versionHandler)
	http.HandleFunc("/shutdown", s.shutdownHandler)

	go func() {
		containerPort, hostPort := s.getHTTPServerPort()
		addr := fmt.Sprintf("0.0.0.0:%d", containerPort)
		s.log.Infof("Listening on %s (%s:%d)", addr, s.OwnAddress, hostPort)
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
			header.Add("Location", fmt.Sprintf("http://%s:%d/hello", master.Address, master.Port))
			w.WriteHeader(http.StatusTemporaryRedirect)
		} else {
			writeError(w, http.StatusBadRequest, "No master known.")
		}
		return
	}

	// Learn my own address (if needed)
	if len(s.myPeers.Peers) == 0 {
		myself := findHost(r.Host)
		_, hostPort := s.getHTTPServerPort()
		s.myPeers.Peers = []Peer{
			Peer{
				Address:    myself,
				Port:       hostPort,
				PortOffset: 0,
				DataDir:    s.DataDir,
			},
		}
		s.myPeers.AgencySize = s.AgencySize
		s.myPeers.MyIndex = 0
	}

	if r.Method == "POST" {
		var req SlaveRequest
		defer r.Body.Close()
		body, _ := ioutil.ReadAll(r.Body)
		json.Unmarshal(body, &req)

		slaveAddr := req.SlaveAddress
		if slaveAddr == "" {
			slaveAddr = findHost(r.RemoteAddr)
		}
		slavePort := req.SlavePort

		if !s.allowSameDataDir {
			for _, p := range s.myPeers.Peers {
				if p.Address == slaveAddr && p.DataDir == req.DataDir {
					writeError(w, http.StatusBadRequest, "Cannot use same directory as peer.")
					return
				}
			}
		}

		newPeer := Peer{
			Address:    slaveAddr,
			Port:       slavePort,
			PortOffset: len(s.myPeers.Peers) * portOffsetIncrement,
			DataDir:    req.DataDir,
		}
		s.myPeers.Peers = append(s.myPeers.Peers, newPeer)
		s.log.Infof("New peer: %s, portOffset: %d", newPeer.Address, newPeer.PortOffset)
		if len(s.myPeers.Peers) == s.AgencySize {
			s.starter <- true
		}
	}
	b, err := json.Marshal(s.myPeers)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
	} else {
		w.Write(b)
	}
}

func (s *Service) processListHandler(w http.ResponseWriter, r *http.Request) {
	// Gather processes
	resp := ProcessListResponse{}
	peers := s.myPeers.Peers
	index := s.myPeers.MyIndex
	if index < len(peers) {
		p := peers[index]
		portOffset := p.PortOffset
		ip := p.Address
		if p := s.servers.agentProc; p != nil {
			resp.Servers = append(resp.Servers, ServerProcess{
				Type:        "agent",
				IP:          ip,
				Port:        s.MasterPort + portOffset + portOffsetAgent,
				ProcessID:   p.ProcessID(),
				ContainerID: p.ContainerID(),
			})
		}
		if p := s.servers.coordinatorProc; p != nil {
			resp.Servers = append(resp.Servers, ServerProcess{
				Type:        "coordinator",
				IP:          ip,
				Port:        s.MasterPort + portOffset + portOffsetCoordinator,
				ProcessID:   p.ProcessID(),
				ContainerID: p.ContainerID(),
			})
		}
		if p := s.servers.dbserverProc; p != nil {
			resp.Servers = append(resp.Servers, ServerProcess{
				Type:        "dbserver",
				IP:          ip,
				Port:        s.MasterPort + portOffset + portOffsetDBServer,
				ProcessID:   p.ProcessID(),
				ContainerID: p.ContainerID(),
			})
		}
	}
	expectedServers := 2
	if s.needsAgent() {
		expectedServers = 3
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
		s.logsHandler(w, r, "agent", portOffsetAgent)
	} else {
		w.WriteHeader(http.StatusNotFound)
	}
}

// dbserverLogsHandler servers the entire dbserver log.
func (s *Service) dbserverLogsHandler(w http.ResponseWriter, r *http.Request) {
	s.logsHandler(w, r, "dbserver", portOffsetDBServer)
}

// coordinatorLogsHandler servers the entire coordinator log.
func (s *Service) coordinatorLogsHandler(w http.ResponseWriter, r *http.Request) {
	s.logsHandler(w, r, "coordinator", portOffsetCoordinator)
}

func (s *Service) logsHandler(w http.ResponseWriter, r *http.Request, mode string, serverPortOffset int) {
	if s.myPeers.MyIndex < 0 || s.myPeers.MyIndex >= len(s.myPeers.Peers) {
		// Not ready yet
		w.WriteHeader(http.StatusPreconditionFailed)
		return
	}
	// Find log path
	portOffset := s.myPeers.Peers[s.myPeers.MyIndex].PortOffset
	myPort := s.MasterPort + portOffset + serverPortOffset
	logPath := filepath.Join(s.DataDir, fmt.Sprintf("%s%d", mode, myPort), "arangod.log")
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
	} else {
		s.cancel()
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}
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

func (s *Service) getHTTPServerPort() (containerPort, hostPort int) {
	containerPort = s.MasterPort
	hostPort = s.announcePort
	if s.announcePort == s.MasterPort && len(s.myPeers.Peers) > 0 {
		containerPort += s.myPeers.Peers[s.myPeers.MyIndex].PortOffset
	}
	if s.isNetHost {
		hostPort = containerPort
	}
	return containerPort, hostPort
}
