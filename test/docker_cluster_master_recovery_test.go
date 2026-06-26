//
// DISCLAIMER
//
// Copyright 2026 ArangoDB GmbH, Cologne, Germany
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
	"regexp"
	"strings"
	"testing"
	"time"
)

// TestDockerClusterMasterRecovery starts a master starter in docker, followed by 2 slave starters.
// Once started, it destroys the master starter and attempts a recovery using the remaining starters
// in --starter.join (bootstrap master recovery workaround).
//
// Port layout matches docker_cluster_recovery_test and process_cluster_master_recovery_test
// (basePort + 0/100/200). Requires native Linux Docker with --net=host (CircleCI machine executor);
// Docker Desktop on WSL2 does not expose host networking correctly.
func TestDockerClusterMasterRecovery(t *testing.T) {
	log := GetLogger(t)

	testMatch(t, testModeDocker, starterModeCluster, false)

	cID1 := createDockerID("starter-test-cluster-master1-")
	createDockerVolume(t, cID1)
	defer removeDockerVolume(t, cID1)

	cID2 := createDockerID("starter-test-cluster-master2-")
	createDockerVolume(t, cID2)
	defer removeDockerVolume(t, cID2)

	cID3 := createDockerID("starter-test-cluster-master3-")
	createDockerVolume(t, cID3)
	defer removeDockerVolume(t, cID3)

	removeDockerContainersByLabel(t, "starter-test=true")
	removeStarterCreatedDockerContainers(t)

	masterJoin := fmt.Sprintf("localhost:%d", basePort)
	slaveJoins := fmt.Sprintf("localhost:%d,localhost:%d", basePort+100, basePort+200)

	start := time.Now()

	dockerRun1 := spawnMemberInDocker(t, basePort, cID1, "", "", "")
	defer dockerRun1.Close()
	defer removeDockerContainer(t, cID1)

	dockerRun2 := spawnMemberInDocker(t, basePort+100, cID2, masterJoin, "", "")
	defer dockerRun2.Close()
	defer removeDockerContainer(t, cID2)

	dockerRun3 := spawnMemberInDocker(t, basePort+200, cID3, masterJoin, "", "")
	defer dockerRun3.Close()
	defer removeDockerContainer(t, cID3)

	if ok := WaitUntilStarterReady(t, whatCluster, 3, dockerRun1, dockerRun2, dockerRun3); ok {
		t.Logf("Cluster start took %s", time.Since(start))
		testCluster(t, insecureStarterEndpoint(0), false)
		testCluster(t, insecureStarterEndpoint(100), false)
		testCluster(t, insecureStarterEndpoint(200), false)
	}

	log.Log("Kill master starter")

	ctx := context.Background()
	c := NewStarterClient(t, insecureStarterEndpoint(0))
	plist, err := c.Processes(ctx)
	if err != nil {
		t.Errorf("Processes failed: %s", describe(err))
		waitForCallFunction(t,
			ShutdownStarterCall(insecureStarterEndpoint(0)),
			ShutdownStarterCall(insecureStarterEndpoint(100)),
			ShutdownStarterCall(insecureStarterEndpoint(200)),
		)
		return
	}

	containersToKill := []string{cID1}
	for _, s := range plist.Servers {
		containersToKill = append(containersToKill, s.ContainerID)
	}

	checkpoint := log.Checkpoint()
	checkpoint.Log("Kill master docker containers")

	killDockerRun1 := Spawn(t, "docker rm -vf "+strings.Join(containersToKill, " "))
	killDockerRun1.Wait()

	checkpoint.Log("Wait for master starter to stop")
	dockerRun1.Wait()

	removeDockerVolume(t, cID1)

	checkpoint.Log("Wait for leader election on surviving starters")
	time.Sleep(35 * time.Second)

	recVolID := createDockerID("starter-test-cluster-master-recovery-")
	createDockerVolume(t, recVolID)
	defer removeDockerVolume(t, recVolID)

	recoveryContent := fmt.Sprintf("localhost:%d", basePort)
	dockerBuildRecoveryRun := Spawn(t, strings.Join([]string{
		"docker run -i",
		"--label starter-test=true",
		"--name=" + cID1 + "recovery-builder",
		fmt.Sprintf("-v %s:/data", recVolID),
		"alpine",
		fmt.Sprintf("sh -c \"echo %s > /data/RECOVERY\"", recoveryContent),
	}, " "))
	dockerBuildRecoveryRun.Wait()

	checkpoint.Log("Wait for master port to be closed")
	WaitForHttpPortClosed(checkpoint, NewThrottle(time.Second), insecureStarterEndpoint(0)).ExecuteT(t, time.Minute, time.Second)

	checkpoint.Log("Start master recovery container")
	recDockerRun1 := spawnMemberInDocker(t, basePort, recVolID, slaveJoins, "", "")
	defer recDockerRun1.Close()
	defer removeDockerContainer(t, recVolID)

	if ok := WaitUntilStarterReady(t, whatCluster, 3, recDockerRun1, dockerRun2, dockerRun3); ok {
		t.Logf("Cluster start (with master recovery) took %s", time.Since(start))
		testCluster(t, insecureStarterEndpoint(0), false)
		testCluster(t, insecureStarterEndpoint(100), false)
		testCluster(t, insecureStarterEndpoint(200), false)
	}

	startWait := time.Now()
	for {
		checkRecovery := Spawn(t, fmt.Sprintf(
			"docker run --rm -v %s:/data alpine sh -c 'test ! -f /data/RECOVERY'",
			recVolID,
		))
		err := checkRecovery.Wait()
		checkRecovery.Close()
		if err == nil {
			t.Log("RECOVERY file has vanished, good.")
			break
		}
		time.Sleep(time.Second)
		if time.Since(startWait) > 30*time.Second {
			t.Errorf("Expected RECOVERY file to not exist in volume %s", recVolID)
			break
		}
	}

	waitForCallFunction(t,
		ShutdownStarterCall(insecureStarterEndpoint(0)),
		ShutdownStarterCall(insecureStarterEndpoint(100)),
		ShutdownStarterCall(insecureStarterEndpoint(200)),
	)
}

