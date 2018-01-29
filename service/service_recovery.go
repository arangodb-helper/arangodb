//
// DISCLAIMER
//
// Copyright 2018 ArangoDB GmbH, Cologne, Germany
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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	driver "github.com/arangodb/go-driver"
)

const (
	recoveryFileName = "RECOVERY"
)

// PerformRecovery looks for a RECOVERY file in the data directory and performs
// a recovery of such a file exists.
func (s *Service) PerformRecovery(ctx context.Context, bsCfg BootstrapConfig) (BootstrapConfig, error) {
	recoveryPath := filepath.Join(s.cfg.DataDir, recoveryFileName)
	recoveryContent, err := ioutil.ReadFile(recoveryPath)
	if os.IsNotExist(err) {
		// Recovery file does not exist. We're done.
		return bsCfg, nil
	}
	if err != nil {
		s.log.Errorf("Cannot read RECOVERY file")
		return bsCfg, maskAny(err)
	}

	// Parse recovery file content (expected `host:port`)
	starterHost, starterPort, err := net.SplitHostPort(strings.TrimSpace(string(recoveryContent)))
	if err != nil {
		s.log.Errorf("Invalid content of RECOVERY file; expected `host:port`: %#v", err)
		return bsCfg, maskAny(err)
	}
	starterHost = normalizeHostName(starterHost)
	port, err := strconv.Atoi(starterPort)
	if err != nil {
		s.log.Errorf("Invalid port of RECOVERY file; expected `host:port`: %#v", err)
		return bsCfg, maskAny(err)
	}

	// Check mode
	if !s.mode.SupportsRecovery() {
		s.log.Errorf("Recovery is not support for mode '%s'", s.mode)
		return bsCfg, maskAny(fmt.Errorf("Recovery not supported"))
	}

	// Notify user
	s.log.Infof("Trying to recover as starter %s:%d", starterHost, port)

	// Get cluster config info from one of the remaining starters.
	clusterConfig, err := s.getRecoveryClusterConfig(ctx, s.cfg.MasterAddresses, net.JoinHostPort(starterHost, starterPort))
	if err != nil {
		s.log.Errorf("Cannot get cluster configuration from remaining starters: %#v", err)
		return bsCfg, maskAny(err)
	}

	// Look for ID of this starter
	peer, found := clusterConfig.PeerByAddressAndPort(starterHost, port)
	if !found {
		s.log.Errorf("Cannot find a peer in cluster configuration for address %s with port %d", starterHost, port)
		foundHosts := make([]string, 0, len(clusterConfig.AllPeers))
		for _, p := range clusterConfig.AllPeers {
			foundHosts = append(foundHosts, net.JoinHostPort(p.Address, strconv.Itoa(p.Port+p.PortOffset)))
		}
		sort.Strings(foundHosts)
		s.log.Infof("Starters found are: %s", strings.Join(foundHosts, ", "))
		return bsCfg, maskAny(fmt.Errorf("No peer found for %s:%d", starterHost, port))
	}

	// Set our peer ID
	s.id = peer.ID
	s.myPeers = clusterConfig
	bsCfg.ID = peer.ID

	// Do we have an agent on our peer?
	if peer.HasAgent() {
		// Ask cluster for its health in order to find the ID of our agent
		client, err := clusterConfig.CreateCoordinatorsClient(ctx, bsCfg.JwtSecret)
		if err != nil {
			s.log.Errorf("Cannot create coordinator client: %#v", err)
			return bsCfg, maskAny(err)
		}

		// Fetch cluster health
		c, err := client.Cluster(ctx)
		if err != nil {
			s.log.Errorf("Cannot get cluster client: %#v", err)
			return bsCfg, maskAny(err)
		}
		h, err := c.Health(ctx)
		if err != nil {
			s.log.Errorf("Cannot get cluster health: %#v", err)
			return bsCfg, maskAny(err)
		}

		// Find agent ID
		found := false
		agentPort := peer.Port + peer.PortOffset + ServerType(ServerTypeAgent).PortOffset()
		expectedAgentHost := strings.ToLower(net.JoinHostPort(peer.Address, strconv.Itoa(agentPort)))
		foundAgentHosts := make([]string, 0, len(h.Health))
		for id, server := range h.Health {
			if server.Role == driver.ServerRoleAgent {
				ep, err := url.Parse(server.Endpoint)
				if err != nil {
					s.log.Errorf("Failed to parse server endpoint: %#v", err)
				} else {
					if strings.ToLower(ep.Host) == expectedAgentHost {
						bsCfg.RecoveryAgentID = string(id)
						found = true
						break
					} else {
						foundAgentHosts = append(foundAgentHosts, ep.Host)
					}
				}
			}
		}
		if !found {
			s.log.Errorf("Cannot find server ID of agent with host '%s'", expectedAgentHost)
			sort.Strings(foundAgentHosts)
			s.log.Infof("Agent found are: %s", strings.Join(foundAgentHosts, ", "))
			return bsCfg, maskAny(fmt.Errorf("Cannot find agent ID"))
		}

		// Remove agent data directory
		agentDataDir, err := s.serverHostDir(ServerTypeAgent)
		if err != nil {
			s.log.Errorf("Cannot get agent directory: %#v", err)
			return bsCfg, maskAny(err)
		}
		os.RemoveAll(agentDataDir)
	}

	// Record recovery file, so we can remove it when all is started again
	s.recoveryFile = recoveryPath

	// Inform user
	s.log.Infof("Recovery information all available, starting...")

	return bsCfg, nil
}

