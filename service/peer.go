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
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/arangodb-helper/arangodb/pkg/definitions"

	driver "github.com/arangodb/go-driver"
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
	HasSyncMasterFlag      bool   `json:"HasSyncMaster,omitempty"`      // If set, this peer is running a sync master
	HasSyncWorkerFlag      bool   `json:"HasSyncWorker,omitempty"`      // If set, this peer is running a sync worker
	IsSecure               bool   // If set, servers started by this peer are using an SSL connection
}

// NewPeer initializes a new Peer instance with given values.
func NewPeer(id, address string, port, portOffset int, dataDir string, hasAgent, hasDBServer, hasCoordinator, hasResilientSingle, hasSyncMaster, hasSyncWorker, isSecure bool) Peer {
	p := Peer{
		ID:                     id,
		Address:                address,
		Port:                   port,
		PortOffset:             portOffset,
		DataDir:                dataDir,
		HasAgentFlag:           hasAgent,
		IsSecure:               isSecure,
		HasResilientSingleFlag: hasResilientSingle,
		HasSyncMasterFlag:      hasSyncMaster,
		HasSyncWorkerFlag:      hasSyncWorker,
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

// HasSyncMaster returns true if this peer is running an arangosync master server
func (p Peer) HasSyncMaster() bool { return p.HasSyncMasterFlag }

// HasSyncWorker returns true if this peer is running an arangosync worker server
func (p Peer) HasSyncWorker() bool { return p.HasSyncWorkerFlag }

// CreateStarterURL creates a URL to the relative path to the starter on this peer.
func (p Peer) CreateStarterURL(relPath string) string {
	addr := net.JoinHostPort(p.Address, strconv.Itoa(p.Port+p.PortOffset))
	relPath = strings.TrimPrefix(relPath, "/")
	scheme := NewURLSchemes(p.IsSecure).Browser
	return fmt.Sprintf("%s://%s/%s", scheme, addr, relPath)
}

// CreateDBServerAPI creates a client for the dbserver of the peer
func (p Peer) CreateDBServerAPI(clientBuilder ClientBuilder) (driver.Client, error) {
	if p.HasDBServer() {
		port := p.Port + p.PortOffset + definitions.ServerType(definitions.ServerTypeDBServer).PortOffset()
		scheme := NewURLSchemes(p.IsSecure).Browser
		ep := fmt.Sprintf("%s://%s", scheme, net.JoinHostPort(p.Address, strconv.Itoa(port)))
		c, err := clientBuilder.CreateClient([]string{ep}, ConnectionTypeDatabase, definitions.ServerTypeDBServer)
		if err != nil {
			return nil, maskAny(err)
		}
		return c, nil
	}
	return nil, maskAny(fmt.Errorf("Peer has no dbserver"))
}

func (p Peer) CreateClient(clientBuilder ClientBuilder, t definitions.ServerType) (driver.Client, error) {
	port := p.Port + p.PortOffset + definitions.ServerType(t).PortOffset()
	scheme := NewURLSchemes(p.IsSecure).Browser
	ep := fmt.Sprintf("%s://%s", scheme, net.JoinHostPort(p.Address, strconv.Itoa(port)))
	c, err := clientBuilder.CreateClient([]string{ep}, ConnectionTypeDatabase, t)
	if err != nil {
		return nil, maskAny(err)
	}
	return c, nil
}

// CreateCoordinatorAPI creates a client for the coordinator of the peer
func (p Peer) CreateCoordinatorAPI(clientBuilder ClientBuilder) (driver.Client, error) {
	if p.HasCoordinator() {
		port := p.Port + p.PortOffset + definitions.ServerType(definitions.ServerTypeCoordinator).PortOffset()
		scheme := NewURLSchemes(p.IsSecure).Browser
		ep := fmt.Sprintf("%s://%s", scheme, net.JoinHostPort(p.Address, strconv.Itoa(port)))
		c, err := clientBuilder.CreateClient([]string{ep}, ConnectionTypeDatabase, definitions.ServerTypeCoordinator)
		if err != nil {
			return nil, maskAny(err)
		}
		return c, nil
	}
	return nil, maskAny(fmt.Errorf("Peer has no coordinator"))
}

// CreateAgentAPI creates a client for the agent of the peer
func (p Peer) CreateAgentAPI(clientBuilder ClientBuilder) (driver.Client, error) {
	if p.HasAgent() {
		port := p.Port + p.PortOffset + definitions.ServerType(definitions.ServerTypeAgent).PortOffset()
		scheme := NewURLSchemes(p.IsSecure).Browser
		ep := fmt.Sprintf("%s://%s", scheme, net.JoinHostPort(p.Address, strconv.Itoa(port)))
		c, err := clientBuilder.CreateClient([]string{ep}, ConnectionTypeDatabase, definitions.ServerTypeAgent)
		if err != nil {
			return nil, maskAny(err)
		}
		return c, nil
	}
	return nil, maskAny(fmt.Errorf("Peer has no agent"))
}

// PortRangeOverlaps returns true if the port range of this peer overlaps with a port
// range starting at given port.
func (p Peer) PortRangeOverlaps(otherPort int, clusterConfig ClusterConfig) bool {
	myStart := p.Port + p.PortOffset                        // Inclusive
	myEnd := clusterConfig.NextPortOffset(myStart) - 1      // Inclusive
	otherEnd := clusterConfig.NextPortOffset(otherPort) - 1 // Inclusive

	return (otherPort >= myStart && otherPort <= myEnd) ||
		(otherEnd >= myStart && otherEnd <= myEnd)
}