// TestDockerClusterMasterRecoverySelfJoinOnly exercises bootstrap master recovery on a live
// 3-node docker cluster when --starter.join lists only the replaced starter address: recovery
// must fail fast and must not rejoin the cluster as a new peer.
//
// Requires native Linux Docker with --net=host (CircleCI machine executor).
func TestDockerClusterMasterRecoverySelfJoinOnly(t *testing.T) {
	log := GetLogger(t)

	testMatch(t, testModeDocker, starterModeCluster, false)

	cID1 := createDockerID("starter-test-cluster-master1-")
	createDockerVolume(t, cID1)
	defer removeDockerVolume(t, cID1)

	cID2 := createDockerID("starter-test-cluster-master2-")
	createDockerVolume(t, cID2)
	defer removeDockerVolume(t, cID2)

	cID3 := createDockerID("starter-test-cluster-master3-")
	createDockerVolume(t, cID3)
	defer removeDockerVolume(t, cID3)

	removeDockerContainersByLabel(t, "starter-test=true")
	removeStarterCreatedDockerContainers(t)

	masterJoin := fmt.Sprintf("localhost:%d", basePort)

	dockerRun1 := spawnMemberInDocker(t, basePort, cID1, "", "", "")
	defer dockerRun1.Close()
	defer removeDockerContainer(t, cID1)

	dockerRun2 := spawnMemberInDocker(t, basePort+100, cID2, masterJoin, "", "")
	defer dockerRun2.Close()
	defer removeDockerContainer(t, cID2)

	dockerRun3 := spawnMemberInDocker(t, basePort+200, cID3, masterJoin, "", "")
	defer dockerRun3.Close()
	defer removeDockerContainer(t, cID3)

	if !WaitUntilStarterReady(t, whatCluster, 3, dockerRun1, dockerRun2, dockerRun3) {
		waitForCallFunction(t,
			ShutdownStarterCall(insecureStarterEndpoint(100)),
			ShutdownStarterCall(insecureStarterEndpoint(200)),
		)
		return
	}

	ctx := context.Background()
	c := NewStarterClient(t, insecureStarterEndpoint(0))
	plist, err := c.Processes(ctx)
	if err != nil {
		t.Fatalf("Processes failed: %s", describe(err))
	}

	containersToKill := []string{cID1}
	for _, s := range plist.Servers {
		containersToKill = append(containersToKill, s.ContainerID)
	}

	checkpoint := log.Checkpoint()
	checkpoint.Log("Kill master docker containers")

	killDockerRun1 := Spawn(t, "docker rm -vf "+strings.Join(containersToKill, " "))
	killDockerRun1.Wait()

	checkpoint.Log("Wait for master starter to stop")
	dockerRun1.Wait()

	removeDockerVolume(t, cID1)

	checkpoint.Log("Wait for leader election on surviving starters")
	time.Sleep(35 * time.Second)

	checkpoint.Log("Wait for master port to be closed")
	WaitForHttpPortClosed(checkpoint, NewThrottle(time.Second), insecureStarterEndpoint(0)).ExecuteT(t, time.Minute, time.Second)

	recVolID := createDockerID("starter-test-cluster-master-recovery-self-join-")
	createDockerVolume(t, recVolID)
	defer removeDockerVolume(t, recVolID)

	recoveryContent := fmt.Sprintf("localhost:%d", basePort)
	dockerBuildRecoveryRun := Spawn(t, strings.Join([]string{
		"docker run -i",
		"--label starter-test=true",
		"--name=" + cID1 + "recovery-builder",
		fmt.Sprintf("-v %s:/data", recVolID),
		"alpine",
		fmt.Sprintf("sh -c \"echo %s > /data/RECOVERY\"", recoveryContent),
	}, " "))
	dockerBuildRecoveryRun.Wait()

	checkpoint.Log("Start master recovery container with self-only join")
	recDockerRun1 := spawnMemberInDocker(t, basePort, recVolID, masterJoin, "", "")
	defer recDockerRun1.Close()
	defer removeDockerContainer(t, recVolID)

	if err := recDockerRun1.ExpectTimeout(
		context.Background(),
		30*time.Second,
		regexp.MustCompile(recoveryFailurePattern),
		"self-join recovery",
	); err != nil {
		logSubProcessOutput(t, "docker master recovery (self-join, on timeout)", recDockerRun1)
		t.Fatalf("expected recovery failure log line within 30s, but timed out: %s", describe(err))
	}
	logSubProcessOutput(t, "docker master recovery (self-join, matched failure)", recDockerRun1)

	if regexp.MustCompile(`Your cluster can now be accessed`).Match(recDockerRun1.Output()) {
		logSubProcessOutput(t, "docker master recovery (unexpected cluster ready)", recDockerRun1)
		t.Fatal("recovery starter joined/bootstrapped the cluster instead of failing on self-only join")
	}

	WaitUntilStarterExit(t, 10*time.Second, 1, recDockerRun1)
	t.Log("recovery starter exited as expected after failure")

	testCluster(t, insecureStarterEndpoint(100), false)
	testCluster(t, insecureStarterEndpoint(200), false)

	waitForCallFunction(t,
		ShutdownStarterCall(insecureStarterEndpoint(100)),
		ShutdownStarterCall(insecureStarterEndpoint(200)),
	)
}
