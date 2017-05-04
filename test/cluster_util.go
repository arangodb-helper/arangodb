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

package test

import (
	"context"
	"testing"

	"github.com/arangodb/ArangoDBStarter/client"
)

// testCluster runs a series of tests to verify a good cluster.
func testCluster(t *testing.T, starterEndpoint string) client.API {
	ctx := context.Background()
	c := NewStarterClient(t, starterEndpoint)

	// Fetch version
	if info, err := c.Version(ctx); err != nil {
		t.Errorf("Failed to get starter version: %s", describe(err))
	} else {
		if isVerbose {
			t.Logf("Found starter version %s, %s", info.Version, info.Build)
		}
	}

	// Fetch server processes
	processes, err := c.Processes(ctx)
	if err != nil {
		t.Fatalf("Failed to get server processes: %s", describe(err))
	}

	// Check agent
	if sp, ok := processes.ServerByType(client.ServerTypeAgent); ok {
		if isVerbose {
			t.Logf("Found agent at %s:%d", sp.IP, sp.Port)
		}
	}

	// Check coordinator
	if sp, ok := processes.ServerByType(client.ServerTypeCoordinator); ok {
		if isVerbose {
			t.Logf("Found coordinator at %s:%d", sp.IP, sp.Port)
		}
	} else {
		t.Errorf("No coordinator found in %s", starterEndpoint)
	}

	// Check dbserver
	if sp, ok := processes.ServerByType(client.ServerTypeDBServer); ok {
		if isVerbose {
			t.Logf("Found dbserver at %s:%d", sp.IP, sp.Port)
		}
	} else {
		t.Errorf("No dbserver found in %s", starterEndpoint)
	}

	return c
}
