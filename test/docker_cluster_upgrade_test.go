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
	"fmt"
	"testing"
	"time"
)

// TestDockerClusterUpgrade runs 3 arangodb starters in docker with default settings.
// Once running it starts a database upgrade.
func TestDockerClusterUpgrade(t *testing.T) {
	testMatch(t, testModeDocker, starterModeCluster, false)

	cID1 := createDockerID("starter-test-cluster-default1-")
	createDockerVolume(t, cID1)
	defer removeDockerVolume(t, cID1)

	cID2 := createDockerID("starter-test-cluster-default2-")
	createDockerVolume(t, cID2)
	defer removeDockerVolume(t, cID2)

	cID3 := createDockerID("starter-test-cluster-default3-")
	createDockerVolume(t, cID3)
	defer removeDockerVolume(t, cID3)

	// Cleanup of left over tests
	removeDockerContainersByLabel(t, "starter-test=true")
	removeStarterCreatedDockerContainers(t)

	joins := fmt.Sprintf("localhost:%d", basePort)

	start := time.Now()

	dockerRun1 := spawnMemberInDocker(t, basePort, cID1, "", "", "")
	defer dockerRun1.Close()
	defer removeDockerContainer(t, cID1)

	dockerRun2 := spawnMemberInDocker(t, basePort+(1*portIncrement), cID2, joins, "", "")
	defer dockerRun2.Close()
	defer removeDockerContainer(t, cID2)

	dockerRun3 := spawnMemberInDocker(t, basePort+(2*portIncrement), cID3, joins, "", "")
	defer dockerRun3.Close()
	defer removeDockerContainer(t, cID3)

	if ok := WaitUntilStarterReady(t, whatCluster, 3, dockerRun1, dockerRun2, dockerRun3); ok {
		t.Logf("Cluster start took %s", time.Since(start))
		testCluster(t, insecureStarterEndpoint(0*portIncrement), false)
		testCluster(t, insecureStarterEndpoint(1*portIncrement), false)
		testCluster(t, insecureStarterEndpoint(2*portIncrement), false)
	}

	testUpgradeProcess(t, insecureStarterEndpoint(0*portIncrement))

	waitForCallFunction(t,
		ShutdownStarterCall(insecureStarterEndpoint(0*portIncrement)),
		ShutdownStarterCall(insecureStarterEndpoint(1*portIncrement)),
		ShutdownStarterCall(insecureStarterEndpoint(2*portIncrement)))
}
