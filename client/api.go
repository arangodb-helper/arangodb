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

package client

import (
	"context"

	driver "github.com/arangodb/go-driver"
)

// API is the interface implemented by the starter's HTTP API's.
type API interface {
	// ID requests the starters ID.
	ID(ctx context.Context) (IDInfo, error)

	// Version requests the starter version.
	Version(ctx context.Context) (VersionInfo, error)

	// DatabaseVersion returns the version of the `arangod` binary that is being
	// used by this starter.
	DatabaseVersion(ctx context.Context) (driver.Version, error)

	// Processes loads information of all the database server processes launched by the starter.
	Processes(ctx context.Context) (ProcessList, error)

	// Endpoints loads the URL's needed to reach all starters, agents & coordinators in the cluster.
	Endpoints(ctx context.Context) (EndpointList, error)

	// Shutdown will shutdown a starter (and all its started database servers).
	// With goodbye set, it will remove the peer slot for the starter.
	Shutdown(ctx context.Context, goodbye bool) error

	// StartDatabaseUpgrade is called to start the upgrade process
	StartDatabaseUpgrade(ctx context.Context, force bool) error

	// RetryDatabaseUpgrade resets a failure mark in the existing upgrade plan
	// such that the starters will retry the upgrade once more.
	RetryDatabaseUpgrade(ctx context.Context) error

	// Status returns the status of any upgrade plan
	UpgradeStatus(context.Context) (UpgradeStatus, error)
}

// IDInfo contains the ID of the starter
type IDInfo struct {
	ID string `json:"id"`
}

// VersionInfo is the JSON response of a `/version` request.
type VersionInfo struct {
	Version string `json:"version"`
	Build   string `json:"build"`
}

// DatabaseVersionResponse is the JSON response of a `/database-version` request.
type DatabaseVersionResponse struct {
	Version driver.Version `json:"version"`
}

// EndpointList is the JSON response of a `/endpoints` request.
// It contains URL's of all starters, agents & coordinators in the cluster.
type EndpointList struct {
	Starters     []string `json:"starters,omitempty"`     // List of URL's to all starter APIs
	Agents       []string `json:"agents,omitempty"`       // List of URL's to all agents (database servers) in the cluster
	Coordinators []string `json:"coordinators,omitempty"` // List of URL's to all coordinators (database servers) in the cluster
}

// ProcessList is the JSON response of a `/process` request.
type ProcessList struct {
	ServersStarted bool            `json:"servers-started,omitempty"` // True if the server have all been started
	Servers        []ServerProcess `json:"servers,omitempty"`         // List of servers started by the starter
}

// ServerType holds a type of (arangod) server
type ServerType string

const (
	ServerTypeCoordinator = ServerType("coordinator")
	ServerTypeDBServer    = ServerType("dbserver")
	ServerTypeAgent       = ServerType("agent")
	ServerTypeSingle      = ServerType("single")
	ServerTypeSyncMaster  = ServerType("syncmaster")
	ServerTypeSyncWorker  = ServerType("syncworker")
)

// ServerProcess holds all information of a single server started by the starter.
type ServerProcess struct {
	Type        ServerType `json:"type"`                   // agent | coordinator | dbserver
	IP          string     `json:"ip"`                     // IP address needed to reach the server
	Port        int        `json:"port"`                   // Port needed to reach the server
	ProcessID   int        `json:"pid,omitempty"`          // PID of the process (0 when running in docker)
	ContainerID string     `json:"container-id,omitempty"` // ID of docker container running the server
	ContainerIP string     `json:"container-ip,omitempty"` // IP address of docker container running the server
	IsSecure    bool       `json:"is-secure,omitempty"`    // If set, this server is using an SSL connection
}

// ServerByType returns the server of given type.
// If no such server process is found, false is returned.
func (list ProcessList) ServerByType(serverType ServerType) (ServerProcess, bool) {
	for _, sp := range list.Servers {
		if sp.Type == serverType {
			return sp, true
		}
	}
	return ServerProcess{}, false
}

// UpgradeStatus is the JSON structure returns from a `GET /database-auto-upgrade`
// request.
type UpgradeStatus struct {
	// Ready is set to true when the entire upgrade has been finished succesfully.
	Ready bool `json:"ready"`
	// Failed is set to true when the upgrade process has yielded an error
	Failed bool `json:"failed"`
	// Reasons contains a human readable description of the state
	Reason string `json:"reason,omitempty"`
	// ServersUpgraded contains the servers that have been upgraded
	ServersUpgraded []UpgradeStatusServer `json:"servers_upgraded"`
	// ServersRemaining contains the servers that have not yet been upgraded
	ServersRemaining []UpgradeStatusServer `json:"servers_remaining"`
}

// UpgradeStatusServer is the nested JSON structure returns from a `GET /database-auto-upgrade`
// request.
type UpgradeStatusServer struct {
	// Type of the server
	Type ServerType `json:"type"`
	// Port the server is listening on
	Port int `json:"port"`
	// Address of the server (IP or hostname)
	Address string `json:"address"`
}
