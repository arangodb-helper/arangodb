//
// DISCLAIMER
//
// Copyright 2017-2024 ArangoDB GmbH, Cologne, Germany
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
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/arangodb-helper/arangodb/client"
)

// bootstrapSlave starts the Service as slave and begins bootstrapping the cluster from nothing.
func (s *Service) bootstrapSlave(peerAddress string, runner Runner, config Config, bsCfg BootstrapConfig) {
	masterURL := s.createBootstrapMasterURL(peerAddress, config)
	for {
		s.log.Info().Msgf("Contacting master %s...", masterURL)
		_, hostPort, err := s.getHTTPServerPort()
		if err != nil {
			s.log.Fatal().Err(err).Msg("Failed to get HTTP server port")
		}
		encoded, err := json.Marshal(HelloRequest{
			DataDir:      config.DataDir,
			SlaveID:      s.id,
			SlaveAddress: config.OwnAddress,
			SlavePort:    hostPort,
			IsSecure:     s.IsSecure(),
			Agent:        copyBoolRef(bsCfg.StartAgent),
			DBServer:     copyBoolRef(bsCfg.StartDBserver),
			Coordinator:  copyBoolRef(bsCfg.StartCoordinator),
		})
		if err != nil {
			s.log.Fatal().Err(err).Msg("Failed to encode Hello request")
		}
		helloURL, err := getURLWithPath(masterURL, "/hello")
		if err != nil {
			s.log.Fatal().Err(err).Msg("Failed to create Hello URL")
		}
		r, err := httpClient.Post(helloURL, contentTypeJSON, bytes.NewReader(encoded))
		if err != nil {
			s.log.Info().Err(err).Msg("Initial handshake with master failed")
			time.Sleep(time.Second)
			continue
		}

		body, err := io.ReadAll(r.Body)
		r.Body.Close()
		if err != nil {
			s.log.Info().Err(err).Msg("Cannot start because HTTP response from master was bad")
			time.Sleep(time.Second)
			continue
		}

		if r.StatusCode == http.StatusServiceUnavailable {
			s.log.Info().Err(err).Msg("Cannot start because service unavailable")
			time.Sleep(time.Second)
			continue
		}

		if r.StatusCode == http.StatusNotFound {
			s.log.Info().Err(err).Msg("Cannot start because service not found")
			time.Sleep(time.Second)
			continue
		}

		if r.StatusCode != http.StatusOK {
			err := client.ParseResponseError(r, body)
			s.log.Fatal().Msgf("Cannot start because of HTTP error from master: code=%d, message=%s\n", r.StatusCode, err.Error())
			return
		}
		var result ClusterConfig
		if err := json.Unmarshal(body, &result); err != nil {
			s.log.Warn().Err(err).Msg("Cannot parse body from master")
			return
		}
		// Check result
		if _, found := result.PeerByID(s.id); !found {
			s.log.Fatal().Msg("Master responsed with cluster config that does not contain my ID, please check master")
			return
		}
		if result.ServerStorageEngine == "" {
			s.log.Fatal().Msg("Master responsed with cluster config that does not contain a ServerStorageEngine, please update master first")
			return
		}
		// Save cluster config
		s.myPeers = result
		bsCfg.ServerStorageEngine = result.ServerStorageEngine
		break
	}

	// Check HTTP server port
	containerHTTPPort, _, err := s.getHTTPServerPort()
	if err != nil {
		s.log.Fatal().Err(err).Msg("Cannot find HTTP server info")
	}
	if !WaitUntilPortAvailable(config.BindAddress, containerHTTPPort, time.Second*5) {
		s.log.Fatal().Msgf("Port %d is already in use", containerHTTPPort)
	}

	// Run the HTTP service so we can forward other clients
	s.startHTTPServer(config)

	// Wait until we can start:
	if s.myPeers.AgencySize > 1 {
		s.log.Info().Msgf("Waiting for %d servers to show up...", s.myPeers.AgencySize)
	}
	for {
		if s.myPeers.HaveEnoughAgents() {
			// We have enough peers for a valid agency
			break
		} else {
			// Wait a bit until we have enough peers for a valid agency
			time.Sleep(time.Second)
			master := s.myPeers.AllPeers[0] // TODO replace with bootstrap master
			r, err := httpClient.Get(master.CreateStarterURL("/hello"))
			if err != nil {
				s.log.Error().Err(err).Msg("Failed to connect to master")
				time.Sleep(time.Second * 2)
			} else if r.StatusCode != 200 {
				s.log.Warn().Msgf("Invalid status received from master: %d", r.StatusCode)
			} else {
				defer r.Body.Close()
				body, _ := io.ReadAll(r.Body)
				var clusterConfig ClusterConfig
				json.Unmarshal(body, &clusterConfig)
				s.myPeers = clusterConfig
			}
		}
	}

	s.log.Info().Msgf("Serving as slave with ID '%s' on %s:%d...", s.id, config.OwnAddress, s.announcePort)
	s.log.Info().Msgf("Using storage engine '%s'", bsCfg.ServerStorageEngine)
	s.saveSetup()
	s.startRunning(runner, config, bsCfg)
}
