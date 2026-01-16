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
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	driver_v1 "github.com/arangodb/go-driver"
	"github.com/arangodb/go-driver/agency"
	driverV1HTTP "github.com/arangodb/go-driver/http"
	driver "github.com/arangodb/go-driver/v2/arangodb"
	driverConnection "github.com/arangodb/go-driver/v2/connection"
	driverJwt "github.com/arangodb/go-driver/v2/utils/jwt"

	"github.com/arangodb-helper/arangodb/pkg/definitions"
	"github.com/arangodb-helper/arangodb/service/options"
)

// ClusterConfig contains all the information of a cluster from a starter's point of view.
// When this type (or any of the types used in here) is changed, increase `SetupConfigVersion`.
type ClusterConfig struct {
	AllPeers            []Peer                    `json:"Peers"` // All peers
	AgencySize          int                       // Number of agents
	LastModified        *time.Time                `json:"LastModified,omitempty"`        // Time of last modification
	PortOffsetIncrement int                       `json:"PortOffsetIncrement,omitempty"` // Increment of port offsets for peers on same address
	ServerStorageEngine string                    `json:"ServerStorageEngine,omitempty"` // Storage engine being used
	PersistentOptions   options.PersistentOptions `json:"PersistentOptions,omitempty"`   // Options which were used during first start of DB and can't be changed anymore
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

func (p *ClusterConfig) IsPortOffsetInUse() bool {
	for _, x := range p.AllPeers {
		if x.PortOffset != 0 {
			return true
		}
	}
	return false
}

// Initialize a new cluster configuration
func (p *ClusterConfig) Initialize(initialPeer Peer, agencySize int, storageEngine string, persistentOptions options.PersistentOptions) {
	p.AllPeers = []Peer{initialPeer}
	p.AgencySize = agencySize
	p.PortOffsetIncrement = definitions.PortOffsetIncrementNew
	p.ServerStorageEngine = storageEngine
	p.PersistentOptions = persistentOptions
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

// ForEachPeer updates all peers using predicate
func (p *ClusterConfig) ForEachPeer(updateFunc func(p Peer) Peer) {
	for i, peer := range p.AllPeers {
		p.AllPeers[i] = updateFunc(peer)
	}
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
	for _, peer := range p.AllPeers {
		// Validate peer has valid Address and Port before creating endpoint
		// Empty Address or Port 0 indicates peer is not yet fully initialized
		if peer.Address == "" || peer.Port == 0 {
			// Skip peers that are not yet fully initialized
			// This prevents returning empty arrays when peers exist but aren't ready
			continue
		}
		port := peer.Port + peer.PortOffset
		scheme := NewURLSchemes(peer.IsSecure).Browser
		ep := fmt.Sprintf("%s://%s", scheme, net.JoinHostPort(peer.Address, strconv.Itoa(port)))
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

// GetSingleEndpoints creates a list of URL's for all single servers.
func (p ClusterConfig) GetSingleEndpoints() ([]string, error) {
	// Build endpoint list
	var endpoints []string
	for _, p := range p.AllPeers {
		port := p.Port + p.PortOffset + definitions.ServerType(definitions.ServerTypeSingle).PortOffset()
		scheme := NewURLSchemes(p.IsSecure).Browser
		ep := fmt.Sprintf("%s://%s", scheme, net.JoinHostPort(p.Address, strconv.Itoa(port)))
		endpoints = append(endpoints, ep)

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

	// Create a v1 connection for agency operations since agency package requires v1 Connection interface
	// Get JWT secret from the client builder if available
	var jwtSecret string
	if s, ok := clientBuilder.(*Service); ok {
		jwtSecret = s.jwtSecret
	}

	connConfig := driverV1HTTP.ConnectionConfig{
		Endpoints:          endpoints,
		DontFollowRedirect: false,
		TLSConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}

	conn, err := driverV1HTTP.NewConnection(connConfig)
	if err != nil {
		return nil, maskAny(err)
	}

	// Set authentication if JWT secret is available
	if jwtSecret != "" {
		// Use v2 JWT package for creating auth header (it's compatible)
		jwtBearer, err := driverJwt.CreateArangodJwtAuthorizationHeader(jwtSecret, "starter")
		if err != nil {
			return nil, maskAny(err)
		}
		auth := driver_v1.RawAuthentication(jwtBearer)
		conn, err = conn.SetAuthentication(auth)
		if err != nil {
			return nil, maskAny(err)
		}
	}

	a, err := agency.NewAgency(conn)
	if err != nil {
		return nil, maskAny(err)
	}
	return a, nil
}

// CreateClusterAPI creates a client for the cluster
func (p ClusterConfig) CreateClusterAPI(ctx context.Context, clientBuilder ClientBuilder) (driver.ClientAdminCluster, error) {
	// Build endpoint list
	endpoints, err := p.GetCoordinatorEndpoints()
	if err != nil {
		return nil, maskAny(err)
	}
	c, err := clientBuilder.CreateClient(endpoints, ConnectionTypeDatabase, definitions.ServerTypeUnknown)
	if err != nil {
		return nil, maskAny(err)
	}
	// In v2, Client embeds ClientAdmin which embeds ClientAdminCluster
	// ClientAdminCluster methods are directly available on Client
	// Since Client implements ClientAdminCluster interface, we can return it directly
	// But we need to access it through the ClientAdmin interface
	var cluster driver.ClientAdminCluster = c
	return cluster, nil
}

// CreateCoordinatorsClient creates go-driver client targeting the coordinators.
func (p ClusterConfig) CreateCoordinatorsClient(jwtSecret string) (driver.Client, error) {
	// Build endpoint list
	endpoints, err := p.GetCoordinatorEndpoints()
	if err != nil {
		return nil, maskAny(err)
	}
	endpoint := driverConnection.NewRoundRobinEndpoints(endpoints)
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	connConfig := driverConnection.HttpConfiguration{
		Endpoint:  endpoint,
		Transport: transport,
	}
	conn := driverConnection.NewHttpConnection(connConfig)
	if jwtSecret != "" {
		value, err := driverJwt.CreateArangodJwtAuthorizationHeader(jwtSecret, "starter")
		if err != nil {
			return nil, maskAny(err)
		}
		auth := driverConnection.NewHeaderAuth("Authorization", value)
		if err := conn.SetAuthentication(auth); err != nil {
			return nil, maskAny(err)
		}
	}
	c := driver.NewClient(conn)
	return c, nil
}

// Set the LastModified timestamp to now.
func (p *ClusterConfig) updateLastModified() {
	ts := time.Now()
	p.LastModified = &ts
}
