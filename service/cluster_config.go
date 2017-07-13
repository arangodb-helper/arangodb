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
	"strings"

	"github.com/arangodb-helper/arangodb/service/agency"
)

// Peer contains all persistent settings of a starter.
type Peer struct {
	ID                 string // Unique of of the peer
	Address            string // IP address of arangodb peer server
	Port               int    // Port number of arangodb peer server
	PortOffset         int    // Offset to add to base ports for the various servers (agent, coordinator, dbserver)
	DataDir            string // Directory holding my data
	HasAgentFlag       bool   `json:"HasAgent"`                 // If set, this peer is running an agent
	HasDBServerFlag    *bool  `json:"HasDBServer,omitempty"`    // If set or is nil, this peer is running a dbserver
	HasCoordinatorFlag *bool  `json:"HasCoordinator,omitempty"` // If set or is nil, this peer is running a coordinator
	IsSecure           bool   // If set, servers started by this peer are using an SSL connection
}

// NewPeer initializes a new Peer instance with given values.
func NewPeer(id, address string, port, portOffset int, dataDir string, hasAgent, hasDBServer, hasCoordinator, isSecure bool) Peer {
	p := Peer{
		ID:           id,
		Address:      address,
		Port:         port,
		PortOffset:   portOffset,
		DataDir:      dataDir,
		HasAgentFlag: hasAgent,
		IsSecure:     isSecure,
	}
	if !hasDBServer {
		p.HasDBServerFlag = boolRef(false)
	}
	if !hasCoordinator {
		p.HasCoordinatorFlag = boolRef(false)
	}
	return p
}

// HasAgent returns true if this peer is running an agent
func (p Peer) HasAgent() bool { return p.HasAgentFlag }

// HasDBServer returns true if this peer is running a dbserver
func (p Peer) HasDBServer() bool { return p.HasDBServerFlag == nil || *p.HasDBServerFlag }

// HasCoordinator returns true if this peer is running a coordinator
func (p Peer) HasCoordinator() bool { return p.HasCoordinatorFlag == nil || *p.HasCoordinatorFlag }

// CreateStarterURL creates a URL to the relative path to the starter on this peer.
func (p Peer) CreateStarterURL(relPath string) string {
	addr := net.JoinHostPort(p.Address, strconv.Itoa(p.Port+p.PortOffset))
	relPath = strings.TrimPrefix(relPath, "/")
	scheme := NewURLSchemes(p.IsSecure).Browser
	return fmt.Sprintf("%s://%s/%s", scheme, addr, relPath)
}

// ClusterConfig contains all the informtion of a cluster from a starter's point of view.
// When this type (or any of the types used in here) is changed, increase `SetupConfigVersion`.
type ClusterConfig struct {
	Peers      []Peer // All peers (index 0 is reserver for the master)
	AgencySize int    // Number of agents
}

// PeerByID returns a peer with given id & true, or false if not found.
func (p ClusterConfig) PeerByID(id string) (Peer, bool) {
	for _, x := range p.Peers {
		if x.ID == id {
			return x, true
		}
	}
	return Peer{}, false
}

// UpdatePeerByID updates the peer with given id & true, or false if not found.
func (p *ClusterConfig) UpdatePeerByID(update Peer) bool {
	for index, x := range p.Peers {
		if x.ID == update.ID {
			p.Peers[index] = update
			return true
		}
	}
	return false
}

// RemovePeerByID removes the peer with given ID.
func (p *ClusterConfig) RemovePeerByID(id string) bool {
	newPeers := make([]Peer, 0, len(p.Peers))
	found := false
	for _, x := range p.Peers {
		if x.ID != id {
			newPeers = append(newPeers, x)
		} else {
			found = true
		}
	}
	p.Peers = newPeers
	return found
}

// IDs returns the IDs of all peers.
func (p ClusterConfig) IDs() []string {
	list := make([]string, 0, len(p.Peers))
	for _, x := range p.Peers {
		list = append(list, x.ID)
	}
	return list
}

// GetFreePortOffset returns the first unallocated port offset.
func (p ClusterConfig) GetFreePortOffset(peerAddress string, allPortOffsetsUnique bool) int {
	portOffset := 0
	for {
		found := false
		for _, p := range p.Peers {
			if p.PortOffset == portOffset {
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
	for _, x := range p.Peers {
		if x.HasAgent() {
			count++
		}
	}
	return count >= p.AgencySize
}

// IsSecure returns true if any of the peers is secure.
func (p ClusterConfig) IsSecure() bool {
	for _, x := range p.Peers {
		if x.IsSecure {
			return true
		}
	}
	return false
}

// CreateAgencyAPI creates a client for the agency
func (p ClusterConfig) CreateAgencyAPI(prepareRequest func(*http.Request) error) (agency.API, error) {
	// Build endpoint list
	var endpoints []url.URL
	for _, p := range p.Peers {
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
	return agency.NewAgencyClient(endpoints, prepareRequest)
}
