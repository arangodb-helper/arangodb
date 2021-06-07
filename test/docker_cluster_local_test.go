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

// TestDockerClusterLocal runs the arangodb starter in docker with `--local`
func TestDockerClusterLocal(t *testing.T) {
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
			--starter.address=$IP \
			--starter.local
	*/
	volID := createDockerID("vol-starter-test-local-cluster-")
	createDockerVolume(t, volID)
	defer removeDockerVolume(t, volID)

	// Cleanup of left over tests
	removeDockerContainersByLabel(t, "starter-test=true")
	removeStarterCreatedDockerContainers(t)

	start := time.Now()

	cID := createDockerID("starter-test-local-cluster-")
	dockerRun := Spawn(t, strings.Join([]string{
		"docker run -i",
		"--label starter-test=true",
		"--name=" + cID,
		"--rm",
		createLicenseKeyOption(),
		fmt.Sprintf("-p %d:%d", basePort, basePort),
		fmt.Sprintf("-v %s:/data", volID),
		"-v /var/run/docker.sock:/var/run/docker.sock",
		"arangodb/arangodb-starter",
		"--docker.container=" + cID,
		"--starter.address=$IP",
		"--starter.local",
		createEnvironmentStarterOptions(),
	}, " "))
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
			--starter.address=$IP \
			--starter.local \
			--cluster.agency-size=1
	*/
	volID := createDockerID("vol-starter-test-local-cluster-as1-")
	createDockerVolume(t, volID)
	defer removeDockerVolume(t, volID)

	// Cleanup of left over tests
	removeDockerContainersByLabel(t, "starter-test=true")
	removeStarterCreatedDockerContainers(t)

	start := time.Now()

	cID := createDockerID("starter-test-local-cluster-as1-")
	dockerRun := Spawn(t, strings.Join([]string{
		"docker run -i",
		"--label starter-test=true",
		"--name=" + cID,
		"--rm",
		createLicenseKeyOption(),
		fmt.Sprintf("-p %d:%d", basePort, basePort),
		fmt.Sprintf("-v %s:/data", volID),
		"-v /var/run/docker.sock:/var/run/docker.sock",
		"arangodb/arangodb-starter",
		"--docker.container=" + cID,
		"--starter.address=$IP",
		"--starter.local",
		"--cluster.agency-size=1",
		createEnvironmentStarterOptions(),
	}, " "))
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
			--dockerContainer=adb1 --ownAddress=$IP \
			--local
	*/
	volID := createDockerID("vol-starter-test-local-cluster-")
	createDockerVolume(t, volID)
	defer removeDockerVolume(t, volID)

	// Cleanup of left over tests
	removeDockerContainersByLabel(t, "starter-test=true")
	removeStarterCreatedDockerContainers(t)

	start := time.Now()

	cID := createDockerID("starter-test-local-cluster-")
	dockerRun := Spawn(t, strings.Join([]string{
		"docker run -i",
		"--label starter-test=true",
		"--name=" + cID,
		"--rm",
		createLicenseKeyOption(),
		fmt.Sprintf("-p %d:%d", basePort, basePort),
		fmt.Sprintf("-v %s:/data", volID),
		"-v /var/run/docker.sock:/var/run/docker.sock",
		"arangodb/arangodb-starter",
		"--dockerContainer=" + cID,
		"--ownAddress=$IP",
		"--local",
		createEnvironmentStarterOptions(),
	}, " "))
	defer dockerRun.Close()
	defer removeDockerContainer(t, cID)

	if ok := WaitUntilStarterReady(t, whatCluster, 1, dockerRun); ok {
		t.Logf("Cluster start took %s", time.Since(start))
		testCluster(t, insecureStarterEndpoint(0*portIncrement), false)
	}

	waitForCallFunction(t,
		ShutdownStarterCall(insecureStarterEndpoint(0*portIncrement)))
}
