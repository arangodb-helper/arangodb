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
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// TestDockerClusterDefault runs 3 arangodb starters in docker with default settings.
func TestDockerClusterDefault(t *testing.T) {
	needTestMode(t, testModeDocker)
	if os.Getenv("IP") == "" {
		t.Fatal("IP envvar must be set to IP address of this machine")
	}
	/*
		docker volume create arangodb1
		docker run -it --name=adb1 --rm -p 8528:8528 \
			-v arangodb1:/data \
			-v /var/run/docker.sock:/var/run/docker.sock \
			arangodb/arangodb-starter \
			--docker.container=adb1 \
			--starter.address=$IP
	*/
	volID1 := createDockerID("vol-starter-test-cluster-default1-")
	createDockerVolume(t, volID1)
	defer removeDockerVolume(t, volID1)

	volID2 := createDockerID("vol-starter-test-cluster-default2-")
	createDockerVolume(t, volID2)
	defer removeDockerVolume(t, volID2)

	volID3 := createDockerID("vol-starter-test-cluster-default3-")
	createDockerVolume(t, volID3)
	defer removeDockerVolume(t, volID3)

	// Cleanup of left over tests
	removeDockerContainersByLabel(t, "starter-test=true")
	removeStarterCreatedDockerContainers(t)

	start := time.Now()

	cID1 := createDockerID("starter-test-cluster-default1-")
	dockerRun1 := Spawn(t, strings.Join([]string{
		"docker run -it",
		"--label starter-test=true",
		"--name=" + cID1,
		"--rm",
		fmt.Sprintf("-p %d:%d", basePort, basePort),
		fmt.Sprintf("-v %s:/data", volID1),
		"-v /var/run/docker.sock:/var/run/docker.sock",
		"arangodb/arangodb-starter",
		"--docker.container=" + cID1,
		"--starter.address=$IP",
	}, " "))
	defer dockerRun1.Close()
	defer removeDockerContainer(t, cID1)

	cID2 := createDockerID("starter-test-cluster-default2-")
	dockerRun2 := Spawn(t, strings.Join([]string{
		"docker run -it",
		"--label starter-test=true",
		"--name=" + cID2,
		"--rm",
		fmt.Sprintf("-p %d:%d", basePort+5, basePort),
		fmt.Sprintf("-v %s:/data", volID2),
		"-v /var/run/docker.sock:/var/run/docker.sock",
		"arangodb/arangodb-starter",
		"--docker.container=" + cID2,
		"--starter.address=$IP",
		fmt.Sprintf("--starter.join=$IP:%d", basePort),
	}, " "))
	defer dockerRun2.Close()
	defer removeDockerContainer(t, cID2)

	cID3 := createDockerID("starter-test-cluster-default3-")
	dockerRun3 := Spawn(t, strings.Join([]string{
		"docker run -it",
		"--label starter-test=true",
		"--name=" + cID3,
		"--rm",
		fmt.Sprintf("-p %d:%d", basePort+10, basePort),
		fmt.Sprintf("-v %s:/data", volID3),
		"-v /var/run/docker.sock:/var/run/docker.sock",
		"arangodb/arangodb-starter",
		"--docker.container=" + cID3,
		"--starter.address=$IP",
		fmt.Sprintf("--starter.join=$IP:%d", basePort),
	}, " "))
	defer dockerRun3.Close()
	defer removeDockerContainer(t, cID3)

	if ok := WaitUntilStarterReady(t, whatCluster, dockerRun1, dockerRun2, dockerRun3); ok {
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

// TestOldDockerClusterDefault runs 3 arangodb starters in docker with default settings.
func TestOldDockerClusterDefault(t *testing.T) {
	needTestMode(t, testModeDocker)
	if os.Getenv("IP") == "" {
		t.Fatal("IP envvar must be set to IP address of this machine")
	}
	/*
		docker volume create arangodb1
		docker run -it --name=adb1 --rm -p 8528:8528 \
			-v arangodb1:/data \
			-v /var/run/docker.sock:/var/run/docker.sock \
			arangodb/arangodb-starter \
			--dockerContainer=adb1 --ownAddress=$IP \
			--local
	*/
	volID1 := createDockerID("vol-starter-test-cluster-default1-")
	createDockerVolume(t, volID1)
	defer removeDockerVolume(t, volID1)

	volID2 := createDockerID("vol-starter-test-cluster-default2-")
	createDockerVolume(t, volID2)
	defer removeDockerVolume(t, volID2)

	volID3 := createDockerID("vol-starter-test-cluster-default3-")
	createDockerVolume(t, volID3)
	defer removeDockerVolume(t, volID3)

	// Cleanup of left over tests
	removeDockerContainersByLabel(t, "starter-test=true")
	removeStarterCreatedDockerContainers(t)

	start := time.Now()

	cID1 := createDockerID("starter-test-cluster-default1-")
	dockerRun1 := Spawn(t, strings.Join([]string{
		"docker run -it",
		"--label starter-test=true",
		"--name=" + cID1,
		"--rm",
		fmt.Sprintf("-p %d:%d", basePort, basePort),
		fmt.Sprintf("-v %s:/data", volID1),
		"-v /var/run/docker.sock:/var/run/docker.sock",
		"arangodb/arangodb-starter",
		"--dockerContainer=" + cID1,
		"--ownAddress=$IP",
	}, " "))
	defer dockerRun1.Close()
	defer removeDockerContainer(t, cID1)

	cID2 := createDockerID("starter-test-cluster-default2-")
	dockerRun2 := Spawn(t, strings.Join([]string{
		"docker run -it",
		"--label starter-test=true",
		"--name=" + cID2,
		"--rm",
		fmt.Sprintf("-p %d:%d", basePort+5, basePort),
		fmt.Sprintf("-v %s:/data", volID2),
		"-v /var/run/docker.sock:/var/run/docker.sock",
		"arangodb/arangodb-starter",
		"--dockerContainer=" + cID2,
		"--ownAddress=$IP",
		fmt.Sprintf("--join=$IP:%d", basePort),
	}, " "))
	defer dockerRun2.Close()
	defer removeDockerContainer(t, cID2)

	cID3 := createDockerID("starter-test-cluster-default3-")
	dockerRun3 := Spawn(t, strings.Join([]string{
		"docker run -it",
		"--label starter-test=true",
		"--name=" + cID3,
		"--rm",
		fmt.Sprintf("-p %d:%d", basePort+10, basePort),
		fmt.Sprintf("-v %s:/data", volID3),
		"-v /var/run/docker.sock:/var/run/docker.sock",
		"arangodb/arangodb-starter",
		"--dockerContainer=" + cID3,
		"--ownAddress=$IP",
		fmt.Sprintf("--join=$IP:%d", basePort),
	}, " "))
	defer dockerRun3.Close()
	defer removeDockerContainer(t, cID3)

	if ok := WaitUntilStarterReady(t, whatCluster, dockerRun1, dockerRun2, dockerRun3); ok {
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
