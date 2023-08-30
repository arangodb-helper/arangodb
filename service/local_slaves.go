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
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

// createAndStartLocalSlaves creates additional peers for local slaves and starts services for them.
func (s *Service) createAndStartLocalSlaves(wg *sync.WaitGroup, config Config, bsCfg BootstrapConfig) {
	peersNeeded := bsCfg.PeersNeeded()
	peers := make([]Peer, 0, peersNeeded)
	for index := 2; index <= peersNeeded; index++ {
		p := Peer{}
		var err error
		p.ID, err = createUniqueID()
		if err != nil {
			s.log.Error().Err(err).Msg("Failed to create unique ID")
			continue
		}
		p.DataDir = filepath.Join(config.DataDir, fmt.Sprintf("local-slave-%d", index-1))
		peers = append(peers, p)
	}
	s.startLocalSlaves(wg, config, bsCfg, peers)
}

// startLocalSlaves starts additional services for local slaves based on the given peers.
func (s *Service) startLocalSlaves(wg *sync.WaitGroup, config Config, bsCfg BootstrapConfig, peers []Peer) {
	s.log = s.mustCreateIDLogger(s.id)
	s.log.Info().Msgf("Starting %d local slaves...", len(peers)-1)
	masterAddr := config.OwnAddress
	if masterAddr == "" {
		masterAddr = "127.0.0.1"
	}
	masterAddr = net.JoinHostPort(masterAddr, strconv.Itoa(s.announcePort))
	for _, p := range peers {
		if p.ID == s.id {
			continue
		}
		slaveLog := s.mustCreateIDLogger(p.ID)
		slaveBsCfg := bsCfg
		slaveBsCfg.ID = p.ID
		slaveBsCfg.StartLocalSlaves = false
		os.MkdirAll(p.DataDir, 0755)

		// Read existing setup.json (if any)
		setupConfig, relaunch, _ := ReadSetupConfig(slaveLog, p.DataDir)
		if relaunch {
			oldOpts := setupConfig.Peers.PersistentOptions
			newOpts := config.Configuration.PersistentOptions
			if err := oldOpts.ValidateCompatibility(&newOpts); err != nil {
				s.log.Error().Err(err).Msg("Please check pass-through options")
			}

			slaveBsCfg.LoadFromSetupConfig(setupConfig)
		}

		slaveConfig := config // Create copy
		slaveConfig.DataDir = p.DataDir
		slaveConfig.MasterAddresses = []string{masterAddr}
		slaveService := NewService(s.stopPeer.ctx, slaveLog, s.logService, slaveConfig, bsCfg, true)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := slaveService.Run(s.stopPeer.ctx, slaveBsCfg, setupConfig.Peers, relaunch); err != nil {
				s.log.Error().Str("peer", p.ID).Err(err).Msg("Unable to start one of peers")
			}
		}()
	}
}
