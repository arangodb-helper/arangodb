//
// DISCLAIMER
//
// Copyright 2017-2021 ArangoDB GmbH, Cologne, Germany
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
// Author Tomasz Mielech
//

package service

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	driver "github.com/arangodb/go-driver"
	"github.com/arangodb/go-driver/agency"
	driver_http "github.com/arangodb/go-driver/http"
	"github.com/arangodb/go-driver/jwt"

	"github.com/arangodb-helper/arangodb/pkg/definitions"
)

// ClusterConfig contains all the informtion of a cluster from a starter's point of view.
// When this type (or any of the types used in here) is changed, increase `SetupConfigVersion`.
type ClusterConfig struct {
	AllPeers            []Peer     `json:"Peers"` // All peers
	AgencySize          int        // Number of agents
	LastModified        *time.Time `json:"LastModified,omitempty"`        // Time of last modification
	PortOffsetIncrement int        `json:"PortOffsetIncrement,omitempty"` // Increment of port offsets for peers on same address
	ServerStorageEngine string     `json:ServerStorageEngine,omitempty"`  // Storage engine being used
}

// PeerByID returns a peer with given id & true, or false if not found.
func (p ClusterConfig) PeerByID(id string) (Peer, bool) {
	for _, x := range p.AllPeers {
		if x.ID == id {
			return x, true
		}
	}
	return Peer{}, false
}

// PeerByAddressAndPort returns a peer with given address, port & true, or false if not found.
func (p ClusterConfig) PeerByAddressAndPort(address string, port int) (Peer, bool) {
	address = strings.ToLower(address)
	for _, x := range p.AllPeers {
		if x.Port+x.PortOffset == port && strings.ToLower(x.Address) == address {
			return x, true
		}
	}
	return Peer{}, false
}

// AllAgents returns a list of all peers that have an agent.
func (p ClusterConfig) AllAgents() []Peer {
	var result []Peer
	for _, x := range p.AllPeers {
		if x.HasAgent() {
			result = append(result, x)
		}
	}
	return result
}

// Initialize a new cluster configuration
func (p *ClusterConfig) Initialize(initialPeer Peer, agencySize int, storageEngine string) {
	p.AllPeers = []Peer{initialPeer}
	p.AgencySize = agencySize
	p.PortOffsetIncrement = definitions.PortOffsetIncrementNew
	p.ServerStorageEngine = storageEngine
	p.updateLastModified()
}

// UpdatePeerByID updates the peer with given id & true, or false if not found.
func (p *ClusterConfig) UpdatePeerByID(update Peer) bool {
	for index, x := range p.AllPeers {
		if x.ID == update.ID {
			p.AllPeers[index] = update
			p.updateLastModified()
			return true
		}
	}
	return false
}

// AddPeer adds the given peer to the list of all peers, only if the id is not yet one of the peers.
// Returns true of success, false otherwise
func (p *ClusterConfig) AddPeer(newPeer Peer) bool {
	for _, x := range p.AllPeers {
		if x.ID == newPeer.ID {
			return false
		}
	}
	p.AllPeers = append(p.AllPeers, newPeer)
	p.updateLastModified()
	return true
}

// RemovePeerByID removes the peer with given ID.
func (p *ClusterConfig) RemovePeerByID(id string) bool {
	newPeers := make([]Peer, 0, len(p.AllPeers))
	found := false
	for _, x := range p.AllPeers {
		if x.ID != id {
			newPeers = append(newPeers, x)
		} else {
			found = true
		}
	}
	if found {
		p.AllPeers = newPeers
		p.updateLastModified()
	}
	return found
}

// IDs returns the IDs of all peers.
func (p ClusterConfig) IDs() []string {
	list := make([]string, 0, len(p.AllPeers))
	for _, x := range p.AllPeers {
		list = append(list, x.ID)
	}
	return list
}

