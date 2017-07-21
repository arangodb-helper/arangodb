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
	"fmt"
	"net"
	"net/url"
	"sort"
	"strconv"
	"time"

	"github.com/arangodb-helper/arangodb/client"
)

// createBootstrapMasterURL creates a URL from a given peer address.
// Allowed formats for peer address are:
// <host>
// <host>:<port>
func (s *Service) createBootstrapMasterURL(peerAddress string, cfg Config) string {
	masterPort := cfg.MasterPort
	if host, port, err := net.SplitHostPort(peerAddress); err == nil {
		peerAddress = host
		masterPort, _ = strconv.Atoi(port)
	}
	masterAddr := net.JoinHostPort(peerAddress, strconv.Itoa(masterPort))
	scheme := NewURLSchemes(s.IsSecure()).Browser
	return fmt.Sprintf("%s://%s", scheme, masterAddr)
}

// isPeerAddressMyself starts a local server and then makes an ID call
// to the given peer address.
// Returns true if ID calls succeeds and is equal to own ID.
func (s *Service) isPeerAddressMyself(rootCtx context.Context, peerAddr string, cfg Config) (bool, error) {
	// Create peer URL
	peerURL, err := url.Parse(s.createBootstrapMasterURL(peerAddr, cfg))
	if err != nil {
		return false, maskAny(err)
	}
	peerClient, err := client.NewArangoStarterClient(*peerURL)
	if err != nil {
		return false, maskAny(err)
	}

	// Create server
	srv, hostAddr, containerAddr, err := s.createHTTPServer(cfg)
	if err != nil {
		return false, maskAny(err)
	}

	// Run HTTP server until signalled
	serverErrors := make(chan error)
	go func() {
		defer close(serverErrors)
		idOnly := true
		if err := srv.Run(hostAddr, containerAddr, s.tlsConfig, idOnly); err != nil {
			serverErrors <- err
		}
	}()

	// Fetch ID until success or timeout
	ctx, cancel := context.WithTimeout(rootCtx, time.Second*10)
	idFound := make(chan string)
	go func() {
		defer close(idFound)
		for {
			if idInfo, err := peerClient.ID(ctx); err == nil {
				idFound <- idInfo.ID
				return
			}
			if ctx.Err() != nil {
				return
			}
			select {
			case <-time.After(time.Millisecond * 100):
				// Retry
			case <-ctx.Done():
				return
			}
		}
	}()

	// Wait until a ID is found or we have a server run error.
	select {
	case id := <-idFound:
		cancel()
		srv.Close()
		return s.id == id, nil
	case err := <-serverErrors:
		cancel()
		srv.Close()
		s.log.Infof("Error while trying to run HTTP server: %#v", err)
		return false, nil
	}
}

// shouldActAsBootstrapMaster returns if this starter should act as
// master during the bootstrap phase of the cluster.
func (s *Service) shouldActAsBootstrapMaster(rootCtx context.Context, cfg Config) (bool, string, error) {
	masterAddrs := cfg.MasterAddresses
	switch len(masterAddrs) {
	case 0:
		// No `--starter.join` act as master
		return true, "", nil
	case 1:
		// Single `--starter.join` act as slave
		return false, masterAddrs[0], nil
	}

	// There are multiple `--starter.join` arguments.
	// We're the bootstrap master if we're the first one in the list.
	sort.Strings(masterAddrs)
	isSelf, err := s.isPeerAddressMyself(rootCtx, masterAddrs[0], cfg)
	if err != nil {
		return false, "", maskAny(err)
	}
	return isSelf, masterAddrs[0], nil
}
