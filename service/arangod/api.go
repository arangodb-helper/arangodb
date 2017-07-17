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

package arangod

import (
	"context"
	"time"
)

// API abstracts the API of the ArangoDB agency / cluster / server
type API interface {
	// Agency returns API of the agency
	// Endpoints must be URL's of one or more agents of the cluster.
	Agency() AgencyAPI

	// Server returns API of single server
	// Returns an error when multiple endpoints are configured.
	Server() (ServerAPI, error)

	// Cluster returns API of the cluster.
	// Endpoints must be URL's of one or more coordinators of the cluster.
	Cluster() ClusterAPI
}

// AgencyAPI abstracts the API of the ArangoDB agency
type AgencyAPI interface {
	// ReadKey reads the value of a given key in the agency.
	ReadKey(ctx context.Context, key []string) (interface{}, error)

	// WriteKeyIfEmpty writes the given value with the given key only if the key was empty before.
	WriteKeyIfEmpty(ctx context.Context, key []string, value interface{}, ttl time.Duration) error

	// WriteKeyIfEqualTo writes the given new value with the given key only if the existing value for that key equals
	// to the given old value.
	WriteKeyIfEqualTo(ctx context.Context, key []string, newValue, oldValue interface{}, ttl time.Duration) error
}

// ServerAPI abstracts the API of a single ArangoDB server
type ServerAPI interface {
	// Gets the ID of this server in the cluster.
	// ID will be empty for single servers.
	ID(ctx context.Context) (string, error)

	// Shutdown a specific server, optionally removing it from its cluster.
	Shutdown(ctx context.Context, removeFromCluster bool) error
}

// ClusterAPI abstracts the API of an ArangoDB cluster
type ClusterAPI interface {
	// CleanOutServer triggers activities to clean out a DBServers.
	CleanOutServer(ctx context.Context, serverID string) error
	// IsCleanedOut checks if the dbserver with given ID has been cleaned out.
	IsCleanedOut(ctx context.Context, serverID string) (bool, error)
	// NumberOfServers returns the number of coordinator & dbservers in a clusters and the
	// ID's of cleaned out servers.
	NumberOfServers(ctx context.Context) (NumberOfServersResponse, error)
}

// NumberOfServersResponse holds the data returned from a NumberOfServer request.
type NumberOfServersResponse struct {
	NoCoordinators   int      `json:"numberOfCoordinators,omitempty"`
	NoDBServers      int      `json:"numberOfDBServers,omitempty"`
	CleanedServerIDs []string `json:"cleanedServers,omitempty"`
}
