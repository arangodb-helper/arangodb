package service

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
)

type SlaveRequest struct {
	DataDir string
}

// startHTTPServer initializes and runs the HTTP server.
// If will return directly after starting it.
func (s *Service) startHTTPServer() {
	http.HandleFunc("/hello", s.hello)
	go func() {
		portOffset := 0
		if len(s.myPeers.PortOffsets) > 0 {
			portOffset = s.myPeers.PortOffsets[s.myPeers.MyIndex]
		}
		addr := fmt.Sprintf("0.0.0.0:%d", s.MasterPort+portOffset)
		s.log.Infof("Listening on %s", addr)
		if err := http.ListenAndServe(addr, nil); err != nil {
			s.log.Errorf("Failed to listen on %s: %v", addr, err)
		}
	}()
}

// HTTP service function:

func (s *Service) hello(w http.ResponseWriter, r *http.Request) {
	s.log.Debugf("Received request from %s", r.RemoteAddr)
	if s.state == stateSlave {
		header := w.Header()
		if len(s.myPeers.Hosts) > 0 {
			header.Add("Location", fmt.Sprintf("http://%s:%d/hello", s.myPeers.MasterHostIP, s.myPeers.MasterPort))
			w.WriteHeader(http.StatusTemporaryRedirect)
		} else {
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `{"error": "No master known."}`)
		}
		return
	}
	if len(s.myPeers.Hosts) == 0 {
		// Learn my own address
		myself := findHost(r.Host)
		s.myPeers.Hosts = append(s.myPeers.Hosts, myself)
		s.myPeers.PortOffsets = append(s.myPeers.PortOffsets, 0)
		s.myPeers.Directories = append(s.myPeers.Directories, s.DataDir)
		s.myPeers.AgencySize = s.AgencySize
		s.myPeers.MyIndex = 0
		s.myPeers.MasterHostIP = myself
	}
	if r.Method == "POST" {
		var req SlaveRequest
		defer r.Body.Close()
		body, _ := ioutil.ReadAll(r.Body)
		json.Unmarshal(body, &req)
		peerDir := req.DataDir

		newGuy := findHost(r.RemoteAddr)
		found := false
		for i := len(s.myPeers.Hosts) - 1; i >= 0; i-- {
			if s.myPeers.Hosts[i] == newGuy {
				if s.myPeers.Directories[i] == peerDir {
					w.WriteHeader(http.StatusBadRequest)
					io.WriteString(w, `{"error": "Cannot use same directory as peer."}`)
					return
				}
				s.myPeers.PortOffsets = append(s.myPeers.PortOffsets, s.myPeers.PortOffsets[i]+1)
				s.myPeers.Directories = append(s.myPeers.Directories, peerDir)
				found = true
				break
			}
		}
		s.myPeers.Hosts = append(s.myPeers.Hosts, newGuy)
		if !found {
			s.myPeers.PortOffsets = append(s.myPeers.PortOffsets, 0)
			s.myPeers.Directories = append(s.myPeers.Directories, peerDir)
		}
		s.log.Infof("New peer: %s, portOffset: %d", newGuy, s.myPeers.PortOffsets[len(s.myPeers.PortOffsets)-1])
		if len(s.myPeers.Hosts) == s.AgencySize {
			s.starter <- true
		}
	}
	b, e := json.Marshal(s.myPeers)
	if e != nil {
		io.WriteString(w, "Hello world! Your address is:"+r.RemoteAddr)
	} else {
		w.Write(b)
	}
}
