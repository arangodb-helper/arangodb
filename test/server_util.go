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
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/pkg/errors"

	"github.com/arangodb-helper/arangodb/client"
	"github.com/arangodb-helper/arangodb/service"
	"github.com/arangodb/go-driver"
	driverhttp "github.com/arangodb/go-driver/http"
)

const (
	basePort            = service.DefaultMasterPort
	syncMonitoringToken = "syncMonitoringSecretToken"
)

var (
	// Custom httpClient which allows insecure HTTPS connections.
	httpClient = &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
				DualStack: true,
			}).DialContext,
			MaxIdleConns:        100,
			IdleConnTimeout:     90 * time.Second,
			TLSHandshakeTimeout: 10 * time.Second,
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
)

// insecureStarterEndpoint creates an insecure (HTTP) endpoint for a starter
// at localhost using default base port + given offset.
func insecureStarterEndpoint(portOffset int) string {
	return fmt.Sprintf("http://localhost:%d", basePort+portOffset)
}

// secureStarterEndpoint creates a secure (HTTPS) endpoint for a starter
// at localhost using default base port + given offset.
func secureStarterEndpoint(portOffset int) string {
	return fmt.Sprintf("https://localhost:%d", basePort+portOffset)
}

// testCluster runs a series of tests to verify a good cluster.
func testCluster(t *testing.T, starterEndpoint string, isSecure bool) client.API {
	c := NewStarterClient(t, starterEndpoint)
	testProcesses(t, c, "cluster", starterEndpoint, isSecure, false, false, 0, 0)
	return c
}

// testClusterWithSync runs a series of tests to verify a good cluster with synchronization enabled.
func testClusterWithSync(t *testing.T, starterEndpoint string, isSecure bool) client.API {
	c := NewStarterClient(t, starterEndpoint)
	testProcesses(t, c, "cluster", starterEndpoint, isSecure, false, true, 0, time.Minute*5)
	return c
}

// testSingle runs a series of tests to verify a good single server.
func testSingle(t *testing.T, starterEndpoint string, isSecure bool) client.API {
	c := NewStarterClient(t, starterEndpoint)
	testProcesses(t, c, "single", starterEndpoint, isSecure, false, false, 0, 0)
	return c
}

// testResilientSingle runs a series of tests to verify good resilientsingle servers.
func testResilientSingle(t *testing.T, starterEndpoint string, isSecure bool, expectAgencyOnly bool) client.API {
	c := NewStarterClient(t, starterEndpoint)

	testProcesses(t, c, "resilientsingle", starterEndpoint, isSecure, expectAgencyOnly, false, time.Second*30, time.Minute*2)
	return c
}

// waitForStarter waits when starter endpoint starts responding
func waitForStarter(t *testing.T, c client.API) {
	throttle := NewThrottle(2 * time.Second)
	NewTimeoutFunc(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if _, err := c.Version(ctx); err != nil {
			throttle.Execute(func() {
				t.Logf("Version check failed due to %s", err.Error())
			})
			return nil
		} else {
			return NewInterrupt()
		}
	}).ExecuteT(t, time.Minute, 500*time.Millisecond)
}

