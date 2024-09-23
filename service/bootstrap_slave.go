//
// DISCLAIMER
//
// Copyright 2014 ArangoDB GmbH, Cologne, Germany
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

	"github.com/rs/zerolog"

	"github.com/arangodb-helper/arangodb/client"
)

// bootstrapSlave starts the Service as slave and begins bootstrapping the cluster from nothing.
func (s *Service) bootstrapSlave(peerAddress string, runner Runner, config Config, bsCfg BootstrapConfig) {
	masterURL := s.createBootstrapMasterURL(peerAddress, config)

	containerHTTPPort, hostPort, err := s.getHTTPServerPort()
	if err != nil {
		s.log.Fatal().Err(err).Msg("Failed to get HTTP server port")
	}

	req := BuildHelloRequest(s.id, hostPort, s.IsSecure(), config, bsCfg)

	currentConfig := RegisterPeer(s.log, masterURL, req)

	// save result
	bsCfg.ServerStorageEngine = currentConfig.ServerStorageEngine
	s.runtimeClusterManager.myPeers = currentConfig

	if !WaitUntilPortAvailable(config.BindAddress, containerHTTPPort, time.Second*5) {
		s.log.Fatal().Msgf("Port %d is already in use", containerHTTPPort)
	}

	// Run the HTTP service so we can forward other clients
	s.startHTTPServer(config)

	// Wait until we can start:
	if s.runtimeClusterManager.myPeers.AgencySize > 1 {
		s.log.Info().Msgf("Waiting for %d servers to show up...", s.runtimeClusterManager.myPeers.AgencySize)
	}
	for {
		if s.runtimeClusterManager.myPeers.HaveEnoughAgents() {
			// We have enough peers for a valid agency
			break
		} else {
			// Wait a bit until we have enough peers for a valid agency
			time.Sleep(time.Second)
			master := s.runtimeClusterManager.myPeers.AllPeers[0] // TODO replace with bootstrap master
			r, err := httpClient.Get(master.CreateStarterURL("/hello"))
			if err != nil {
				s.log.Error().Err(err).Msg("Failed to connect to leader")
				time.Sleep(time.Second * 2)
			} else if r.StatusCode != 200 {
				s.log.Warn().Msgf("Invalid status received from the leader: %d", r.StatusCode)
			} else {
				defer r.Body.Close()
				body, _ := io.ReadAll(r.Body)
				var clusterConfig ClusterConfig
				json.Unmarshal(body, &clusterConfig)
				s.runtimeClusterManager.myPeers = clusterConfig
			}
		}
	}

	s.log.Info().Msgf("Serving as follower with ID '%s' on %s:%d...", s.id, config.OwnAddress, s.announcePort)
	s.log.Info().Msgf("Using storage engine '%s'", bsCfg.ServerStorageEngine)
	s.saveSetup()
	s.startRunning(runner, config, bsCfg)
}

func RegisterPeer(log zerolog.Logger, masterURL string, req HelloRequest) ClusterConfig {
	for {
		log.Info().Msgf("Contacting master %s...", masterURL)

		encoded, err := json.Marshal(req)
		if err != nil {
			log.Fatal().Err(err).Msg("Failed to encode Hello request")
		}
		helloURL, err := getURLWithPath(masterURL, "/hello")
		if err != nil {
			log.Fatal().Err(err).Msg("Failed to create Hello URL")
		}
		r, err := httpClient.Post(helloURL, contentTypeJSON, bytes.NewReader(encoded))
		if err != nil {
			log.Info().Err(err).Msg("Initial handshake with leader failed")
			time.Sleep(time.Second)
			continue
		}

		body, err := io.ReadAll(r.Body)
		r.Body.Close()
		if err != nil {
			log.Info().Err(err).Msg("Cannot start because HTTP response from leader was bad")
			time.Sleep(time.Second)
			continue
		}

		if r.StatusCode == http.StatusServiceUnavailable {
			log.Info().Err(err).Msg("Cannot start because service unavailable")
			time.Sleep(time.Second)
			continue
		}

		if r.StatusCode == http.StatusNotFound {
			log.Info().Err(err).Msg("Cannot start because service not found")
			time.Sleep(time.Second)
			continue
		}

		var result ClusterConfig

		if r.StatusCode != http.StatusOK {
			err := client.ParseResponseError(r, body)
			log.Fatal().Msgf("Cannot start because of HTTP error from master: code=%d, message=%s\n", r.StatusCode, err.Error())
			return result
		}

		if err := json.Unmarshal(body, &result); err != nil {
			log.Warn().Err(err).Msg("Cannot parse body from the leader")
			return result
		}
		// Check result
		if _, found := result.PeerByID(req.SlaveID); !found {
			log.Fatal().Msg("Master responded with cluster config that does not contain my ID, please check leader")
			return result
		}
		if result.ServerStorageEngine == "" {
			log.Fatal().Msg("Master responded with cluster config that does not contain a ServerStorageEngine, please update leader first")
			return result
		}

		return result
	}
}

func BuildHelloRequest(id string, slavePort int, isSecure bool, config Config, bsCfg BootstrapConfig) HelloRequest {
	return HelloRequest{
		DataDir:         config.DataDir,
		SlaveID:         id,
		SlaveAddress:    config.OwnAddress,
		SlavePort:       slavePort,
		IsSecure:        isSecure,
		Agent:           copyBoolRef(bsCfg.StartAgent),
		DBServer:        copyBoolRef(bsCfg.StartDBserver),
		Coordinator:     copyBoolRef(bsCfg.StartCoordinator),
		ResilientSingle: copyBoolRef(bsCfg.StartResilientSingle),
		SyncMaster:      copyBoolRef(bsCfg.StartSyncMaster),
		SyncWorker:      copyBoolRef(bsCfg.StartSyncWorker),
	}
}
