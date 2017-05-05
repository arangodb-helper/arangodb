//
// DISCLAIMER
//
// Copyright 2017 ArangoDB GmbH, Cologne, Germany
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// Copyright holder is ArangoDB GmbH, Cologne, Germany
//
// Author Ewout Prangsma
//

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
	// Check HTTP server port
	containerHTTPPort, _, err := s.getHTTPServerPort()
	if err != nil {
		s.log.Fatalf("Cannot find HTTP server info: %#v", err)
	}
	if !IsPortOpen(containerHTTPPort) {
		s.log.Fatalf("Port %d is already in use", containerHTTPPort)
	}

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
				HasAgent:   !s.isSingleMode(),
				IsSecure:   s.IsSecure(),
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
