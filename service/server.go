package service

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
)

type SlaveRequest struct {
	SlaveAddress string // Address used to reach the slave (if empty, this will be derived from the request)
	SlavePort    int    // Port used to reach the slave
	DataDir      string // Directory used for data by this slave
}

type ProcessListResponse struct {
	Servers []ServerProcess `json:"servers,omitempty"` // List of servers started by ArangoDB
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

	go func() {
		portOffset := 0
		if len(s.myPeers.Peers) > 0 {
			portOffset = s.myPeers.Peers[s.myPeers.MyIndex].PortOffset
		}
		addr := fmt.Sprintf("0.0.0.0:%d", s.MasterPort+portOffset)
		s.log.Infof("Listening on %s", addr)
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
		s.myPeers.Peers = []Peer{
			Peer{
				Address:    myself,
				Port:       s.announcePort,
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
			PortOffset: len(s.myPeers.Peers),
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
	p := s.myPeers.Peers[s.myPeers.MyIndex]
	portOffset := p.PortOffset
	ip := p.Address
	if p := s.servers.agentProc; p != nil {
		resp.Servers = append(resp.Servers, ServerProcess{
			Type:        "agent",
			IP:          ip,
			Port:        basePortAgent + portOffset,
			ProcessID:   p.ProcessID(),
			ContainerID: p.ContainerID(),
		})
	}
	if p := s.servers.coordinatorProc; p != nil {
		resp.Servers = append(resp.Servers, ServerProcess{
			Type:        "coordinator",
			IP:          ip,
			Port:        basePortCoordinator + portOffset,
			ProcessID:   p.ProcessID(),
			ContainerID: p.ContainerID(),
		})
	}
	if p := s.servers.dbserverProc; p != nil {
		resp.Servers = append(resp.Servers, ServerProcess{
			Type:        "dbserver",
			IP:          ip,
			Port:        basePortDBServer + portOffset,
			ProcessID:   p.ProcessID(),
			ContainerID: p.ContainerID(),
		})
	}
	b, err := json.Marshal(resp)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
	} else {
		w.Write(b)
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
