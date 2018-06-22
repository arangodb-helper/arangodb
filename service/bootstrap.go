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

// fetchIDFromPeer tries to get the ID through given client API.
// When ID is received it is send in the given channel.
func fetchIDFromPeer(ctx context.Context, peerClient client.API, idChan chan string) {
	defer close(idChan)
	for {
		if idInfo, err := peerClient.ID(ctx); err == nil {
			idChan <- idInfo.ID
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

	// Create server
	srv, containerPort, hostAddr, containerAddr, err := s.createHTTPServer(cfg)
	if err != nil {
		return false, maskAny(err)
	}
	defer srv.Close()

	// Run HTTP server until signalled
	serverErrors := make(chan error)
	go func() {
		defer close(serverErrors)
		idOnly := true
		if err := srv.Run(hostAddr, containerAddr, s.tlsConfig, idOnly); err != nil {
			serverErrors <- err
		}
	}()

	// Fetch ID of local peer until success or timeout,
	// just so we know our server is up.
	localURL := *peerURL // Create copy
	localURL.Host = net.JoinHostPort("localhost", strconv.Itoa(containerPort))
	localPeerClient, err := client.NewArangoStarterClient(localURL)
	if err != nil {
		return false, maskAny(err)
	}
	ctx, cancel := context.WithTimeout(rootCtx, time.Second*10)
	defer cancel()
	localIDFound := make(chan string)
	go fetchIDFromPeer(ctx, localPeerClient, localIDFound)

	// Wait until we successfully fetched our local ID
	select {
	case <-localIDFound:
		s.log.Info().Msg("Found ID from localhost peer")
		// We can continue
	case err := <-serverErrors:
		s.log.Info().Err(err).Msg("Error while trying to run HTTP server")
		return false, nil
	}

	// Fetch ID of given peer until success or timeout
	peerClient, err := client.NewArangoStarterClient(*peerURL)
	if err != nil {
		return false, maskAny(err)
	}
	idFound := make(chan string)
	go fetchIDFromPeer(ctx, peerClient, idFound)

	// Wait until a ID is found or we have a server run error.
	select {
	case id := <-idFound:
		s.log.Info().Msgf("Found ID '%s' from peer, looking for '%s'", id, s.id)
		return s.id == id, nil
	case err := <-serverErrors:
		s.log.Info().Err(err).Msg("Error while trying to run HTTP server")
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
