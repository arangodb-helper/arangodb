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
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/arangodb-helper/arangodb/service/arangod"
)

// ClusterConfig contains all the informtion of a cluster from a starter's point of view.
// When this type (or any of the types used in here) is changed, increase `SetupConfigVersion`.
type ClusterConfig struct {
	AllPeers     []Peer     `json:"Peers"` // All peers
	AgencySize   int        // Number of agents
	LastModified *time.Time `json:"LastModified,omitempty"` // Time of last modification
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
func (p *ClusterConfig) Initialize(initialPeer Peer, agencySize int) {
	p.AllPeers = []Peer{initialPeer}
	p.AgencySize = agencySize
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
	for {
		found := false
		for _, p := range p.AllPeers {
			if p.PortRangeOverlaps(basePort + portOffset) {
				if allPortOffsetsUnique || p.Address == peerAddress {
					found = true
					break
				}
			}
		}
		if !found {
			return portOffset
		}
		portOffset += portOffsetIncrement
	}
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
func (p ClusterConfig) GetPeerEndpoints() ([]url.URL, error) {
	// Build endpoint list
	var endpoints []url.URL
	for _, p := range p.AllPeers {
		port := p.Port + p.PortOffset
		scheme := NewURLSchemes(p.IsSecure).Browser
		u, err := url.Parse(fmt.Sprintf("%s://%s", scheme, net.JoinHostPort(p.Address, strconv.Itoa(port))))
		if err != nil {
			return nil, maskAny(err)
		}
		endpoints = append(endpoints, *u)
	}
	return endpoints, nil
}

// GetAgentEndpoints creates a list of URL's for all agents.
func (p ClusterConfig) GetAgentEndpoints() ([]url.URL, error) {
	// Build endpoint list
	var endpoints []url.URL
	for _, p := range p.AllPeers {
		if p.HasAgent() {
			port := p.Port + p.PortOffset + ServerType(ServerTypeAgent).PortOffset()
			scheme := NewURLSchemes(p.IsSecure).Browser
			u, err := url.Parse(fmt.Sprintf("%s://%s", scheme, net.JoinHostPort(p.Address, strconv.Itoa(port))))
			if err != nil {
				return nil, maskAny(err)
			}
			endpoints = append(endpoints, *u)
		}
	}
	return endpoints, nil
}

// GetCoordinatorEndpoints creates a list of URL's for all coordinators.
func (p ClusterConfig) GetCoordinatorEndpoints() ([]url.URL, error) {
	// Build endpoint list
	var endpoints []url.URL
	for _, p := range p.AllPeers {
		if p.HasCoordinator() {
			port := p.Port + p.PortOffset + ServerType(ServerTypeCoordinator).PortOffset()
			scheme := NewURLSchemes(p.IsSecure).Browser
			u, err := url.Parse(fmt.Sprintf("%s://%s", scheme, net.JoinHostPort(p.Address, strconv.Itoa(port))))
			if err != nil {
				return nil, maskAny(err)
			}
			endpoints = append(endpoints, *u)
		}
	}
	return endpoints, nil
}

// CreateAgencyAPI creates a client for the agency
func (p ClusterConfig) CreateAgencyAPI(prepareRequest func(*http.Request) error) (arangod.AgencyAPI, error) {
	// Build endpoint list
	endpoints, err := p.GetAgentEndpoints()
	if err != nil {
		return nil, maskAny(err)
	}
	c, err := arangod.NewClusterClient(endpoints, prepareRequest)
	if err != nil {
		return nil, maskAny(err)
	}
	return c.Agency(), nil
}

// CreateClusterAPI creates a client for the cluster
func (p ClusterConfig) CreateClusterAPI(prepareRequest func(*http.Request) error) (arangod.ClusterAPI, error) {
	// Build endpoint list
	endpoints, err := p.GetCoordinatorEndpoints()
	if err != nil {
		return nil, maskAny(err)
	}
	c, err := arangod.NewClusterClient(endpoints, prepareRequest)
	if err != nil {
		return nil, maskAny(err)
	}
	return c.Cluster(), nil
}

// Set the LastModified timestamp to now.
func (p *ClusterConfig) updateLastModified() {
	ts := time.Now()
	p.LastModified = &ts
}
