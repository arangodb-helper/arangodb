//
// DISCLAIMER
//
// Copyright 2017-2023 ArangoDB GmbH, Cologne, Germany
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

package service

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// bootstrapMaster starts the Service as master and begins bootstrapping the cluster from nothing.
func (s *Service) bootstrapMaster(ctx context.Context, runner Runner, config Config, bsCfg BootstrapConfig) {
	// Check HTTP server port
	containerHTTPPort, _, err := s.getHTTPServerPort()
	if err != nil {
		s.log.Fatal().Err(err).Msg("Cannot find HTTP server info")
	}
	if !WaitUntilPortAvailable(config.BindAddress, containerHTTPPort, time.Second*5) {
		s.log.Fatal().Msgf("Port %d is already in use", containerHTTPPort)
	}

	// Select storage engine
	storageEngine := bsCfg.ServerStorageEngine
	if storageEngine == "" {
		storageEngine = s.DatabaseFeatures().DefaultStorageEngine()
		bsCfg.ServerStorageEngine = storageEngine
	}
	s.log.Info().Msgf("Using storage engine '%s'", bsCfg.ServerStorageEngine)

	// Create initial cluster configuration
	servers := preparePeerServers(s.mode, bsCfg, config)

	me := newPeer(s.id, config.OwnAddress, s.announcePort, 0, config.DataDir, servers, s.IsSecure())
	s.myPeers.Initialize(me, bsCfg.AgencySize, storageEngine, s.cfg.Configuration.PersistentOptions)
	s.learnOwnAddress = config.OwnAddress == ""

	// Start HTTP listener
	s.startHTTPServer(config)

	// Permanent loop:
	s.log.Info().Msgf("Serving as master with ID '%s' on %s:%d...", s.id, config.OwnAddress, s.announcePort)

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
		s.log.Info().Msg("Starting service...")
		s.startRunning(runner, config, bsCfg)
		return
	}

	wg := sync.WaitGroup{}
	if bsCfg.StartLocalSlaves {
		// Start additional local slaves
		s.createAndStartLocalSlaves(&wg, config, bsCfg)
	} else {
		// Show commands needed to start slaves
		s.log.Info().Msgf("Waiting for %d servers to show up.\n", s.myPeers.AgencySize)
		s.showSlaveStartCommands(runner, config)
	}

	for {
		time.Sleep(time.Second)
		select {
		case <-s.bootstrapCompleted.ctx.Done():
			s.saveSetup()
			s.log.Info().Msg("Starting service...")
			s.startRunning(runner, config, bsCfg)
			break
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
	s.log.Info().Msg("Use the following commands to start other servers:")
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
func (s *Service) mustCreateIDLogger(id string) zerolog.Logger {
	return s.log.With().Str("id", id).Logger()
}
