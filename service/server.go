package service

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strconv"
)

// HTTP service function:

func (s *Service) hello(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("Received request from %s\n", r.RemoteAddr)
	if s.state == stateSlave {
		header := w.Header()
		if len(s.myPeers.Hosts) > 0 {
			header.Add("Location", fmt.Sprintf("http://%s:%d/hello", s.myPeers.Hosts[0], s.MasterPort))
			w.WriteHeader(http.StatusTemporaryRedirect)
		} else {
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `{"error": "No master known."}`)
		}
		return
	}
	if len(s.myPeers.Hosts) == 0 {
		myself := findHost(r.Host)
		s.myPeers.Hosts = append(s.myPeers.Hosts, myself)
		s.myPeers.PortOffsets = append(s.myPeers.PortOffsets, 0)
		s.myPeers.Directories = append(s.myPeers.Directories, s.DataDir)
		s.myPeers.AgencySize = s.AgencySize
		s.myPeers.MyIndex = 0
	}
	if r.Method == "POST" {
		var newPeer peers
		defer r.Body.Close()
		body, _ := ioutil.ReadAll(r.Body)
		json.Unmarshal(body, &newPeer)
		peerDir := newPeer.Directories[0]

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
			s.myPeers.Directories = append(s.myPeers.Directories, newPeer.Directories[0])
		}
		fmt.Println("New peer:", newGuy+", portOffset: "+strconv.Itoa(s.myPeers.PortOffsets[len(s.myPeers.PortOffsets)-1]))
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
