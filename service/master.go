package service

import (
	"fmt"
	"net/http"
	"strconv"
	"time"
)

func (s *Service) startMaster(runner Runner) {
	// HTTP service:
	http.HandleFunc("/hello", s.hello)
	go http.ListenAndServe("0.0.0.0:"+strconv.Itoa(s.MasterPort), nil)
	// Permanent loop:
	fmt.Println("Serving as master...")
	if s.AgencySize == 1 {
		s.myPeers.Hosts = append(s.myPeers.Hosts, s.OwnAddress)
		s.myPeers.PortOffsets = append(s.myPeers.PortOffsets, 0)
		s.myPeers.Directories = append(s.myPeers.Directories, s.DataDir)
		s.myPeers.AgencySize = s.AgencySize
		s.myPeers.MyIndex = 0
		s.saveSetup()
		fmt.Println("Starting service...")
		s.startRunning(runner)
		return
	}
	fmt.Println("Waiting for", s.AgencySize, "servers to show up.")
	for {
		time.Sleep(time.Second)
		select {
		case <-s.starter:
			s.saveSetup()
			fmt.Println("Starting service...")
			s.startRunning(runner)
			return
		default:
		}
		if s.stop {
			break
		}
	}
}
