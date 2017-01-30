package service

import (
	"fmt"
	"strconv"
	"time"
)

func (s *Service) startMaster(runner Runner) {
	// Start HTTP listener
	s.startHTTPServer()

	// Permanent loop:
	s.log.Infof("Serving as master on %s:%d...", s.OwnAddress, s.announcePort)

	if s.AgencySize == 1 {
		s.myPeers.Peers = []Peer{
			Peer{
				Address:    s.OwnAddress,
				Port:       s.announcePort,
				PortOffset: 0,
				DataDir:    s.DataDir,
			},
		}
		s.myPeers.AgencySize = s.AgencySize
		s.myPeers.MyIndex = 0
		s.saveSetup()
		s.log.Info("Starting service...")
		s.startRunning(runner)
		return
	}
	s.log.Infof("Waiting for %d servers to show up.\n", s.AgencySize)
	s.log.Infof("Use the following commands to start other servers:")
	fmt.Println()
	for index := 2; index <= s.AgencySize; index++ {
		port := ""
		if s.announcePort != s.MasterPort {
			port = strconv.Itoa(s.announcePort)
		}
		fmt.Println(runner.CreateStartArangodbCommand(index, s.OwnAddress, port))
		fmt.Println()
	}
	for {
		time.Sleep(time.Second)
		select {
		case <-s.starter:
			s.saveSetup()
			s.log.Info("Starting service...")
			s.startRunning(runner)
			return
		default:
		}
		if s.stop {
			break
		}
	}
}
