//
// DISCLAIMER
//
// Copyright 2018 ArangoDB GmbH, Cologne, Germany
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
	"os"
	"strings"
	"testing"
	"time"
)

// TestDockerClusterRecovery starts a master starter in docker, followed by 2 slave starters.
// Once started, it destroys one of the starters and attempts a recovery.
func TestDockerClusterRecovery(t *testing.T) {
	log := GetLogger(t)

	SkipOnTravis(t, "Test does not work on TRAVIS VM") // TODO: Fix needed

	needTestMode(t, testModeDocker)
	needStarterMode(t, starterModeCluster)
	if os.Getenv("IP") == "" {
		t.Fatal("IP envvar must be set to IP address of this machine")
	}
	/*
		docker volume create arangodb1
		docker run -i --name=adb1 --rm -p 8528:8528 \
			-v arangodb1:/data \
			-v /var/run/docker.sock:/var/run/docker.sock \
			arangodb/arangodb-starter \
			--docker.container=adb1 \
			--starter.address=$IP
	*/
	volID1 := createDockerID("vol-starter-test-cluster-recovery1-")
	createDockerVolume(t, volID1)
	defer removeDockerVolume(t, volID1)

	volID2 := createDockerID("vol-starter-test-cluster-recovery2-")
	createDockerVolume(t, volID2)
	defer removeDockerVolume(t, volID2)

	volID3 := createDockerID("vol-starter-test-cluster-recovery3-")
	createDockerVolume(t, volID3)
	defer removeDockerVolume(t, volID3)

	// Cleanup of left over tests
	removeDockerContainersByLabel(t, "starter-test=true")
	removeStarterCreatedDockerContainers(t)

	start := time.Now()

	cID1 := createDockerID("starter-test-cluster-recovery1-")
	dockerRun1 := Spawn(t, strings.Join([]string{
		"docker run -i",
		"--label starter-test=true",
		"--name=" + cID1,
		createLicenseKeyOption(),
		fmt.Sprintf("-p %d:%d", basePort, basePort),
		fmt.Sprintf("-v %s:/data", volID1),
		"-v /var/run/docker.sock:/var/run/docker.sock",
		"arangodb/arangodb-starter",
		"--docker.container=" + cID1,
		fmt.Sprintf("--starter.port=%d", basePort),
		"--starter.address=$IP",
		createEnvironmentStarterOptions(),
	}, " "))
	defer dockerRun1.Close()
	defer removeDockerContainer(t, cID1)

	cID2 := createDockerID("starter-test-cluster-recovery2-")
	dockerRun2 := Spawn(t, strings.Join([]string{
		"docker run -i",
		"--label starter-test=true",
		"--name=" + cID2,
		createLicenseKeyOption(),
		fmt.Sprintf("-p %d:%d", basePort+100, basePort+100),
		fmt.Sprintf("-v %s:/data", volID2),
		"-v /var/run/docker.sock:/var/run/docker.sock",
		"arangodb/arangodb-starter",
		"--docker.container=" + cID2,
		fmt.Sprintf("--starter.port=%d", basePort+100),
		"--starter.address=$IP",
		createEnvironmentStarterOptions(),
		fmt.Sprintf("--starter.join=$IP:%d", basePort),
	}, " "))
	defer dockerRun2.Close()
	defer removeDockerContainer(t, cID2)

	cID3 := createDockerID("starter-test-cluster-recovery3-")
	dockerRun3 := Spawn(t, strings.Join([]string{
		"docker run -i",
		"--label starter-test=true",
		"--name=" + cID3,
		createLicenseKeyOption(),
		fmt.Sprintf("-p %d:%d", basePort+200, basePort+200),
		fmt.Sprintf("-v %s:/data", volID3),
		"-v /var/run/docker.sock:/var/run/docker.sock",
		"arangodb/arangodb-starter",
		"--docker.container=" + cID3,
		fmt.Sprintf("--starter.port=%d", basePort+200),
		"--starter.address=$IP",
		createEnvironmentStarterOptions(),
		fmt.Sprintf("--starter.join=$IP:%d", basePort),
	}, " "))
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
	removeDockerVolume(t, volID2)

	checkpoint.Log("Recovery")

	// Create new volume
	recVolID2 := createDockerID("vol-starter-test-cluster-recovery2-recovery-")
	createDockerVolume(t, recVolID2)
	defer removeDockerVolume(t, recVolID2)

	// Create RECOVERY file in new volume
	recoveryContent := fmt.Sprintf("%s:%d", os.Getenv("IP"), basePort+(100))
	dockerBuildRecoveryRun := Spawn(t, strings.Join([]string{
		"docker run -i",
		"--label starter-test=true",
		"--name=" + cID2 + "recovery-builder",
		fmt.Sprintf("-v %s:/data", recVolID2),
		"alpine",
		fmt.Sprintf("sh -c \"echo %s > /data/RECOVERY\"", recoveryContent),
	}, " "))
	dockerBuildRecoveryRun.Wait()

	checkpoint.Log("Wait for port to be closed")
	WaitForHttpPortClosed(checkpoint, NewThrottle(time.Second), insecureStarterEndpoint(100)).ExecuteT(t, time.Minute, time.Second)

	checkpoint.Log("Start docker container")
	// Restart dockerRun2
	recCID2 := createDockerID("starter-test-cluster-recovery2-recovery-")
	recDockerRun2 := Spawn(t, strings.Join([]string{
		"docker run -i",
		"--label starter-test=true",
		"--name=" + recCID2,
		createLicenseKeyOption(),
		fmt.Sprintf("-p %d:%d", basePort+100, basePort+100),
		fmt.Sprintf("-v %s:/data", recVolID2),
		"-v /var/run/docker.sock:/var/run/docker.sock",
		"arangodb/arangodb-starter",
		"--docker.container=" + recCID2,
		fmt.Sprintf("--starter.port=%d", basePort+100),
		"--starter.address=$IP",
		createEnvironmentStarterOptions(),
		fmt.Sprintf("--starter.join=$IP:%d", basePort),
	}, " "))
	defer recDockerRun2.Close()
	defer removeDockerContainer(t, recCID2)
	checkpoint.Log("Docker container started")

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
