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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"reflect"
	"sync"
	"time"

	"github.com/arangodb-helper/arangodb/service/arangod"
	logging "github.com/op/go-logging"
)

const (
	masterURLTTL = time.Second * 30
)

var (
	masterURLKey = []string{"arangodb-helper", "arangodb", "master"}
)

// runtimeClusterManager keeps the cluster configuration up to date during a running state.
type runtimeClusterManager struct {
	mutex            sync.Mutex
	runtimeContext   runtimeClusterManagerContext
	lastMasterURL    string
	avoidBeingMaster bool // If set, this peer will not try to become master
	interruptChan    chan struct{}
}

// runtimeClusterManagerContext provides a context for the runtimeClusterManager.
type runtimeClusterManagerContext interface {
	// ClusterConfig returns the current cluster configuration and the current peer
	ClusterConfig() (ClusterConfig, *Peer, ServiceMode)

	// ChangeState alters the current state of the service
	ChangeState(newState State)

	// PrepareDatabaseServerRequestFunc returns a function that is used to
	// prepare a request to a database server (including authentication).
	PrepareDatabaseServerRequestFunc() func(*http.Request) error

	// UpdateClusterConfig updates the current cluster configuration.
	UpdateClusterConfig(ClusterConfig)
}

// Create a client for the agency
func (s *runtimeClusterManager) createAgencyAPI() (arangod.AgencyAPI, error) {
	prepareReq := s.runtimeContext.PrepareDatabaseServerRequestFunc()
	// Get cluster config
	clusterConfig, _, _ := s.runtimeContext.ClusterConfig()
	// Create client
	return clusterConfig.CreateAgencyAPI(prepareReq)
}

// getMasterURL tries to get the URL of the current master from
// a well known location in the agency.
func (s *runtimeClusterManager) getMasterURL(ctx context.Context) (string, error) {
	// Get api client
	api, err := s.createAgencyAPI()
	if err != nil {
		return "", maskAny(err)
	}
	// Try to read master URL
	ctx, cancel := context.WithTimeout(ctx, time.Second*10)
	defer cancel()
	if result, err := api.ReadKey(ctx, masterURLKey); err != nil {
		if arangod.IsKeyNotFound(err) {
			return "", nil
		}
		return "", maskAny(err)
	} else if strResult, ok := result.(string); ok {
		return strResult, nil
	} else if mapResult, ok := result.(map[string]interface{}); ok && len(mapResult) == 0 {
		return "", nil
	} else {
		return "", maskAny(fmt.Errorf("Invalid value type at key: %v", reflect.TypeOf(result)))
	}
}

// tryBecomeMaster tries to write our URL into a well known location in the agency,
// assuming there is no master.
func (s *runtimeClusterManager) tryBecomeMaster(ctx context.Context, ownURL string) error {
	// Get api client
	api, err := s.createAgencyAPI()
	if err != nil {
		return maskAny(err)
	}
	// Try to write our master URL
	ctx, cancel := context.WithTimeout(ctx, time.Second*10)
	defer cancel()
	if err := api.WriteKeyIfEmpty(ctx, masterURLKey, ownURL, masterURLTTL); err != nil {
		return maskAny(err)
	}
	return nil
}

// tryRemainMaster tries to write our URL into a well known location in the agency,
// assuming we're already the master.
func (s *runtimeClusterManager) tryRemainMaster(ctx context.Context, ownURL string) error {
	// Get api client
	api, err := s.createAgencyAPI()
	if err != nil {
		return maskAny(err)
	}
	// Try to update our master URL
	ctx, cancel := context.WithTimeout(ctx, time.Second*10)
	defer cancel()
	if err := api.WriteKeyIfEqualTo(ctx, masterURLKey, ownURL, ownURL, masterURLTTL); err != nil {
		return maskAny(err)
	}
	return nil
}

// tryStopBeingMaster tries to write an empty URL into a well known location in the agency,
// assuming we're already the master.
// This will enable other peers to become master
func (s *runtimeClusterManager) tryStopBeingMaster(ctx context.Context, ownURL string) error {
	// Get api client
	api, err := s.createAgencyAPI()
	if err != nil {
		return maskAny(err)
	}
	// Try to update our master URL
	ctx, cancel := context.WithTimeout(ctx, time.Second*10)
	defer cancel()
	if err := api.RemoveKeyIfEqualTo(ctx, masterURLKey, ownURL); err != nil {
		return maskAny(err)
	}
	return nil
}

// updateClusterConfiguration asks the master at given URL for the latest cluster configuration.
func (s *runtimeClusterManager) updateClusterConfiguration(ctx context.Context, masterURL string) error {
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
		return maskAny(fmt.Errorf("Invalid status %d from master", r.StatusCode))
	}
	// Parse result
	defer r.Body.Close()
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return maskAny(err)
	}
	var clusterConfig ClusterConfig
	if err := json.Unmarshal(body, &clusterConfig); err != nil {
		return maskAny(err)
	}
	// We've received a cluster config
	s.runtimeContext.UpdateClusterConfig(clusterConfig)

	return nil
}

// registerMasterChangedCallback registers our callback URL with the agency
func (s *runtimeClusterManager) registerMasterChangedCallback(ctx context.Context, ownURL string) error {
	// Get api client
	api, err := s.createAgencyAPI()
	if err != nil {
		return maskAny(err)
	}
	// Register callback
	cbURL, err := getURLWithPath(ownURL, "/cb/masterChanged")
	if err != nil {
		return maskAny(err)
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second*10)
	defer cancel()
	if err := api.RegisterChangeCallback(ctx, masterURLKey, cbURL); err != nil {
		return maskAny(err)
	}
	return nil
}

