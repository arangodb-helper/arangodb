//
// DISCLAIMER
//
// Copyright 2018-2024 ArangoDB GmbH, Cologne, Germany
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

package test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/arangodb-helper/arangodb/client"
)

// TestProcessClusterUpgrade starts a master starter, followed by 2 slave starters.
// Once running, it starts a database upgrade.
func TestProcessClusterUpgrade(t *testing.T) {
	removeArangodProcesses(t)
	testMatch(t, testModeProcess, starterModeCluster, false)
	dataDirMaster := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDirMaster)

	start := time.Now()

	master := Spawn(t, "${STARTER} "+createEnvironmentStarterOptions())
	defer master.Close()

	dataDirSlave1 := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDirSlave1)
	slave1 := Spawn(t, "${STARTER} --starter.join 127.0.0.1 "+createEnvironmentStarterOptions())
	defer slave1.Close()

	dataDirSlave2 := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDirSlave2)
	slave2 := Spawn(t, "${STARTER} --starter.join 127.0.0.1 "+createEnvironmentStarterOptions())
	defer slave2.Close()

	if ok := WaitUntilStarterReady(t, whatCluster, 3, master, slave1, slave2); ok {
		t.Logf("Cluster start took %s", time.Since(start))
		testCluster(t, insecureStarterEndpoint(0*portIncrement), false)
		testCluster(t, insecureStarterEndpoint(1*portIncrement), false)
		testCluster(t, insecureStarterEndpoint(2*portIncrement), false)
	}

	testUpgradeProcess(t, insecureStarterEndpoint(0*portIncrement))

	if isVerbose {
		t.Log("Waiting for termination")
	}
	SendIntrAndWait(t, master, slave1, slave2)
}

func testUpgradeProcess(t *testing.T, endpoint string) {
	c := NewStarterClient(t, endpoint)
	ctx := context.Background()

	waitForStarter(t, c)
	WaitUntilCoordinatorReadyAPI(t, insecureStarterEndpoint(0*portIncrement))
	WaitUntilCoordinatorReadyAPI(t, insecureStarterEndpoint(1*portIncrement))
	WaitUntilCoordinatorReadyAPI(t, insecureStarterEndpoint(2*portIncrement))

	t.Log("Starting database upgrade")

	// Retry StartDatabaseUpgrade if master is not yet known or lock is held (leader election may not have completed)
	deadline := time.Now().Add(30 * time.Second)
	for {
		err := c.StartDatabaseUpgrade(ctx, false)
		if err == nil {
			// Success!
			break
		}
		// Check if it's a service unavailable error (master not known yet)
		if client.IsServiceUnavailable(err) {
			if time.Now().After(deadline) {
				t.Fatalf("StartDatabaseUpgrade failed: master not available after 30 seconds: %v", err)
			}
			t.Logf("Master not yet known, retrying StartDatabaseUpgrade in 500ms...")
			time.Sleep(500 * time.Millisecond)
			continue
		}
		// Check if it's a lock already held error - retry as another starter might be upgrading
		errStr := err.Error()
		if strings.Contains(errStr, "lock already held") || strings.Contains(errStr, "already held") {
			if time.Now().After(deadline) {
				t.Fatalf("StartDatabaseUpgrade failed: lock still held after 30 seconds: %v", err)
			}
			t.Logf("Lock already held, retrying StartDatabaseUpgrade in 1s...")
			time.Sleep(1 * time.Second)
			continue
		}
		// Other error, fail immediately
		t.Fatalf("StartDatabaseUpgrade failed: %v", err)
	}

	// Give the upgrade plan time to be written to the agency
	time.Sleep(1 * time.Second)

	// Wait until upgrade complete
	recentErrors := 0
	deadline = time.Now().Add(time.Minute * 10)
	for {
		status, err := c.UpgradeStatus(ctx)
		if err != nil {
			// Check if it's a "no upgrade plan" error - retry as plan might not be written yet
			errStr := err.Error()
			if strings.Contains(errStr, "no upgrade plan") || strings.Contains(errStr, "There is no upgrade plan") {
				if time.Now().After(deadline) {
					t.Fatalf("UpgradeStatus failed: upgrade plan not found after 30 seconds: %s", err)
				}
				t.Logf("Upgrade plan not found yet, retrying UpgradeStatus in 500ms...")
				time.Sleep(500 * time.Millisecond)
				continue
			}
			recentErrors++
			if recentErrors > 20 {
				t.Fatalf("UpgradeStatus failed: %s", err)
			} else {
				t.Logf("UpgradeStatus failed: %s", err)
			}
		} else {
			recentErrors = 0
			if status.Failed {
				t.Fatalf("Upgrade failed: %s", status.Reason)
			}
			if status.Ready {
				if isVerbose {
					t.Logf("UpgradeStatus good: %v", status)
				}
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("Upgrade failed to finish in time")
		}
		time.Sleep(time.Second)
	}
}
