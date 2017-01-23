package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"time"
)

func (s *Service) startSlave(peerAddress string, runner Runner) {
	fmt.Println("Contacting master", peerAddress, "...")
	b, _ := json.Marshal(peers{Directories: []string{s.DataDir}})
	buf := bytes.Buffer{}
	buf.Write(b)
	r, e := http.Post(fmt.Sprintf("http://%s:%d/hello", peerAddress, s.MasterPort), "application/json", &buf)
	if e != nil {
		fmt.Println("Cannot start because of error from master:", e)
		return
	}

	body, e := ioutil.ReadAll(r.Body)
	defer r.Body.Close()
	if e != nil {
		fmt.Println("Cannot start because HTTP response from master was bad:", e)
		return
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
		time.Sleep(1000000000)
		r, e = http.Get(fmt.Sprintf("http://%s:%d/hello", s.myPeers.Hosts[0], s.MasterPort))
		body, e = ioutil.ReadAll(r.Body)
		r.Body.Close()
		var newPeers peers
		json.Unmarshal(body, &newPeers)
		s.myPeers.Hosts = newPeers.Hosts
		s.myPeers.PortOffsets = newPeers.PortOffsets
	}
}
