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
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestDockerClusterRecovery starts a master starter in docker, followed by 2 slave starters.
// Once started, it destroys one of the starters and attempts a recovery.
func TestDockerClusterRecovery(t *testing.T) {
	log := GetLogger(t)

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

	dockerRun2 := spawnMemberInDocker(t, basePort+100, cID2, joins, "", "")
	defer dockerRun2.Close()
	defer removeDockerContainer(t, cID2)

	dockerRun3 := spawnMemberInDocker(t, basePort+200, cID3, joins, "", "")
	defer dockerRun3.Close()
	defer removeDockerContainer(t, cID3)

	if ok := WaitUntilStarterReady(t, whatCluster, 3, dockerRun1, dockerRun2, dockerRun3); ok {
		t.Logf("Cluster start took %s", time.Since(start))
		testCluster(t, insecureStarterEndpoint(0), false)
		testCluster(t, insecureStarterEndpoint(100), false)
		testCluster(t, insecureStarterEndpoint(200), false)
	}

	log.Log("Kill Server1")

	// Cluster is up.
	// Kill starter slave-1 and all its processes
	ctx := context.Background()
	c := NewStarterClient(t, insecureStarterEndpoint(100))
	plist, err := c.Processes(ctx)
	if err != nil {
		t.Errorf("Processes failed: %s", describe(err))
		return
	}
	// Kill starter & servers
	containersToKill := []string{cID2}
	for _, s := range plist.Servers {
		containersToKill = append(containersToKill, s.ContainerID)
	}

	checkpoint := log.Checkpoint()

	checkpoint.Log("Kill docker containers")

	killDockerRun2 := Spawn(t, "docker rm -vf "+strings.Join(containersToKill, " "))
	killDockerRun2.Wait()

	checkpoint.Log("Wait for docker command to stop")

	// Wait for command to close
	dockerRun2.Wait()

	// Remove entire docker volume
	removeDockerVolume(t, cID2)

	checkpoint.Log("Recovery")

	// Create new volume
	recCID2 := createDockerID("starter-test-cluster-recovery2-recovery-")
	createDockerVolume(t, recCID2)
	defer removeDockerVolume(t, recCID2)

	// Create RECOVERY file in new volume
	recoveryContent := fmt.Sprintf("localhost:%d", basePort+(100))
	dockerBuildRecoveryRun := Spawn(t, strings.Join([]string{
		"docker run -i",
		"--label starter-test=true",
		"--name=" + cID2 + "recovery-builder",
		fmt.Sprintf("-v %s:/data", recCID2),
		"alpine",
		fmt.Sprintf("sh -c \"echo %s > /data/RECOVERY\"", recoveryContent),
	}, " "))
	dockerBuildRecoveryRun.Wait()

	checkpoint.Log("Wait for port to be closed")
	WaitForHttpPortClosed(checkpoint, NewThrottle(time.Second), insecureStarterEndpoint(100)).ExecuteT(t, time.Minute, time.Second)

	checkpoint.Log("Start docker container")
	// Restart dockerRun2
	recDockerRun2 := spawnMemberInDocker(t, basePort+100, recCID2, joins, "", "")
	defer recDockerRun2.Close()
	defer removeDockerContainer(t, recCID2)

	// Wait until recovered
	if ok := WaitUntilStarterReady(t, whatCluster, 1, recDockerRun2); ok {
		t.Logf("Cluster start (with recovery) took %s", time.Since(start))
		testCluster(t, insecureStarterEndpoint(0), false)
		testCluster(t, insecureStarterEndpoint(100), false)
		testCluster(t, insecureStarterEndpoint(200), false)
	}

	// RECOVERY file must now be gone
	/*	if _, err := os.Stat(filepath.Join(dataDirSlave1, "RECOVERY")); !os.IsNotExist(err) {
		t.Errorf("Expected RECOVERY file to not-exist, got: %s", describe(err))
	}*/

	waitForCallFunction(t,
		ShutdownStarterCall(insecureStarterEndpoint(0)),
		ShutdownStarterCall(insecureStarterEndpoint(100)),
		ShutdownStarterCall(insecureStarterEndpoint(200)))
}
