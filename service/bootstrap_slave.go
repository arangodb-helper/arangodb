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
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"strconv"
	"time"
)

// bootstrapSlave starts the Service as slave and begins bootstrapping the cluster from nothing.
func (s *Service) bootstrapSlave(peerAddress string, runner Runner, config Config, bsCfg BootstrapConfig) {
	masterPort := config.MasterPort
	if host, port, err := net.SplitHostPort(peerAddress); err == nil {
		peerAddress = host
		masterPort, _ = strconv.Atoi(port)
	}
	for {
		masterAddr := net.JoinHostPort(peerAddress, strconv.Itoa(masterPort))
		s.log.Infof("Contacting master %s...", masterAddr)
		_, hostPort, err := s.getHTTPServerPort()
		if err != nil {
			s.log.Fatalf("Failed to get HTTP server port: %#v", err)
		}
		b, _ := json.Marshal(HelloRequest{
			DataDir:      config.DataDir,
			SlaveID:      s.id,
			SlaveAddress: config.OwnAddress,
			SlavePort:    hostPort,
			IsSecure:     s.IsSecure(),
			Agent:        copyBoolRef(bsCfg.StartAgent),
			DBServer:     copyBoolRef(bsCfg.StartDBserver),
			Coordinator:  copyBoolRef(bsCfg.StartCoordinator),
		})
		buf := bytes.Buffer{}
		buf.Write(b)
		scheme := NewURLSchemes(s.IsSecure()).Browser
		r, e := httpClient.Post(fmt.Sprintf("%s://%s/hello", scheme, masterAddr), "application/json", &buf)
		if e != nil {
			s.log.Infof("Cannot start because of error from master: %v", e)
			time.Sleep(time.Second)
			continue
		}

		body, e := ioutil.ReadAll(r.Body)
		defer r.Body.Close()
		if e != nil {
			s.log.Infof("Cannot start because HTTP response from master was bad: %v", e)
			time.Sleep(time.Second)
			continue
		}

		if r.StatusCode != http.StatusOK {
			var errResp ErrorResponse
			json.Unmarshal(body, &errResp)
			s.log.Fatalf("Cannot start because of HTTP error from master: code=%d, message=%s\n", r.StatusCode, errResp.Error)
		}
		e = json.Unmarshal(body, &s.myPeers)
		if e != nil {
			s.log.Warningf("Cannot parse body from master: %v", e)
			return
		}
		//s.AgencySize = s.myPeers.AgencySize
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

// sendMasterGoodbye informs the master that we're leaving for good.
func (s *Service) sendMasterGoodbye() error {
	master := s.myPeers.AllPeers[0] // TODO replace with bootstrap master
	if s.id == master.ID {
		// I'm the master, do nothing
		return nil
	}
	u := master.CreateStarterURL("/goodbye")
	s.log.Infof("Saying goodbye to master at %s", u)
	req := GoodbyeRequest{SlaveID: s.id}
	data, err := json.Marshal(req)
	if err != nil {
		return maskAny(err)
	}
	resp, err := httpClient.Post(u, "application/json", bytes.NewReader(data))
	if err != nil {
		return maskAny(err)
	}
	if resp.StatusCode != http.StatusOK {
		return maskAny(fmt.Errorf("Invalid status %d", resp.StatusCode))
	}
	return nil
}
