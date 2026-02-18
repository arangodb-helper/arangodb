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
	"testing"
	"time"
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

	if err := c.StartDatabaseUpgrade(ctx, false); err != nil {
		t.Fatalf("StartDatabaseUpgrade failed: %v", err)
	}
	// Wait until upgrade complete
	recentErrors := 0
	deadline := time.Now().Add(time.Minute * 10)
	for {
		status, err := c.UpgradeStatus(ctx)
		if err != nil {
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
