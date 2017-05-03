package service

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	logging "github.com/op/go-logging"
)

// startMaster starts the Service as master.
func (s *Service) startMaster(runner Runner) {
	// Start HTTP listener
	s.startHTTPServer()

	// Permanent loop:
	s.log.Infof("Serving as master with ID '%s' on %s:%d...", s.ID, s.OwnAddress, s.announcePort)

	if s.AgencySize == 1 {
		s.myPeers.Peers = []Peer{
			Peer{
				ID:         s.ID,
				Address:    s.OwnAddress,
				Port:       s.announcePort,
				PortOffset: 0,
				DataDir:    s.DataDir,
				HasAgent:   true,
			},
		}
		s.myPeers.AgencySize = s.AgencySize
		s.saveSetup()
		s.log.Info("Starting service...")
		s.startRunning(runner)
		return
	}

	wg := sync.WaitGroup{}
	if s.StartLocalSlaves {
		// Start additional local slaves
		s.createAndStartLocalSlaves(&wg)
	} else {
		// Show commands needed to start slaves
		s.log.Infof("Waiting for %d servers to show up.\n", s.AgencySize)
		s.showSlaveStartCommands(runner)
	}

	for {
		time.Sleep(time.Second)
		select {
		case <-s.startRunningWaiter.Done():
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
	// Wait for any local slaves to return.
	wg.Wait()
}

// showSlaveStartCommands prints out the commands needed to start additional slaves.
func (s *Service) showSlaveStartCommands(runner Runner) {
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
}

// mustCreateIDLogger creates a logger that includes the given ID in each log line.
func (s *Service) mustCreateIDLogger(id string) *logging.Logger {
	backend := logging.NewLogBackend(os.Stderr, "", log.LstdFlags)
	formattedBackend := logging.NewBackendFormatter(backend, logging.MustStringFormatter(fmt.Sprintf("[%s] %%{message}", id)))
	log := logging.MustGetLogger(s.log.Module)
	log.SetBackend(logging.AddModuleLevel(formattedBackend))
	return log
}
