package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"strconv"
	"time"
)

func (s *Service) startSlave(peerAddress string, runner Runner) {
	masterPort := s.MasterPort
	if host, port, err := net.SplitHostPort(peerAddress); err == nil {
		peerAddress = host
		masterPort, _ = strconv.Atoi(port)
	}
	for {
		s.log.Infof("Contacting master %s:%d...", peerAddress, masterPort)
		b, _ := json.Marshal(SlaveRequest{DataDir: s.DataDir})
		buf := bytes.Buffer{}
		buf.Write(b)
		r, e := http.Post(fmt.Sprintf("http://%s:%d/hello", peerAddress, masterPort), "application/json", &buf)
		if e != nil {
			s.log.Infof("Cannot start because of error from master: %v", e)
			time.Sleep(time.Second)
			continue
		}

		body, e := ioutil.ReadAll(r.Body)
		defer r.Body.Close()
		if e != nil {
			s.log.Infof("Cannot start because HTTP response from master was bad: %v", e)
			time.Sleep(time.Second)
			continue
		}

		if r.StatusCode != http.StatusOK {
			var errResp ErrorResponse
			json.Unmarshal(body, &errResp)
			s.log.Fatalf("Cannot start because of HTTP error from master: code=%d, message=%s\n", r.StatusCode, errResp.Error)
		}
		e = json.Unmarshal(body, &s.myPeers)
		if e != nil {
			s.log.Warningf("Cannot parse body from master: %v", e)
			return
		}
		s.myPeers.MyIndex = len(s.myPeers.Hosts) - 1
		s.AgencySize = s.myPeers.AgencySize
		break
	}

	// Run the HTTP service so we can forward other clients
	s.startHTTPServer()

	// Wait until we can start:
	if s.AgencySize > 1 {
		s.log.Infof("Waiting for %d servers to show up...", s.AgencySize)
	}
	for {
		if len(s.myPeers.Hosts) >= s.AgencySize {
			s.log.Info("Starting service...")
			s.saveSetup()
			s.startRunning(runner)
			return
		}
		time.Sleep(time.Second)
		r, _ := http.Get(fmt.Sprintf("http://%s:%d/hello", s.myPeers.MasterHostIP, s.myPeers.MasterPort))
		defer r.Body.Close()
		body, _ := ioutil.ReadAll(r.Body)
		var newPeers peers
		json.Unmarshal(body, &newPeers)
		s.myPeers.Hosts = newPeers.Hosts
		s.myPeers.PortOffsets = newPeers.PortOffsets
	}
}
