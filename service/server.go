//
// DISCLAIMER
//
// Copyright 2017 ArangoDB GmbH, Cologne, Germany
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// Copyright holder is ArangoDB GmbH, Cologne, Germany
//
// Author Ewout Prangsma
//

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

	"github.com/arangodb-helper/arangodb/client"
)

var (
	httpClient = client.DefaultHTTPClient()
)

// HelloRequest is the data structure send of the wire in a `/hello` POST request.
type HelloRequest struct {
	SlaveID      string // Unique ID of the slave
	SlaveAddress string // IP address used to reach the slave (if empty, this will be derived from the request)
	SlavePort    int    // Port used to reach the slave
	DataDir      string // Directory used for data by this slave
	IsSecure     bool   // If set, servers started by this peer are using an SSL connection
	Agent        *bool  `json:",omitempty"` // If not nil, sets if server gets an agent or not. If nil, default handling applies
	DBServer     *bool  `json:",omitempty"` // If not nil, sets if server gets an dbserver or not. If nil, default handling applies
	Coordinator  *bool  `json:",omitempty"` // If not nil, sets if server gets an coordinator or not. If nil, default handling applies
}

type GoodbyeRequest struct {
	SlaveID string // Unique ID of the slave that should be removed.
}

// startHTTPServer initializes and runs the HTTP server.
// If will return directly after starting it.
func (s *Service) startHTTPServer(config Config) {
	mux := http.NewServeMux()
	// Starter to starter API
	mux.HandleFunc("/hello", s.helloHandler)
	mux.HandleFunc("/goodbye", s.goodbyeHandler)
	// External API
	mux.HandleFunc("/process", s.processListHandler)
	mux.HandleFunc("/logs/agent", s.agentLogsHandler)
	mux.HandleFunc("/logs/dbserver", s.dbserverLogsHandler)
	mux.HandleFunc("/logs/coordinator", s.coordinatorLogsHandler)
	mux.HandleFunc("/logs/single", s.singleLogsHandler)
	mux.HandleFunc("/version", s.versionHandler)
	mux.HandleFunc("/shutdown", s.shutdownHandler)

	go func() {
		containerPort, hostPort, err := s.getHTTPServerPort()
		if err != nil {
			s.log.Fatalf("Failed to get HTTP port info: %#v", err)
		}
		addr := fmt.Sprintf("0.0.0.0:%d", containerPort)
		server := &http.Server{
			Addr:    addr,
			Handler: mux,
		}
		if s.tlsConfig != nil {
			s.log.Infof("Listening on %s (%s) using TLS", addr, net.JoinHostPort(config.OwnAddress, strconv.Itoa(hostPort)))
			server.TLSConfig = s.tlsConfig
			if err := server.ListenAndServeTLS("", ""); err != nil {
				s.log.Errorf("Failed to listen on %s: %v", addr, err)
			}
		} else {
			s.log.Infof("Listening on %s (%s)", addr, net.JoinHostPort(config.OwnAddress, strconv.Itoa(hostPort)))
			if err := server.ListenAndServe(); err != nil {
				s.log.Errorf("Failed to listen on %s: %v", addr, err)
			}
		}
	}()
}

// HTTP service function:

