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
	"fmt"
	"net/http"
	"testing"

	"github.com/arangodb/ArangoDBStarter/client"
)

const (
	basePort = 4000
)

// insecureStarterEndpoint creates an insecure (HTTP) endpoint for a starter
// at localhost using default base port + given offset.
func insecureStarterEndpoint(portOffset int) string {
	return fmt.Sprintf("http://localhost:%d", basePort+portOffset)
}

// testCluster runs a series of tests to verify a good cluster.
func testCluster(t *testing.T, starterEndpoint string) client.API {
	c := NewStarterClient(t, starterEndpoint)
	testProcesses(t, c, "cluster", starterEndpoint)
	return c
}

// testSingle runs a series of tests to verify a good single server.
func testSingle(t *testing.T, starterEndpoint string) client.API {
	c := NewStarterClient(t, starterEndpoint)
	testProcesses(t, c, "single", starterEndpoint)
	return c
}

// testProcesses runs a series of tests to verify a good series of database servers.
func testProcesses(t *testing.T, c client.API, mode, starterEndpoint string) {
	ctx := context.Background()

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
		if mode == "single" {
			t.Errorf("Found agent, not allowed in single mode")
		} else {
			if isVerbose {
				t.Logf("Found agent at %s:%d", sp.IP, sp.Port)
			}
			testArangodReachable(t, sp)
		}
	}

	// Check coordinator
	if sp, ok := processes.ServerByType(client.ServerTypeCoordinator); ok {
		if mode == "single" {
			t.Errorf("Found coordinator, not allowed in single mode")
		} else {
			if isVerbose {
				t.Logf("Found coordinator at %s:%d", sp.IP, sp.Port)
			}
			testArangodReachable(t, sp)
		}
	} else if mode == "cluster" {
		t.Errorf("No coordinator found in %s", starterEndpoint)
	}

	// Check dbserver
	if sp, ok := processes.ServerByType(client.ServerTypeDBServer); ok {
		if mode == "single" {
			t.Errorf("Found dbserver, not allowed in single mode")
		} else {
			if isVerbose {
				t.Logf("Found dbserver at %s:%d", sp.IP, sp.Port)
			}
			testArangodReachable(t, sp)
		}
	} else if mode == "cluster" {
		t.Errorf("No dbserver found in %s", starterEndpoint)
	}

	// Check single
	if sp, ok := processes.ServerByType(client.ServerTypeSingle); ok {
		if mode == "cluster" {
			t.Errorf("Found single, not allowed in cluster mode")
		} else {
			if isVerbose {
				t.Logf("Found single at %s:%d", sp.IP, sp.Port)
			}
			testArangodReachable(t, sp)
		}
	} else if mode == "single" {
		t.Errorf("No single found in %s", starterEndpoint)
	}
}

// testArangodReachable tries to call some HTTP API methods of the given server process to make sure
// it is reachable.
func testArangodReachable(t *testing.T, sp client.ServerProcess) {
	scheme := "http"
	if sp.IsSecure {
		scheme = "https"
	}
	url := fmt.Sprintf("%s://%s:%d/_api/version", scheme, sp.IP, sp.Port)
	_, err := http.Get(url)
	if err != nil {
		t.Errorf("Failed to reach arangod at %s:%d", sp.IP, sp.Port)
	}
}
