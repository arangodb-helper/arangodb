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
	"os"
	"testing"
	"time"
)

// TestProcessClusterDefault starts a master starter, followed by 2 slave starters.
func TestProcessClusterDefault(t *testing.T) {
	needTestMode(t, testModeProcess)
	dataDirMaster := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDirMaster)

	start := time.Now()

	master := Spawn(t, "${STARTER}")
	defer master.Close()

	dataDirSlave1 := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDirSlave1)
	slave1 := Spawn(t, "${STARTER} --join 127.0.0.1")
	defer slave1.Close()

	dataDirSlave2 := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDirSlave2)
	slave2 := Spawn(t, "${STARTER} --join 127.0.0.1")
	defer slave2.Close()

	if ok := WaitUntilStarterReady(t, whatCluster, master, slave1, slave2); ok {
		t.Logf("Cluster start took %s", time.Since(start))
		testCluster(t, insecureStarterEndpoint(0), false)
		testCluster(t, insecureStarterEndpoint(5), false)
		testCluster(t, insecureStarterEndpoint(10), false)
	}

	if isVerbose {
		t.Log("Waiting for termination")
	}
	SendIntrAndWait(t, master, slave1, slave2)
}

// TestProcessClusterDefaultShutdownViaAPI starts a master starter, followed by 2 slave starters, shutting all down through the API.
func TestProcessClusterDefaultShutdownViaAPI(t *testing.T) {
	needTestMode(t, testModeProcess)
	dataDirMaster := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDirMaster)

	start := time.Now()

	master := Spawn(t, "${STARTER}")
	defer master.Close()

	dataDirSlave1 := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDirSlave1)
	slave1 := Spawn(t, "${STARTER} --join 127.0.0.1")
	defer slave1.Close()

	dataDirSlave2 := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDirSlave2)
	slave2 := Spawn(t, "${STARTER} --join 127.0.0.1")
	defer slave2.Close()

	if ok := WaitUntilStarterReady(t, whatCluster, master, slave1, slave2); ok {
		t.Logf("Cluster start took %s", time.Since(start))
		testCluster(t, insecureStarterEndpoint(0), false)
		testCluster(t, insecureStarterEndpoint(5), false)
		testCluster(t, insecureStarterEndpoint(10), false)
	}

	if isVerbose {
		t.Log("Waiting for termination")
	}
	ShutdownStarter(t, insecureStarterEndpoint(0))
	ShutdownStarter(t, insecureStarterEndpoint(5))
	ShutdownStarter(t, insecureStarterEndpoint(10))
}
