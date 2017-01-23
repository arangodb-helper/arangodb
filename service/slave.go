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
		fmt.Printf("Contacting master %s:%d...\n", peerAddress, masterPort)
		b, _ := json.Marshal(SlaveRequest{DataDir: s.DataDir})
		buf := bytes.Buffer{}
		buf.Write(b)
		r, e := http.Post(fmt.Sprintf("http://%s:%d/hello", peerAddress, masterPort), "application/json", &buf)
		if e != nil {
			fmt.Println("Cannot start because of error from master:", e)
			time.Sleep(time.Second)
			continue
		}

		body, e := ioutil.ReadAll(r.Body)
		defer r.Body.Close()
		if e != nil {
			fmt.Println("Cannot start because HTTP response from master was bad:", e)
			time.Sleep(time.Second)
			continue
		}

		if r.StatusCode != http.StatusOK {
			var errResp ErrorResponse
			json.Unmarshal(body, &errResp)
			fmt.Printf("Cannot start because of HTTP error from master: code=%d, message=%s\n", r.StatusCode, errResp.Error)
			return
		}
		e = json.Unmarshal(body, &s.myPeers)
		if e != nil {
			fmt.Println("Cannot parse body from master:", e)
			return
		}
		s.myPeers.MyIndex = len(s.myPeers.Hosts) - 1
		s.AgencySize = s.myPeers.AgencySize
		break
	}

	// HTTP service:
	http.HandleFunc("/hello", s.hello)
	go http.ListenAndServe("0.0.0.0:"+strconv.Itoa(s.MasterPort), nil)

	// Wait until we can start:
	if s.AgencySize > 1 {
		fmt.Println("Waiting for", s.AgencySize, "servers to show up...")
	}
	for {
		if len(s.myPeers.Hosts) >= s.AgencySize {
			fmt.Println("Starting service...")
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
