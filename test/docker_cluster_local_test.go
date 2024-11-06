//
// DISCLAIMER
//
// Copyright 2017-2024 ArangoDB GmbH, Cologne, Germany
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
	"fmt"
	"testing"
	"time"
)

// TestDockerClusterLocal runs the arangodb starter in docker with `--local`
func TestDockerClusterLocal(t *testing.T) {
	testMatch(t, testModeDocker, starterModeCluster, false)

	cID := createDockerID("starter-test-cluster-default1-")
	createDockerVolume(t, cID)
	defer removeDockerVolume(t, cID)

	// Cleanup of left over tests
	removeDockerContainersByLabel(t, "starter-test=true")
	removeStarterCreatedDockerContainers(t)

	joins := fmt.Sprintf("localhost:%d,localhost:%d,localhost:%d", basePort, basePort+(1*portIncrement), basePort+(2*portIncrement))

	start := time.Now()

	dockerRun := spawnMemberInDocker(t, basePort, cID, joins, "--starter.local", "")
	defer dockerRun.Close()
	defer removeDockerContainer(t, cID)

	if ok := WaitUntilStarterReady(t, whatCluster, 1, dockerRun); ok {
		t.Logf("Cluster start took %s", time.Since(start))
		testCluster(t, insecureStarterEndpoint(0*portIncrement), false)
	}

	waitForCallFunction(t,
		ShutdownStarterCall(insecureStarterEndpoint(0*portIncrement)))
}

// TestDockerClusterLocalAgencySize1 runs the arangodb starter in docker
// with `--starter.local` & `--cluster.agency-size=1`
func TestDockerClusterLocalAgencySize1(t *testing.T) {
	testMatch(t, testModeDocker, starterModeCluster, false)

	cID := createDockerID("starter-test-cluster-default1-")
	createDockerVolume(t, cID)
	defer removeDockerVolume(t, cID)

	// Cleanup of left over tests
	removeDockerContainersByLabel(t, "starter-test=true --cluster.agency-size=1")
	removeStarterCreatedDockerContainers(t)

	joins := fmt.Sprintf("localhost:%d,localhost:%d,localhost:%d", basePort, basePort+(1*portIncrement), basePort+(2*portIncrement))

	start := time.Now()

	dockerRun := spawnMemberInDocker(t, basePort, cID, joins, "--starter.local", "")
	defer dockerRun.Close()
	defer removeDockerContainer(t, cID)

	if ok := WaitUntilStarterReady(t, whatCluster, 1, dockerRun); ok {
		t.Logf("Cluster start took %s", time.Since(start))
		testCluster(t, insecureStarterEndpoint(0*portIncrement), false)
	}

	waitForCallFunction(t,
		ShutdownStarterCall(insecureStarterEndpoint(0*portIncrement)))
}

// TestOldDockerClusterLocal runs the arangodb starter in docker with `--local`
func TestOldDockerClusterLocal(t *testing.T) {
	testMatch(t, testModeDocker, starterModeCluster, false)

	cID := createDockerID("starter-test-cluster-default1-")
	createDockerVolume(t, cID)
	defer removeDockerVolume(t, cID)

	// Cleanup of left over tests
	removeDockerContainersByLabel(t, "starter-test=true")
	removeStarterCreatedDockerContainers(t)

	joins := fmt.Sprintf("localhost:%d,localhost:%d,localhost:%d", basePort, basePort+(1*portIncrement), basePort+(2*portIncrement))

	start := time.Now()

	dockerRun := spawnMemberInDocker(t, basePort, cID, joins, "--local", "")
	defer dockerRun.Close()
	defer removeDockerContainer(t, cID)

	if ok := WaitUntilStarterReady(t, whatCluster, 1, dockerRun); ok {
		t.Logf("Cluster start took %s", time.Since(start))
		testCluster(t, insecureStarterEndpoint(0*portIncrement), false)
	}

	waitForCallFunction(t,
		ShutdownStarterCall(insecureStarterEndpoint(0*portIncrement)))
}