func (s *Service) helloHandler(w http.ResponseWriter, r *http.Request) {
	// Claim exclusive access to our data structures
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.log.Debugf("Received request from %s", r.RemoteAddr)
	if s.state == stateBootstrapSlave {
		// Redirect to bootstrap master
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
	if s.learnOwnAddress {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("Cannot derive own host address: %v", err))
			return
		}
		myself := normalizeHostName(host)
		_, hostPort, _ := s.getHTTPServerPort()
		myPeer, found := s.myPeers.PeerByID(s.id)
		if found {
			myPeer.Address = myself
			myPeer.Port = hostPort
			s.myPeers.UpdatePeerByID(myPeer)
			s.learnOwnAddress = false
		}
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
					if s.cfg.AllPortOffsetsUnique {
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
			// In single server mode, do not accept new slaves
			if s.mode.IsSingleMode() {
				writeError(w, http.StatusBadRequest, "In single server mode, slaves cannot be added.")
				return
			}
			// ID not yet found, add it
			portOffset := s.myPeers.GetFreePortOffset(slaveAddr, s.cfg.AllPortOffsetsUnique)
			hasAgent := s.mode.IsClusterMode() && !s.myPeers.HaveEnoughAgents()
			if req.Agent != nil {
				hasAgent = *req.Agent
			}
			hasDBServer := true
			if req.DBServer != nil {
				hasDBServer = *req.DBServer
			}
			hasCoordinator := true
			if req.Coordinator != nil {
				hasCoordinator = *req.Coordinator
			}
			newPeer := NewPeer(req.SlaveID, slaveAddr, slavePort, portOffset, req.DataDir, hasAgent, hasDBServer, hasCoordinator, req.IsSecure)
			s.myPeers.Peers = append(s.myPeers.Peers, newPeer)
			s.log.Infof("Added new peer '%s': %s, portOffset: %d", newPeer.ID, newPeer.Address, newPeer.PortOffset)
		}

		// Start the running the servers if we have enough agents
		if s.myPeers.HaveEnoughAgents() {
			// Save updated configuration
			s.saveSetup()
			// Trigger start running (if needed)
			s.bootstrapCompleted.trigger()
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

	// Check method
	if r.Method != "POST" {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}

	// Check running state
	if !s.state.IsRunning() {
		writeError(w, http.StatusServiceUnavailable, "Have not reached running state yet")
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
	// Claim exclusive access to our data structures
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Gather processes
	resp := client.ProcessList{}
	expectedServers := 0
	myPeer, found := s.myPeers.PeerByID(s.id)
	if found {
		portOffset := myPeer.PortOffset
		ip := myPeer.Address
		if myPeer.HasAgent() {
			expectedServers++
		}
		if myPeer.HasDBServer() {
			expectedServers++
		}
		if myPeer.HasCoordinator() {
			expectedServers++
		}

		createServerProcess := func(serverType ServerType, p Process) client.ServerProcess {
			return client.ServerProcess{
				Type:        client.ServerType(serverType),
				IP:          ip,
				Port:        s.cfg.MasterPort + portOffset + serverType.PortOffset(),
				ProcessID:   p.ProcessID(),
				ContainerID: p.ContainerID(),
				ContainerIP: p.ContainerIP(),
				IsSecure:    s.IsSecure(),
			}
		}

		if p := s.runtimeServerManager.agentProc; p != nil {
			resp.Servers = append(resp.Servers, createServerProcess(ServerTypeAgent, p))
		}
		if p := s.runtimeServerManager.coordinatorProc; p != nil {
			resp.Servers = append(resp.Servers, createServerProcess(ServerTypeCoordinator, p))
		}
		if p := s.runtimeServerManager.dbserverProc; p != nil {
			resp.Servers = append(resp.Servers, createServerProcess(ServerTypeDBServer, p))
		}
		if p := s.runtimeServerManager.singleProc; p != nil {
			resp.Servers = append(resp.Servers, createServerProcess(ServerTypeSingle, p))
		}
	}
	if s.mode.IsSingleMode() {
		expectedServers = 1
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
	s.mutex.Lock()
	myPeer, found := s.myPeers.PeerByID(s.id)
	s.mutex.Unlock()

	if found && myPeer.HasAgent() {
		s.logsHandler(w, r, ServerTypeAgent)
	} else {
		w.WriteHeader(http.StatusNotFound)
	}
}

// dbserverLogsHandler servers the entire dbserver log.
func (s *Service) dbserverLogsHandler(w http.ResponseWriter, r *http.Request) {
	s.mutex.Lock()
	myPeer, found := s.myPeers.PeerByID(s.id)
	s.mutex.Unlock()

	if found && myPeer.HasDBServer() {
		s.logsHandler(w, r, ServerTypeDBServer)
	} else {
		w.WriteHeader(http.StatusNotFound)
	}
}

// coordinatorLogsHandler servers the entire coordinator log.
func (s *Service) coordinatorLogsHandler(w http.ResponseWriter, r *http.Request) {
	s.mutex.Lock()
	myPeer, found := s.myPeers.PeerByID(s.id)
	s.mutex.Unlock()

	if found && myPeer.HasCoordinator() {
		s.logsHandler(w, r, ServerTypeCoordinator)
	} else {
		w.WriteHeader(http.StatusNotFound)
	}
}

// singleLogsHandler servers the entire single server log.
func (s *Service) singleLogsHandler(w http.ResponseWriter, r *http.Request) {
	s.logsHandler(w, r, ServerTypeSingle)
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
	v := client.VersionInfo{
		Version: s.cfg.ProjectVersion,
		Build:   s.cfg.ProjectBuild,
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
	s.Stop()
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
	containerPort = s.cfg.MasterPort
	hostPort = s.announcePort
	if s.announcePort == s.cfg.MasterPort && len(s.myPeers.Peers) > 0 {
		if myPeer, ok := s.myPeers.PeerByID(s.id); ok {
			containerPort += myPeer.PortOffset
		} else {
			return 0, 0, maskAny(fmt.Errorf("No peer information found for ID '%s'", s.id))
		}
	}
	if s.isNetHost {
		hostPort = containerPort
	}
	return containerPort, hostPort, nil
}