// unregisterMasterChangedCallback removes our callback URL from the agency
func (s *runtimeClusterManager) unregisterMasterChangedCallback(ctx context.Context, ownURL string) error {
	// Get api client
	api, err := s.createAgencyAPI()
	if err != nil {
		return maskAny(err)
	}
	// Register callback
	cbURL, err := getURLWithPath(ownURL, "/cb/masterChanged")
	if err != nil {
		return maskAny(err)
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second*10)
	defer cancel()
	if err := api.UnregisterChangeCallback(ctx, masterURLKey, cbURL); err != nil {
		return maskAny(err)
	}
	return nil
}

// Run keeps the cluster configuration up to date, either as master or as slave
// during a running state.
func (s *runtimeClusterManager) Run(ctx context.Context, log *logging.Logger, runtimeContext runtimeClusterManagerContext) {
	s.runtimeContext = runtimeContext
	s.interruptChan = make(chan struct{}, 32)
	_, myPeer, mode := runtimeContext.ClusterConfig()
	if !mode.IsClusterMode() {
		// Cluster manager is only relevant in cluster mode
		return
	}
	if myPeer == nil {
		// We need to know our own peer
		log.Error("Cannot run runtime cluster manager without own peer")
		return
	}
	ownURL := myPeer.CreateStarterURL("/")

	callbackRegistered := false
	for {
		var delay time.Duration
		// Loop until stopping
		if ctx.Err() != nil {
			// Stop requested
			return
		}

		// Try to get master URL
		masterURL, err := s.getMasterURL(ctx)
		if err != nil {
			// Cannot obtain master url, wait a while and try again
			log.Infof("Failed to get master URL, retrying in 5sec (%#v)", err)
			delay = time.Second * 5
		} else {
			// Store current master
			s.mutex.Lock()
			s.lastMasterURL = masterURL
			s.mutex.Unlock()

			// Register master changed callback (if needed)
			if !callbackRegistered && masterURL != "" {
				log.Info("Register master callback...")
				if err := s.registerMasterChangedCallback(ctx, ownURL); err != nil {
					log.Debugf("Failed to register master callback: %#v", err)
				} else {
					log.Info("Registered master callback")
					callbackRegistered = true
					defer s.unregisterMasterChangedCallback(context.Background(), ownURL)
				}
			}

			if masterURL == "" {
				// There is currently no master, try to become master (if allowed)
				if !s.avoidBeingMaster {
					log.Debug("There is no current master, try to become master")
					if err := s.tryBecomeMaster(ctx, ownURL); err != nil {
						log.Infof("tried to become master but failed: %#v", err)
						runtimeContext.ChangeState(stateRunningSlave)
					} else {
						log.Info("Just became master")
						runtimeContext.ChangeState(stateRunningMaster)
					}
					// Wait a bit, after which we'll register callback (if needed)
					delay = time.Second
				} else {
					// We're not allowed to become master but there is no master,
					// just wait a bit
					delay = time.Second
				}
			} else if masterURL == ownURL {
				// We are the master, update our entry in the agency
				log.Debug("We're master, try to remain it")
				runtimeContext.ChangeState(stateRunningMaster)

				if !s.avoidBeingMaster {
					// Update agency
					if err := s.tryRemainMaster(ctx, ownURL); err != nil {
						log.Info("Failed to remain master: %#v", err)
						runtimeContext.ChangeState(stateRunningSlave)

						// Retry soon
						delay = time.Second
					} else {
						// I'm still the master
						// wait a bit before updating master URL
						delay = masterURLTTL / 3
					}
				} else {
					// We're master, but we want to avoid that, try giving up being master
					log.Info("Trying to stop being master...")
					if err := s.tryStopBeingMaster(ctx, ownURL); err != nil {
						log.Warningf("Failed to stop being master: %#v", err)
						// Retry soon
						delay = time.Second
					} else {
						// I'm no longer master
						log.Info("Stopped being master")
						runtimeContext.ChangeState(stateRunningSlave)
						// Come back soon to see who took over
						delay = time.Second
					}
				}
			} else {
				// We are slave, try to update cluster configuration from master
				log.Debugf("We're slave, try to update cluster config from %s", masterURL)
				runtimeContext.ChangeState(stateRunningSlave)

				// Ask current master for cluster configuration
				if err := s.updateClusterConfiguration(ctx, masterURL); err != nil {
					log.Warningf("Failed to load cluster configuration from %s: %#v", masterURL, err)
				}

				// Wait a bit until re-updating the configuration
				delay = time.Second * 15
			}
		}

		// Wait a bit
		select {
		case <-time.After(delay):
		// Delay over, just continue
		case <-ctx.Done():
			// We're asked to stop
			return
		case <-s.interruptChan:
			// We're being interrupted
			log.Debug("Being interrupted")
			// continue now
		}
	}
}

// Get the last known URL of the master (can be empty)
func (s *runtimeClusterManager) GetMasterURL() string {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.lastMasterURL
}

// AvoidBeingMaster instructs the runtime cluster manager to avoid
// becoming master and when it is master, to give that up.
func (s *runtimeClusterManager) AvoidBeingMaster() {
	s.avoidBeingMaster = true
	s.Interrupt()
}

// Interrupt the runtime cluster manager loop after some event has happened.
func (s *runtimeClusterManager) Interrupt() {
	// Interrupt loop so we act on this right away
	if ch := s.interruptChan; ch != nil {
		ch <- struct{}{}
	}
}
