package service

import "time"

func (s *Service) startMaster(runner Runner) {
	// Start HTTP listener
	s.startHTTPServer()

	// Permanent loop:
	s.log.Infof("Serving as master on %s:%d...", s.myPeers.MasterHostIP, s.myPeers.MasterPort)

	if s.AgencySize == 1 {
		s.myPeers.Hosts = append(s.myPeers.Hosts, s.OwnAddress)
		s.myPeers.PortOffsets = append(s.myPeers.PortOffsets, 0)
		s.myPeers.Directories = append(s.myPeers.Directories, s.DataDir)
		s.myPeers.AgencySize = s.AgencySize
		s.myPeers.MyIndex = 0
		s.saveSetup()
		s.log.Info("Starting service...")
		s.startRunning(runner)
		return
	}
	s.log.Infof("Waiting for %d servers to show up.\n", s.AgencySize)
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
