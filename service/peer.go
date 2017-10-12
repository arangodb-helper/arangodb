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

	"github.com/arangodb-helper/arangodb/service/arangod"
)

// Peer contains all persistent settings of a starter.
type Peer struct {
	ID                     string // Unique of of the peer
	Address                string // IP address of arangodb peer server
	Port                   int    // Port number of arangodb peer server
	PortOffset             int    // Offset to add to base ports for the various servers (agent, coordinator, dbserver)
	DataDir                string // Directory holding my data
	HasAgentFlag           bool   `json:"HasAgent"`                     // If set, this peer is running an agent
	HasDBServerFlag        *bool  `json:"HasDBServer,omitempty"`        // If set or is nil, this peer is running a dbserver
	HasCoordinatorFlag     *bool  `json:"HasCoordinator,omitempty"`     // If set or is nil, this peer is running a coordinator
	HasResilientSingleFlag bool   `json:"HasResilientSingle,omitempty"` // If set, this peer is running a resilient single server
	IsSecure               bool   // If set, servers started by this peer are using an SSL connection
}

// NewPeer initializes a new Peer instance with given values.
func NewPeer(id, address string, port, portOffset int, dataDir string, hasAgent, hasDBServer, hasCoordinator, hasResilientSingle, isSecure bool) Peer {
	p := Peer{
		ID:                     id,
		Address:                address,
		Port:                   port,
		PortOffset:             portOffset,
		DataDir:                dataDir,
		HasAgentFlag:           hasAgent,
		IsSecure:               isSecure,
		HasResilientSingleFlag: hasResilientSingle,
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

// HasResilientSingle returns true if this peer is running an resilient single server
func (p Peer) HasResilientSingle() bool { return p.HasResilientSingleFlag }

// CreateStarterURL creates a URL to the relative path to the starter on this peer.
func (p Peer) CreateStarterURL(relPath string) string {
	addr := net.JoinHostPort(p.Address, strconv.Itoa(p.Port+p.PortOffset))
	relPath = strings.TrimPrefix(relPath, "/")
	scheme := NewURLSchemes(p.IsSecure).Browser
	return fmt.Sprintf("%s://%s/%s", scheme, addr, relPath)
}

// CreateDBServerAPI creates a client for the dbserver of the peer
func (p Peer) CreateDBServerAPI(prepareRequest func(*http.Request) error) (arangod.ServerAPI, error) {
	if p.HasDBServer() {
		port := p.Port + p.PortOffset + ServerType(ServerTypeDBServer).PortOffset()
		scheme := NewURLSchemes(p.IsSecure).Browser
		u, err := url.Parse(fmt.Sprintf("%s://%s", scheme, net.JoinHostPort(p.Address, strconv.Itoa(port))))
		if err != nil {
			return nil, maskAny(err)
		}
		c, err := arangod.NewServerClient(*u, prepareRequest, true)
		if err != nil {
			return nil, maskAny(err)
		}
		return c.Server()
	} else {
		return nil, maskAny(fmt.Errorf("Peer has no dbserver"))
	}
}

// CreateCoordinatorAPI creates a client for the coordinator of the peer
func (p Peer) CreateCoordinatorAPI(prepareRequest func(*http.Request) error) (arangod.ServerAPI, error) {
	if p.HasCoordinator() {
		port := p.Port + p.PortOffset + ServerType(ServerTypeCoordinator).PortOffset()
		scheme := NewURLSchemes(p.IsSecure).Browser
		u, err := url.Parse(fmt.Sprintf("%s://%s", scheme, net.JoinHostPort(p.Address, strconv.Itoa(port))))
		if err != nil {
			return nil, maskAny(err)
		}
		c, err := arangod.NewServerClient(*u, prepareRequest, true)
		if err != nil {
			return nil, maskAny(err)
		}
		return c.Server()
	} else {
		return nil, maskAny(fmt.Errorf("Peer has no coordinator"))
	}
}

// PortRangeOverlaps returns true if the port range of this peer overlaps with a port
// range starting at given port.
func (p Peer) PortRangeOverlaps(otherPort int) bool {
	myStart := p.Port + p.PortOffset                // Inclusive
	myEnd := myStart + portOffsetIncrement - 1      // Inclusive
	otherEnd := otherPort + portOffsetIncrement - 1 // Inclusive

	return (otherPort >= myStart && otherPort <= myEnd) ||
		(otherEnd >= myStart && otherEnd <= myEnd)
}
