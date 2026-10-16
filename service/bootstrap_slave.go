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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/arangodb-helper/arangodb/client"
)

// registerPeerUnavailableTimeout bounds retries while the peer we join keeps
// answering 503. A running follower with no master URL used to produce that
// forever, so a starter that cannot join never launched arangod. Connection
// failures stay unlimited so starters may be launched before the master is up.
const registerPeerUnavailableTimeout = 40 * time.Second
const registerPeerRetryDelay = time.Second

var ErrRegisterPeerUnavailable = errors.New("register peer: service unavailable")

// bootstrapSlave starts the Service as slave and begins bootstrapping the cluster from nothing.
func (s *Service) bootstrapSlave(peerAddress string, runner Runner, config Config, bsCfg BootstrapConfig) {
	masterURL := s.createBootstrapMasterURL(peerAddress, config)

	_, hostPort, err := s.GetHTTPServerPort()
	if err != nil {
		s.log.Fatal().Err(err).Msg("Failed to get HTTP server port")
	}

	req := BuildHelloRequest(s.id, hostPort, s.IsSecure(), config, bsCfg)

	currentConfig := RegisterPeer(s.log, masterURL, req)

	// save result
	bsCfg.ServerStorageEngine = currentConfig.ServerStorageEngine
	s.runtimeClusterManager.myPeers = currentConfig

	// refresh port
	containerHTTPPort, _, err := s.GetHTTPServerPort()
	if err != nil {
		s.log.Fatal().Err(err).Msg("Failed to get HTTP server port")
	}

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
				s.log.Error().Err(err).Msg("Failed to connect to the leader")
				time.Sleep(time.Second * 2)
			} else if r.StatusCode != 200 {
				s.log.Warn().Msgf("Invalid status received from leader: %d", r.StatusCode)
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
	result, err := registerPeer(context.Background(), log, masterURL, req, registerPeerUnavailableTimeout, registerPeerRetryDelay)
	if err != nil {
		log.Fatal().Err(err).Msgf("Giving up registering peer with leader at %s", masterURL)
	}
	return result
}

func registerPeer(ctx context.Context, log zerolog.Logger, masterURL string, hello HelloRequest, unavailableTimeout, retryDelay time.Duration) (ClusterConfig, error) {
	var result ClusterConfig

	encoded, err := json.Marshal(hello)
	if err != nil {
		return result, fmt.Errorf("failed to encode Hello request: %w", err)
	}
	helloURL, err := getURLWithPath(masterURL, "/hello")
	if err != nil {
		return result, fmt.Errorf("failed to create Hello URL: %w", err)
	}

	var unavailableSince time.Time
	for {
		log.Info().Msgf("Registering peer with master at %s", masterURL)

		httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, helloURL, bytes.NewReader(encoded))
		if err != nil {
			return result, fmt.Errorf("failed to create Hello request: %w", err)
		}
		httpRequest.Header.Set("Content-Type", contentTypeJSON)
		r, err := httpClient.Do(httpRequest)
		if err != nil {
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			unavailableSince = time.Time{}
			log.Info().Err(err).Msg("Initial handshake with leader failed")
			if err := waitForRegisterPeerRetry(ctx, retryDelay); err != nil {
				return result, err
			}
			continue
		}

		body, err := io.ReadAll(r.Body)
		r.Body.Close()
		if err != nil {
			unavailableSince = time.Time{}
			log.Info().Err(err).Msg("Cannot start because HTTP response from the leader was bad")
			if err := waitForRegisterPeerRetry(ctx, retryDelay); err != nil {
				return result, err
			}
			continue
		}

		if r.StatusCode == http.StatusServiceUnavailable {
			msg := strings.TrimSpace(string(body))
			log.Info().Str("response", msg).Msg("Cannot start because service unavailable")
			now := time.Now()
			if unavailableSince.IsZero() {
				unavailableSince = now
			}
			if now.Sub(unavailableSince) >= unavailableTimeout {
				return result, fmt.Errorf("%w after %s from %s: %s", ErrRegisterPeerUnavailable, unavailableTimeout, masterURL, msg)
			}
			if err := waitForRegisterPeerRetry(ctx, retryDelay); err != nil {
				return result, err
			}
			continue
		}

		// Only an uninterrupted sequence of 503 responses consumes the
		// unavailable budget. Connection errors, malformed responses, and 404
		// all indicate a different transient state.
		unavailableSince = time.Time{}

		if r.StatusCode == http.StatusNotFound {
			log.Info().Msg("Cannot start because service not found")
			if err := waitForRegisterPeerRetry(ctx, retryDelay); err != nil {
				return result, err
			}
			continue
		}

		if r.StatusCode != http.StatusOK {
			err := client.ParseResponseError(r, body)
			return result, fmt.Errorf("cannot start because of HTTP error from leader: code=%d, message=%s", r.StatusCode, err.Error())
		}

		if err := json.Unmarshal(body, &result); err != nil {
			return result, fmt.Errorf("cannot parse body from leader: %w", err)
		}
		// Check result
		if _, found := result.PeerByID(hello.SlaveID); !found {
			return result, fmt.Errorf("leader responded with cluster config that does not contain my ID, please check the leader")
		}
		if result.ServerStorageEngine == "" {
			return result, fmt.Errorf("leader responded with cluster config that does not contain a ServerStorageEngine, please update leader first")
		}

		log.Info().Msgf("Successfully registered peer (ID: %s) with master at %s", hello.SlaveID, masterURL)

		return result, nil
	}
}

func waitForRegisterPeerRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func BuildHelloRequest(id string, slavePort int, isSecure bool, config Config, bsCfg BootstrapConfig) HelloRequest {
	return HelloRequest{
		DataDir:      config.DataDir,
		SlaveID:      id,
		SlaveAddress: config.OwnAddress,
		SlavePort:    slavePort,
		IsSecure:     isSecure,
		Agent:        copyBoolRef(bsCfg.StartAgent),
		DBServer:     copyBoolRef(bsCfg.StartDBserver),
		Coordinator:  copyBoolRef(bsCfg.StartCoordinator),
	}
}

func BuildHelloRequestFromPeer(p Peer) HelloRequest {
	return HelloRequest{
		DataDir: p.DataDir,
		SlaveID: p.ID,

		// we can not change the address and port of the peer
		SlaveAddress: p.Address,
		SlavePort:    p.Port,

		IsSecure:    p.IsSecure,
		Agent:       &p.HasAgentFlag,
		DBServer:    copyBoolRef(p.HasDBServerFlag),
		Coordinator: copyBoolRef(p.HasCoordinatorFlag),
	}
}
