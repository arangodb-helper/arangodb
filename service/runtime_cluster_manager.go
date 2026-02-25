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
	"context"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sync"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/rs/zerolog"

	"github.com/arangodb-helper/arangodb/agency"
)

const (
	masterURLTTL = time.Second * 30
)

var (
	masterURLKey = []string{"arangodb-helper", "arangodb", "leader"}
)

// runtimeClusterManager keeps the cluster configuration up to date during a running state.
type runtimeClusterManager struct {
	mutex            sync.Mutex
	log              zerolog.Logger
	runtimeContext   runtimeClusterManagerContext
	lastMasterURL    string
	avoidBeingMaster bool // If set, this peer will not try to become master
	interruptChan    chan struct{}
	myPeers          ClusterConfig
}

// runtimeClusterManagerContext provides a context for the runtimeClusterManager.
type runtimeClusterManagerContext interface {
	ClientBuilder

	// ClusterConfig returns the current cluster configuration and the current peer
	ClusterConfig() (ClusterConfig, *Peer, ServiceMode)

	// ChangeState alters the current state of the service
	ChangeState(newState State)

	// UpdateClusterConfig updates the current cluster configuration.
	UpdateClusterConfig(ClusterConfig) error

	GetHTTPServerPort() (containerPort, hostPort int, err error)
}

// Create a client for the agency
func (s *runtimeClusterManager) createAgencyAPI() (agency.Agency, error) {
	// Get cluster config
	clusterConfig, _, _ := s.runtimeContext.ClusterConfig()
	// Create client
	return clusterConfig.CreateAgencyAPI(s.runtimeContext)
}

// updateClusterConfiguration asks the master at given URL for the latest cluster configuration.
func (s *runtimeClusterManager) updateClusterConfiguration(ctx context.Context, masterURL string, myPeer string) error {
	helloURL, err := getURLWithPath(masterURL, "/hello?update=1")
	if err != nil {
		return maskAny(err)
	}
	// Perform request
	r, err := httpClient.Get(helloURL)
	if err != nil {
		return maskAny(err)
	}
	// Check status
	if r.StatusCode != 200 {
		return maskAny(fmt.Errorf("invalid status %d from master", r.StatusCode))
	}
	// Parse result
	defer r.Body.Close()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return maskAny(err)
	}
	var clusterConfig ClusterConfig
	if err := json.Unmarshal(body, &clusterConfig); err != nil {
		return maskAny(err)
	}

	latestPeerVersion, _ := s.myPeers.PeerByID(myPeer)

	myPeerFromMaster, exist := clusterConfig.PeerByID(myPeer)
	if !exist {
		s.log.Warn().Msgf("Leader responded with cluster config that does not contain this peer, re-registering. Local peer: %v", latestPeerVersion)
		clusterConfig = RegisterPeer(s.log, masterURL, BuildHelloRequestFromPeer(latestPeerVersion))
	}

	if !reflect.DeepEqual(latestPeerVersion, myPeerFromMaster) {
		s.log.Warn().Msgf("Leader responded with cluster config that does contain different peer, re-registering. Peer from master: %v, Local peer: %v", myPeerFromMaster, latestPeerVersion)
		clusterConfig = RegisterPeer(s.log, masterURL, BuildHelloRequestFromPeer(latestPeerVersion))
	}

	// We've received a cluster config - let's store it
	err = s.runtimeContext.UpdateClusterConfig(clusterConfig)
	if err != nil {
		s.log.Warn().Err(err).Msg("Failed to update cluster configuration")
		return err
	}

	return nil
}

