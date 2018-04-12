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
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	logging "github.com/op/go-logging"
)

// bootstrapMaster starts the Service as master and begins bootstrapping the cluster from nothing.
func (s *Service) bootstrapMaster(ctx context.Context, runner Runner, config Config, bsCfg BootstrapConfig) {
	// Check HTTP server port
	containerHTTPPort, _, err := s.getHTTPServerPort()
	if err != nil {
		s.log.Fatalf("Cannot find HTTP server info: %#v", err)
	}
	if !WaitUntilPortAvailable(containerHTTPPort, time.Second*5) {
		s.log.Fatalf("Port %d is already in use", containerHTTPPort)
	}

	// Create initial cluster configuration
	hasAgent := boolFromRef(bsCfg.StartAgent, !s.mode.IsSingleMode())
	hasDBServer := boolFromRef(bsCfg.StartDBserver, true)
	hasCoordinator := boolFromRef(bsCfg.StartCoordinator, true)
	hasResilientSingle := boolFromRef(bsCfg.StartResilientSingle, s.mode.IsActiveFailoverMode())
	hasSyncMaster := boolFromRef(bsCfg.StartSyncMaster, true) && config.SyncEnabled
	hasSyncWorker := boolFromRef(bsCfg.StartSyncWorker, true) && config.SyncEnabled
	s.myPeers.Initialize(
		NewPeer(s.id, config.OwnAddress, s.announcePort, 0, config.DataDir,
			hasAgent, hasDBServer, hasCoordinator, hasResilientSingle,
			hasSyncMaster, hasSyncWorker,
			s.IsSecure()),
		bsCfg.AgencySize)
	s.learnOwnAddress = config.OwnAddress == ""

	// Start HTTP listener
	s.startHTTPServer(config)

	// Permanent loop:
	s.log.Infof("Serving as master with ID '%s' on %s:%d...", s.id, config.OwnAddress, s.announcePort)

	// Can we start right away?
	needMorePeers := true
	if s.mode.IsSingleMode() {
		needMorePeers = false
	} else if !s.myPeers.HaveEnoughAgents() {
		needMorePeers = true
	} else if bsCfg.StartLocalSlaves {
		peersNeeded := bsCfg.PeersNeeded()
		needMorePeers = len(s.myPeers.AllPeers) < peersNeeded
	}
	if !needMorePeers {
		// We have all the agents that we need, start a single server/cluster right now
		s.saveSetup()
		s.log.Info("Starting service...")
		s.startRunning(runner, config, bsCfg)
		return
	}

	wg := sync.WaitGroup{}
	if bsCfg.StartLocalSlaves {
		// Start additional local slaves
		s.createAndStartLocalSlaves(&wg, config, bsCfg)
	} else {
		// Show commands needed to start slaves
		s.log.Infof("Waiting for %d servers to show up.\n", s.myPeers.AgencySize)
		s.showSlaveStartCommands(runner, config)
	}

	for {
		time.Sleep(time.Second)
		select {
		case <-s.bootstrapCompleted.ctx.Done():
			s.saveSetup()
			s.log.Info("Starting service...")
			s.startRunning(runner, config, bsCfg)
			return
		default:
		}
		if ctx.Err() != nil {
			// Context is cancelled, stop now
			break
		}
	}
	// Wait for any local slaves to return.
	wg.Wait()
}

// showSlaveStartCommands prints out the commands needed to start additional slaves.
func (s *Service) showSlaveStartCommands(runner Runner, config Config) {
	s.log.Infof("Use the following commands to start other servers:")
	fmt.Println()
	for index := 2; index <= s.myPeers.AgencySize; index++ {
		port := ""
		if s.announcePort != config.MasterPort || config.MasterPort != DefaultMasterPort {
			port = strconv.Itoa(s.announcePort)
		}
		fmt.Println(runner.CreateStartArangodbCommand(config.DataDir, index, config.OwnAddress, port, config.DockerStarterImage, s.myPeers))
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
