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
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"path/filepath"

	"github.com/arangodb-helper/arangodb/client"
	logging "github.com/op/go-logging"
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

type httpServer struct {
	//config Config
	log                  *logging.Logger
	context              httpServerContext
	versionInfo          client.VersionInfo
	runtimeServerManager *runtimeServerManager
	masterPort           int
}

type statusCallback func(msg string)

// httpServerContext provides a context for the httpServer.
type httpServerContext interface {
	// ClusterConfig returns the current cluster configuration and the current peer
	ClusterConfig() (ClusterConfig, *Peer, ServiceMode)

	// removePeerByID alters the cluster configuration, removing the peer with given id.
	removePeerByID(id string) (peerRemoved bool, err error)

	// serverHostDir returns the path of the folder (in host namespace) containing data for the given server.
	serverHostDir(serverType ServerType) (string, error)

	// sendMasterGoodbye informs the master that we're leaving for good.
	sendMasterGoodbye() error

	// Stop the peer
	Stop()

	// Handle a hello request.
	// If req==nil, this is a GET request, otherwise it is a POST request.
	HandleHello(ownAddress, remoteAddress string, req *HelloRequest, serviceNotAvailable, redirectTo, badRequest, internalError statusCallback) ClusterConfig
}

// newHTTPServer initializes and an HTTP server.
func newHTTPServer(log *logging.Logger, context httpServerContext, runtimeServerManager *runtimeServerManager, config Config) *httpServer {
	// Create HTTP server
	return &httpServer{
		log:     log,
		context: context,
		versionInfo: client.VersionInfo{
			Version: config.ProjectVersion,
			Build:   config.ProjectBuild,
		},
		runtimeServerManager: runtimeServerManager,
		masterPort:           config.MasterPort,
	}
}

// Start listening for requests.
// This method will return directly after starting.
func (s *httpServer) Start(hostAddr, containerAddr string, tlsConfig *tls.Config) {
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
		server := &http.Server{
			Addr:    containerAddr,
			Handler: mux,
		}
		if tlsConfig != nil {
			s.log.Infof("Listening on %s (%s) using TLS", containerAddr, hostAddr)
			server.TLSConfig = tlsConfig
			if err := server.ListenAndServeTLS("", ""); err != nil {
				s.log.Errorf("Failed to listen on %s: %v", containerAddr, err)
			}
		} else {
			s.log.Infof("Listening on %s (%s)", containerAddr, hostAddr)
			if err := server.ListenAndServe(); err != nil {
				s.log.Errorf("Failed to listen on %s: %v", containerAddr, err)
			}
		}
	}()
}

// HTTP service function:

func (s *httpServer) helloHandler(w http.ResponseWriter, r *http.Request) {
	s.log.Debugf("Received request from %s", r.RemoteAddr)

	// Derive own address
	host, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Cannot derive own host address: %v", err))
		return
	}
	ownAddress := normalizeHostName(host)

	// Prepare callbacks
	didRespond := false
	serviceNotAvailable := func(msg string) {
		writeError(w, http.StatusServiceUnavailable, msg)
		didRespond = true
	}
	redirectTo := func(url string) {
		header := w.Header()
		header.Add("Location", url)
		w.WriteHeader(http.StatusTemporaryRedirect)
		didRespond = true
	}
	badRequest := func(msg string) {
		writeError(w, http.StatusBadRequest, msg)
		didRespond = true
	}
	internalError := func(msg string) {
		writeError(w, http.StatusInternalServerError, msg)
		didRespond = true
	}

	var result ClusterConfig
	if r.Method == "GET" {
		// Let service handle get request
		result = s.context.HandleHello(ownAddress, r.RemoteAddr, nil, serviceNotAvailable, redirectTo, badRequest, internalError)
	} else if r.Method == "POST" {
		// Read request
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

		// Let service handle post request
		result = s.context.HandleHello(ownAddress, r.RemoteAddr, &req, serviceNotAvailable, redirectTo, badRequest, internalError)
	} else {
		// Invalid method
		writeError(w, http.StatusMethodNotAllowed, "GET or POST required")
		return
	}

	// Send result (if no error has been send before)
	if !didRespond {
		b, err := json.Marshal(result)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
		} else {
			w.Write(b)
		}
	}
}

// goodbyeHandler handles a `/goodbye` request that removes a peer from the list of peers.
func (s *httpServer) goodbyeHandler(w http.ResponseWriter, r *http.Request) {
	// Check method
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
	if removed, err := s.context.removePeerByID(req.SlaveID); err != nil {
		// Failure
		writeError(w, http.StatusServiceUnavailable, err.Error())
	} else if !removed {
		// ID not found
		writeError(w, http.StatusNotFound, "Unknown ID")
	} else {
		// Peer removed
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("BYE"))
	}
}

func (s *httpServer) processListHandler(w http.ResponseWriter, r *http.Request) {
	clusterConfig, myPeer, mode := s.context.ClusterConfig()
	isSecure := clusterConfig.IsSecure()

	// Gather processes
	resp := client.ProcessList{}
	expectedServers := 0
	if myPeer != nil {
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
				Port:        s.masterPort + portOffset + serverType.PortOffset(),
				ProcessID:   p.ProcessID(),
				ContainerID: p.ContainerID(),
				ContainerIP: p.ContainerIP(),
				IsSecure:    isSecure,
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
	if mode.IsSingleMode() {
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
func (s *httpServer) agentLogsHandler(w http.ResponseWriter, r *http.Request) {
	_, myPeer, _ := s.context.ClusterConfig()

	if myPeer != nil && myPeer.HasAgent() {
		s.logsHandler(w, r, ServerTypeAgent)
	} else {
		w.WriteHeader(http.StatusNotFound)
	}
}

// dbserverLogsHandler servers the entire dbserver log.
func (s *httpServer) dbserverLogsHandler(w http.ResponseWriter, r *http.Request) {
	_, myPeer, _ := s.context.ClusterConfig()

	if myPeer != nil && myPeer.HasDBServer() {
		s.logsHandler(w, r, ServerTypeDBServer)
	} else {
		w.WriteHeader(http.StatusNotFound)
	}
}

// coordinatorLogsHandler servers the entire coordinator log.
func (s *httpServer) coordinatorLogsHandler(w http.ResponseWriter, r *http.Request) {
	_, myPeer, _ := s.context.ClusterConfig()

	if myPeer != nil && myPeer.HasCoordinator() {
		s.logsHandler(w, r, ServerTypeCoordinator)
	} else {
		w.WriteHeader(http.StatusNotFound)
	}
}

// singleLogsHandler servers the entire single server log.
func (s *httpServer) singleLogsHandler(w http.ResponseWriter, r *http.Request) {
	s.logsHandler(w, r, ServerTypeSingle)
}

func (s *httpServer) logsHandler(w http.ResponseWriter, r *http.Request, serverType ServerType) {
	// Find log path
	myHostDir, err := s.context.serverHostDir(serverType)
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
func (s *httpServer) versionHandler(w http.ResponseWriter, r *http.Request) {
	data, err := json.Marshal(s.versionInfo)
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
func (s *httpServer) shutdownHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	if r.FormValue("mode") == "goodbye" {
		// Inform the master we're leaving for good
		if err := s.context.sendMasterGoodbye(); err != nil {
			s.log.Errorf("Failed to send master goodbye: %#v", err)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	// Stop my services
	s.context.Stop()
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