// removeRecoveryFile removes any recorded RECOVERY file.
func (s *Service) removeRecoveryFile() {
	if s.recoveryFile != "" {
		if err := os.Remove(s.recoveryFile); err != nil {
			s.log.Errorf("Failed to remove RECOVERY file: %#v", err)
		} else {
			s.log.Info("Removed RECOVERY file.")
			s.log.Info("Most likely there is now an extra coordinator & dbserver in FAILED state. Remove them manually using the web UI.")
			s.recoveryFile = ""
		}
	}
}

// getRecoveryClusterConfig tries to load the cluster configuration from the given master URL.
func (s *Service) getRecoveryClusterConfig(ctx context.Context, masterAddresses []string, recoveryAddress string) (ClusterConfig, error) {
	// Helper to fetch from specific master
	fetch := func(ctx context.Context, masterURL string) (ClusterConfig, error) {
		helloURL, err := getURLWithPath(masterURL, "/hello")
		if err != nil {
			return ClusterConfig{}, maskAny(err)
		}
		// Perform request
		r, err := httpClient.Get(helloURL)
		if err != nil {
			return ClusterConfig{}, maskAny(err)
		}
		// Check status
		if r.StatusCode != 200 {
			return ClusterConfig{}, maskAny(fmt.Errorf("Invalid status %d from master", r.StatusCode))
		}
		// Parse result
		defer r.Body.Close()
		body, err := ioutil.ReadAll(r.Body)
		if err != nil {
			return ClusterConfig{}, maskAny(err)
		}
		var clusterConfig ClusterConfig
		if err := json.Unmarshal(body, &clusterConfig); err != nil {
			return ClusterConfig{}, maskAny(err)
		}
		return clusterConfig, nil
	}

	// Go over all master addresses, asking for the cluster config.
	// The first to return a valid value is used.
	for _, addr := range masterAddresses {
		if strings.ToLower(addr) == strings.ToLower(recoveryAddress) {
			// Skip using our own address
			continue
		}
		masterURL := s.createBootstrapMasterURL(addr, s.cfg)
		cCfg, err := fetch(ctx, masterURL)
		if err == nil {
			return cCfg, nil
		}
		s.log.Debugf("Fetching cluster configure from %s failed: %#v", masterURL, err)
	}

	return ClusterConfig{}, maskAny(fmt.Errorf("No starter is able to answer our recovery request"))
}