// GetFreePortOffset returns the first unallocated port offset.
func (p ClusterConfig) GetFreePortOffset(peerAddress string, basePort int, allPortOffsetsUnique bool) int {
	portOffset := 0
	peerAddress = normalizeHostName(peerAddress)
	for {
		found := false
		for _, peer := range p.AllPeers {
			if peer.PortRangeOverlaps(basePort+portOffset, p) {
				if allPortOffsetsUnique || normalizeHostName(peer.Address) == peerAddress {
					found = true
					break
				}
			}
		}
		if !found {
			return portOffset
		}
		portOffset = p.NextPortOffset(portOffset)
	}
}

// NextPortOffset returns the next port offset (from given offset)
func (p ClusterConfig) NextPortOffset(portOffset int) int {
	if p.PortOffsetIncrement == 0 {
		return portOffset + definitions.PortOffsetIncrementOld
	}
	return portOffset + definitions.PortOffsetIncrementNew
}

// HaveEnoughAgents returns true when the number of peers that have an agent
// is greater or equal to AgencySize.
func (p ClusterConfig) HaveEnoughAgents() bool {
	count := 0
	for _, x := range p.AllPeers {
		if x.HasAgent() {
			count++
		}
	}
	return count >= p.AgencySize
}

// IsSecure returns true if any of the peers is secure.
func (p ClusterConfig) IsSecure() bool {
	for _, x := range p.AllPeers {
		if x.IsSecure {
			return true
		}
	}
	return false
}

// GetPeerEndpoints creates a list of URL's for all peer.
func (p ClusterConfig) GetPeerEndpoints() ([]string, error) {
	// Build endpoint list
	var endpoints []string
	for _, p := range p.AllPeers {
		port := p.Port + p.PortOffset
		scheme := NewURLSchemes(p.IsSecure).Browser
		ep := fmt.Sprintf("%s://%s", scheme, net.JoinHostPort(p.Address, strconv.Itoa(port)))
		endpoints = append(endpoints, ep)
	}
	return endpoints, nil
}

// GetAgentEndpoints creates a list of URL's for all agents.
func (p ClusterConfig) GetAgentEndpoints() ([]string, error) {
	// Build endpoint list
	var endpoints []string
	for _, p := range p.AllPeers {
		if p.HasAgent() {
			port := p.Port + p.PortOffset + definitions.ServerType(definitions.ServerTypeAgent).PortOffset()
			scheme := NewURLSchemes(p.IsSecure).Browser
			ep := fmt.Sprintf("%s://%s", scheme, net.JoinHostPort(p.Address, strconv.Itoa(port)))
			endpoints = append(endpoints, ep)
		}
	}
	return endpoints, nil
}

// GetDBServerEndpoints creates a list of URL's for all dbservers.
func (p ClusterConfig) GetDBServerEndpoints() ([]string, error) {
	// Build endpoint list
	var endpoints []string
	for _, p := range p.AllPeers {
		if p.HasDBServer() {
			port := p.Port + p.PortOffset + definitions.ServerType(definitions.ServerTypeDBServer).PortOffset()
			scheme := NewURLSchemes(p.IsSecure).Browser
			ep := fmt.Sprintf("%s://%s", scheme, net.JoinHostPort(p.Address, strconv.Itoa(port)))
			endpoints = append(endpoints, ep)
		}
	}
	return endpoints, nil
}

// GetCoordinatorEndpoints creates a list of URL's for all coordinators.
func (p ClusterConfig) GetCoordinatorEndpoints() ([]string, error) {
	// Build endpoint list
	var endpoints []string
	for _, p := range p.AllPeers {
		if p.HasCoordinator() {
			port := p.Port + p.PortOffset + definitions.ServerType(definitions.ServerTypeCoordinator).PortOffset()
			scheme := NewURLSchemes(p.IsSecure).Browser
			ep := fmt.Sprintf("%s://%s", scheme, net.JoinHostPort(p.Address, strconv.Itoa(port)))
			endpoints = append(endpoints, ep)
		}
	}
	return endpoints, nil
}

// GetAllSingleEndpoints creates a list of URL's for all single servers.
func (p ClusterConfig) GetAllSingleEndpoints() ([]string, error) {
	result, err := p.GetSingleEndpoints(true)
	if err != nil {
		return nil, maskAny(err)
	}
	return result, nil
}

