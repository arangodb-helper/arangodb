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
			s.log.Errorf("Failed to create unique ID: %#v", err)
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
	s.log.Infof("Starting %d local slaves...", len(peers)-1)
	masterAddr := config.OwnAddress
	if masterAddr == "" {
		masterAddr = "127.0.0.1"
	}
	masterAddr = net.JoinHostPort(masterAddr, strconv.Itoa(s.announcePort))
	for idx, p := range peers {
		if p.ID == s.id {
			continue
		}
		slaveLog := s.mustCreateIDLogger(p.ID)
		slaveBsCfg := bsCfg
		slaveBsCfg.ID = p.ID
		slaveBsCfg.StartLocalSlaves = false
		if bsCfg.Mode.IsResilientSingleMode() && idx > 1 {
			slaveBsCfg.StartResilientSingle = boolRef(false)
		}
		os.MkdirAll(p.DataDir, 0755)

		// Read existing setup.json (if any)
		slaveBsCfg, myPeers, relaunch, _ := ReadSetupConfig(slaveLog, p.DataDir, slaveBsCfg)
		slaveConfig := config // Create copy
		slaveConfig.DataDir = p.DataDir
		slaveConfig.MasterAddresses = []string{masterAddr}
		slaveService := NewService(s.stopPeer.ctx, slaveLog, slaveConfig, true)
		wg.Add(1)
		go func() {
			defer wg.Done()
			slaveService.Run(s.stopPeer.ctx, slaveBsCfg, myPeers, relaunch)
		}()
	}
}