func (s *runtimeClusterManager) runLeaderElection(ctx context.Context, myURL string) {
	le := agency.NewLeaderElectionCell[string](masterURLKey, masterURLTTL)

	delay := time.Microsecond
	resignErrBackoff := backoff.NewExponentialBackOff()
	for {
		timer := time.NewTimer(delay)
		// Wait a bit
		select {
		case <-timer.C:
			// Delay over, just continue
		case <-ctx.Done():
			// We're asked to stop
			if !timer.Stop() {
				<-timer.C
			}
			return
		}

		agencyClient, err := s.createAgencyAPI()
		if err != nil {
			delay = time.Second
			s.log.Debug().Err(err).Msgf("could not create agency client. Retrying in %s", delay)
			continue
		}

		oldMasterURL := s.GetMasterURL()
		if s.avoidBeingMaster {
			if oldMasterURL == "" {
				s.log.Debug().Msg("Initializing master URL before resigning")
				currMasterURL, err := le.Read(ctx, agencyClient)
				if err != nil {
					delay = 5 * time.Second
					s.log.Err(err).Msgf("Failed to read current value before resigning. Retrying in %s", delay)
					continue
				}
				s.updateMasterURL(currMasterURL, currMasterURL == myURL)
			}

			s.log.Debug().Str("master_url", myURL).Msgf("Resigning leadership")
			err = le.Resign(ctx, agencyClient)
			if err != nil {
				delay = resignErrBackoff.NextBackOff()
				s.log.Err(err).Msgf("Resigning leadership failed. Retrying in %s", delay)
				continue
			} else {
				s.runtimeContext.ChangeState(stateRunningSlave)
				return
			}
		}

		var masterURL string
		var isMaster bool

		masterURL, isMaster, delay, err = le.Update(ctx, agencyClient, myURL)
		if err != nil {
			delay = 5 * time.Second
			s.log.Error().Err(err).Msgf("Update leader election failed. Retrying in %s", delay)
			continue
		}

		s.log.Debug().
			Str("myURL", myURL).
			Str("masterURL", masterURL).
			Str("oldMasterURL", oldMasterURL).
			Msg("Updating leadership")

		if isMaster && masterURL != myURL {
			s.log.Error().Msgf("Unexpected error: this peer is a master but URL differs. Should be %s got %s", myURL, masterURL)
		}

		s.updateMasterURL(masterURL, isMaster)
	}
}

func (s *runtimeClusterManager) updateMasterURL(masterURL string, isMaster bool) {
	newState := stateRunningSlave
	if isMaster {
		newState = stateRunningMaster
	}
	var oldMasterURL string
	s.mutex.Lock()
	oldMasterURL = s.lastMasterURL
	s.lastMasterURL = masterURL
	s.runtimeContext.ChangeState(newState)
	s.mutex.Unlock()

	if oldMasterURL != masterURL {
		if isMaster {
			s.log.Info().Str("new_master_url", masterURL).Msg("Just became master")
		} else {
			s.log.Info().Str("old_master_url", oldMasterURL).Str("new_master_url", masterURL).Msg("Master changed")
		}
		// trigger main loop so config will be updated sooner
		s.interrupt()
	}
}

// Run keeps the cluster configuration up to date, either as master or as slave
// during a running state.
func (s *runtimeClusterManager) Run(ctx context.Context, log zerolog.Logger, runtimeContext runtimeClusterManagerContext) {
	s.log = log
	s.runtimeContext = runtimeContext
	s.interruptChan = make(chan struct{}, 32)
	_, myPeer, mode := runtimeContext.ClusterConfig()
	if !mode.IsClusterMode() {
		// Cluster manager is only relevant in cluster mode
		return
	}
	if myPeer == nil {
		// We need to know our own peer
		log.Error().Msg("Cannot run runtime cluster manager without own peer")
		return
	}

	ownURL := myPeer.CreateStarterURL("/")

	go s.runLeaderElection(ctx, ownURL)

	for {
		delay := time.Microsecond
		// Loop until stopping
		if ctx.Err() != nil {
			// Stop requested
			return
		}

		masterURL := s.GetMasterURL()
		if masterURL != "" && masterURL != ownURL {
			log.Debug().Msgf("Updating cluster configuration master URL: %s", masterURL)
			// We are a follower, try to update cluster configuration from leader
			if err := s.updateClusterConfiguration(ctx, masterURL, myPeer.ID); err != nil {
				delay = time.Second * 5
				log.Warn().Err(err).Msgf("Failed to load cluster configuration from %s", masterURL)
			} else {
				// Wait a bit until re-updating the configuration
				delay = time.Second * 15
			}
		} else {
			// we are still leading or not initialized, check again later
			delay = time.Second * 5
		}

		timer := time.NewTimer(delay)
		// Wait a bit
		select {
		case <-timer.C:
		// Delay over, just continue
		case <-ctx.Done():
			// We're asked to stop
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-s.interruptChan:
			// We're being interrupted
			log.Debug().Msg("Being interrupted")
			// continue now
		}
	}
}

// GetMasterURL returns the last known URL of the master (can be empty)
func (s *runtimeClusterManager) GetMasterURL() string {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.lastMasterURL
}

// AvoidBeingMaster instructs the runtime cluster manager to avoid
// becoming master and when it is master, to give that up.
func (s *runtimeClusterManager) AvoidBeingMaster() {
	s.avoidBeingMaster = true
}

// interrupt the runtime cluster manager loop after some event has happened.
func (s *runtimeClusterManager) interrupt() {
	// Interrupt loop so we act on this right away
	if ch := s.interruptChan; ch != nil {
		ch <- struct{}{}
	}
}