// GetResilientSingleEndpoints creates a list of URL's for all resilient single servers.
func (p ClusterConfig) GetResilientSingleEndpoints() ([]string, error) {
	return p.GetSingleEndpoints(false)
}

// GetSingleEndpoints creates a list of URL's for all single servers.
func (p ClusterConfig) GetSingleEndpoints(all bool) ([]string, error) {
	// Build endpoint list
	var endpoints []string
	for _, p := range p.AllPeers {
		if all || p.HasResilientSingle() {
			port := p.Port + p.PortOffset + definitions.ServerType(definitions.ServerTypeSingle).PortOffset()
			scheme := NewURLSchemes(p.IsSecure).Browser
			ep := fmt.Sprintf("%s://%s", scheme, net.JoinHostPort(p.Address, strconv.Itoa(port)))
			endpoints = append(endpoints, ep)
		}
	}
	return endpoints, nil
}

// GetSyncMasterEndpoints creates a list of URL's for all sync masters.
func (p ClusterConfig) GetSyncMasterEndpoints() ([]string, error) {
	// Build endpoint list
	var endpoints []string
	for _, p := range p.AllPeers {
		if p.HasSyncMaster() {
			port := p.Port + p.PortOffset + definitions.ServerType(definitions.ServerTypeSyncMaster).PortOffset()
			ep := fmt.Sprintf("https://%s", net.JoinHostPort(p.Address, strconv.Itoa(port)))
			endpoints = append(endpoints, ep)
		}
	}
	return endpoints, nil
}

// CreateAgencyAPI creates a client for the agency
func (p ClusterConfig) CreateAgencyAPI(clientBuilder ClientBuilder) (agency.Agency, error) {
	// Build endpoint list
	endpoints, err := p.GetAgentEndpoints()
	if err != nil {
		return nil, maskAny(err)
	}
	c, err := clientBuilder.CreateClient(endpoints, ConnectionTypeAgency, definitions.ServerTypeUnknown)
	if err != nil {
		return nil, maskAny(err)
	}
	conn := c.Connection()
	a, err := agency.NewAgency(conn)
	if err != nil {
		return nil, maskAny(err)
	}
	return a, nil
}

// CreateClusterAPI creates a client for the cluster
func (p ClusterConfig) CreateClusterAPI(ctx context.Context, clientBuilder ClientBuilder) (driver.Cluster, error) {
	// Build endpoint list
	endpoints, err := p.GetCoordinatorEndpoints()
	if err != nil {
		return nil, maskAny(err)
	}
	c, err := clientBuilder.CreateClient(endpoints, ConnectionTypeDatabase, definitions.ServerTypeUnknown)
	if err != nil {
		return nil, maskAny(err)
	}
	cluster, err := c.Cluster(ctx)
	if err != nil {
		return nil, maskAny(err)
	}
	return cluster, nil
}

// CreateCoordinatorsClient creates go-driver client targeting the coordinators.
func (p ClusterConfig) CreateCoordinatorsClient(jwtSecret string) (driver.Client, error) {
	// Build endpoint list
	endpoints, err := p.GetCoordinatorEndpoints()
	if err != nil {
		return nil, maskAny(err)
	}
	conn, err := driver_http.NewConnection(driver_http.ConnectionConfig{
		Endpoints: endpoints,
		TLSConfig: &tls.Config{InsecureSkipVerify: true},
	})
	if err != nil {
		return nil, maskAny(err)
	}
	options := driver.ClientConfig{
		Connection: conn,
	}
	if jwtSecret != "" {
		value, err := jwt.CreateArangodJwtAuthorizationHeader(jwtSecret, "starter")
		if err != nil {
			return nil, maskAny(err)
		}
		options.Authentication = driver.RawAuthentication(value)
	}
	c, err := driver.NewClient(options)
	if err != nil {
		return nil, maskAny(err)
	}
	return c, nil
}

// Set the LastModified timestamp to now.
func (p *ClusterConfig) updateLastModified() {
	ts := time.Now()
	p.LastModified = &ts
}