// testProcesses runs a series of tests to verify a good series of database servers.
func testProcesses(t *testing.T, c client.API, mode, starterEndpoint string, isSecure bool,
	expectAgencyOnly bool, syncEnabled bool, singleTimeout, reachableTimeout time.Duration) {
	// Give the deployment a little bit of time:
	ctx := context.Background()

	// Wait until starter restarts
	waitForStarter(t, c)
	log := GetLogger(t)

	log.Log("Starter is responding: %s", starterEndpoint)

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
		if sp.IsSecure != isSecure {
			t.Errorf("Invalid IsSecure on agent. Expected %v, got %v", isSecure, sp.IsSecure)
		}
		if mode == "single" {
			t.Errorf("Found agent, not allowed in single mode")
		} else {
			if isVerbose {
				t.Logf("Found agent at %s:%d", sp.IP, sp.Port)
			}
			testArangodReachable(t, sp, reachableTimeout)
		}
	}

	// Check coordinator
	if sp, ok := processes.ServerByType(client.ServerTypeCoordinator); ok {
		if sp.IsSecure != isSecure {
			t.Errorf("Invalid IsSecure on coordinator. Expected %v, got %v", isSecure, sp.IsSecure)
		}
		if mode == "single" || mode == "resilientsingle" {
			t.Errorf("Found coordinator, not allowed in single|resilientsingle mode")
		} else {
			if isVerbose {
				t.Logf("Found coordinator at %s:%d", sp.IP, sp.Port)
			}
			testArangodReachable(t, sp, reachableTimeout)
		}
	} else if mode == "cluster" {
		t.Errorf("No coordinator found in %s", starterEndpoint)
	}

	// Check dbserver
	if sp, ok := processes.ServerByType(client.ServerTypeDBServer); ok {
		if sp.IsSecure != isSecure {
			t.Errorf("Invalid IsSecure on dbserver. Expected %v, got %v", isSecure, sp.IsSecure)
		}
		if mode == "single" || mode == "resilientsingle" {
			t.Errorf("Found dbserver, not allowed in single|resilientsingle mode")
		} else {
			if isVerbose {
				t.Logf("Found dbserver at %s:%d", sp.IP, sp.Port)
			}
			testArangodReachable(t, sp, reachableTimeout)
		}
	} else if mode == "cluster" {
		t.Errorf("No dbserver found in %s", starterEndpoint)
	}

	// Check single
	{
		deadline := time.Now().Add(singleTimeout)
		for {
			if sp, ok := processes.ServerByType(client.ServerTypeSingle); ok {
				if sp.IsSecure != isSecure {
					t.Errorf("Invalid IsSecure on single. Expected %v, got %v", isSecure, sp.IsSecure)
				}
				if mode == "cluster" || expectAgencyOnly {
					t.Errorf("Found single, not allowed in cluster mode")
				} else {
					if isVerbose {
						t.Logf("Found single at %s:%d", sp.IP, sp.Port)
					}
					testArangodReachable(t, sp, reachableTimeout)
				}
			} else if (mode == "single" || mode == "resilientsingle") && !expectAgencyOnly {
				if time.Now().Before(deadline) {
					// For activefailover not all starters have to be ready when we're called,
					// so we allow for some time to pass until we call it a failure.
					time.Sleep(time.Second)
					continue
				}
				t.Errorf("No single found in %s", starterEndpoint)
			}
			break
		}
	}

	// Check syncmaster
	if sp, ok := processes.ServerByType(client.ServerTypeSyncMaster); ok {
		if sp.IsSecure != isSecure {
			t.Errorf("Invalid IsSecure on single. Expected %v, got %v", isSecure, sp.IsSecure)
		}
		if mode != "cluster" || !syncEnabled {
			t.Errorf("Found syncmaster, not allowed in cluster mode without sync")
		} else {
			if isVerbose {
				t.Logf("Found syncmaster at %s:%d", sp.IP, sp.Port)
			}
			testArangoSyncReachable(t, sp, reachableTimeout)
		}
	} else if (mode == "cluster") && syncEnabled {
		t.Errorf("No syncmaster found in %s", starterEndpoint)
	}

	// Check syncworker
	if sp, ok := processes.ServerByType(client.ServerTypeSyncWorker); ok {
		if sp.IsSecure != isSecure {
			t.Errorf("Invalid IsSecure on single. Expected %v, got %v", isSecure, sp.IsSecure)
		}
		if mode != "cluster" || !syncEnabled {
			t.Errorf("Found syncworker, not allowed in cluster mode without sync")
		} else {
			if isVerbose {
				t.Logf("Found syncworker at %s:%d", sp.IP, sp.Port)
			}
			testArangoSyncReachable(t, sp, reachableTimeout)
		}
	} else if (mode == "cluster") && syncEnabled {
		t.Errorf("No syncworker found in %s", starterEndpoint)
	}
}

// testArangodReachable tries to call some HTTP API methods of the given server process to make sure
// it is reachable.
func testArangodReachable(t *testing.T, sp client.ServerProcess, timeout time.Duration) {
	scheme := "http"
	if sp.IsSecure {
		scheme = "https"
	}
	start := time.Now()
	for {
		url := fmt.Sprintf("%s://%s:%d/_api/version", scheme, sp.IP, sp.Port)
		_, err := httpClient.Get(url)
		if err == nil {
			return
		}
		if timeout == 0 || time.Since(start) > timeout {
			t.Errorf("Failed to reach arangod at %s:%d %s", sp.IP, sp.Port, describe(err))
			return
		}
		time.Sleep(time.Second)
	}
}

// testArangoSyncReachable tries to call some HTTP API methods of the given server process to make sure
// it is reachable.
func testArangoSyncReachable(t *testing.T, sp client.ServerProcess, timeout time.Duration) {
	start := time.Now()
	for {
		url := fmt.Sprintf("https://%s:%d/_api/version", sp.IP, sp.Port)
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			t.Fatalf("NewRequest failed: %s", describe(err))
		}
		req.Header.Set("Authorization", "bearer "+syncMonitoringToken)
		_, err = httpClient.Do(req)
		if err == nil {
			return
		}
		if timeout == 0 || time.Since(start) > timeout {
			t.Errorf("Failed to reach arangosync at %s:%d %s", sp.IP, sp.Port, describe(err))
			return
		}
		time.Sleep(time.Second)
	}
}

// CreateClient creates a client to the server of the given type based on the arangodb starter endpoint.
func CreateClient(t *testing.T, starterEndpoint string, serverType client.ServerType,
	auth driver.Authentication) (driver.Client, error) {
	c := NewStarterClient(t, starterEndpoint)
	processes, err := c.Processes(context.Background())
	if err != nil {
		return nil, errors.Wrap(err, "failed to get processes")
	}

	sp, ok := processes.ServerByType(serverType)
	if !ok {
		return nil, errors.Errorf("failed to server of type %s", string(serverType))
	}

	config := driverhttp.ConnectionConfig{
		Endpoints: []string{sp.GetEndpoint()},
		TLSConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}
	connection, err := driverhttp.NewConnection(config)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create a new connection")
	}

	clientCfg := driver.ClientConfig{
		Connection:     connection,
		Authentication: auth,
	}
	client, err := driver.NewClient(clientCfg)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create a new client")
	}
	return client, nil
}
