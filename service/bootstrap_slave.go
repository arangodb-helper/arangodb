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
	"bytes"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/arangodb-helper/arangodb/client"
)

// bootstrapSlave starts the Service as slave and begins bootstrapping the cluster from nothing.
func (s *Service) bootstrapSlave(peerAddress string, runner Runner, config Config, bsCfg BootstrapConfig) {
	masterURL := s.createBootstrapMasterURL(peerAddress, config)
	for {
		s.log.Infof("Contacting master %s...", masterURL)
		_, hostPort, err := s.getHTTPServerPort()
		if err != nil {
			s.log.Fatalf("Failed to get HTTP server port: %#v", err)
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
			s.log.Fatalf("Failed to encode Hello request: %#v", err)
		}
		helloURL, err := getURLWithPath(masterURL, "/hello")
		if err != nil {
			s.log.Fatalf("Failed to create Hello URL: %#v", err)
		}
		r, e := httpClient.Post(helloURL, contentTypeJSON, bytes.NewReader(encoded))
		if e != nil {
			s.log.Infof("Cannot start because of error from master: %v", e)
			time.Sleep(time.Second)
			continue
		}

		body, e := ioutil.ReadAll(r.Body)
		r.Body.Close()
		if e != nil {
			s.log.Infof("Cannot start because HTTP response from master was bad: %v", e)
			time.Sleep(time.Second)
			continue
		}

		if r.StatusCode == http.StatusServiceUnavailable {
			s.log.Infof("Cannot start because service unavailable: %v", e)
			time.Sleep(time.Second)
			continue
		}

		if r.StatusCode != http.StatusOK {
			err := client.ParseResponseError(r, body)
			s.log.Fatalf("Cannot start because of HTTP error from master: code=%d, message=%s\n", r.StatusCode, err.Error())
			return
		}
		var result ClusterConfig
		if e := json.Unmarshal(body, &result); e != nil {
			s.log.Warningf("Cannot parse body from master: %v", e)
			return
		}
		// Check result
		if _, found := result.PeerByID(s.id); !found {
			s.log.Fatalf("Master responsed with cluster config that does not contain my ID, please check master")
			return
		}
		// Save cluster config
		s.myPeers = result
		break
	}

	// Check HTTP server port
	containerHTTPPort, _, err := s.getHTTPServerPort()
	if err != nil {
		s.log.Fatalf("Cannot find HTTP server info: %#v", err)
	}
	if !IsPortOpen(containerHTTPPort) {
		s.log.Fatalf("Port %d is already in use", containerHTTPPort)
	}

	// Run the HTTP service so we can forward other clients
	s.startHTTPServer(config)

	// Wait until we can start:
	if s.myPeers.AgencySize > 1 {
		s.log.Infof("Waiting for %d servers to show up...", s.myPeers.AgencySize)
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
				s.log.Errorf("Failed to connect to master: %v", err)
				time.Sleep(time.Second * 2)
			} else if r.StatusCode != 200 {
				s.log.Warningf("Invalid status received from master: %d", r.StatusCode)
			} else {
				defer r.Body.Close()
				body, _ := ioutil.ReadAll(r.Body)
				var clusterConfig ClusterConfig
				json.Unmarshal(body, &clusterConfig)
				s.myPeers = clusterConfig
			}
		}
	}

	s.log.Infof("Serving as slave with ID '%s' on %s:%d...", s.id, config.OwnAddress, s.announcePort)
	s.saveSetup()
	s.startRunning(runner, config, bsCfg)
}
