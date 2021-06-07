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

// TestDockerResilientSingleLocal runs the arangodb starter in docker with mode `resilientsingle` & `--starter.local`
func TestDockerResilientSingleLocal(t *testing.T) {
	needTestMode(t, testModeDocker)
	needStarterMode(t, starterModeActiveFailover)
	if os.Getenv("IP") == "" {
		t.Fatal("IP envvar must be set to IP address of this machine")
	}
	/*
		docker volume create arangodb
		docker run -i --name=adb --rm -p 8528:8528 \
			-v arangodb:/data \
			-v /var/run/docker.sock:/var/run/docker.sock \
			arangodb/arangodb-starter \
			--docker.container=adb \
			--starter.address=$IP \
			--starter.mode=resilientsingle \
			--starter.local
	*/
	volID := createDockerID("vol-starter-test-local-resilientsingle-")
	createDockerVolume(t, volID)
	defer removeDockerVolume(t, volID)

	// Cleanup of left over tests
	removeDockerContainersByLabel(t, "starter-test=true")
	removeStarterCreatedDockerContainers(t)

	start := time.Now()

	cID := createDockerID("starter-test-local-resilientsingle-")
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
		"--starter.mode=resilientsingle",
		"--starter.local",
		createEnvironmentStarterOptions(),
	}, " "))
	defer dockerRun.Close()
	defer removeDockerContainer(t, cID)

	if ok := WaitUntilStarterReady(t, whatResilientSingle, 1, dockerRun); ok {
		t.Logf("ResilientSingle start took %s", time.Since(start))
		testResilientSingle(t, insecureStarterEndpoint(0*portIncrement), false, false)
	}

	waitForCallFunction(t,
		ShutdownStarterCall(insecureStarterEndpoint(0*portIncrement)))
}

// TestDockerResilientSingleLocalSecure runs the arangodb starter in docker with mode `resilientsingle`,
// `--starter.local` & `--ssl.auto-key`
func TestDockerResilientSingleLocalSecure(t *testing.T) {
	needTestMode(t, testModeDocker)
	needStarterMode(t, starterModeActiveFailover)
	if os.Getenv("IP") == "" {
		t.Fatal("IP envvar must be set to IP address of this machine")
	}
	/*
		docker volume create arangodb
		docker run -i --name=adb --rm -p 8528:8528 \
			-v arangodb:/data \
			-v /var/run/docker.sock:/var/run/docker.sock \
			arangodb/arangodb-starter \
			--docker.container=adb \
			--starter.address=$IP \
			--starter.mode=resilientsingle \
			--starter.local
	*/
	volID := createDockerID("vol-starter-test-local-resilientsingle-secure-")
	createDockerVolume(t, volID)
	defer removeDockerVolume(t, volID)

	// Cleanup of left over tests
	removeDockerContainersByLabel(t, "starter-test=true")
	removeStarterCreatedDockerContainers(t)

	start := time.Now()

	cID := createDockerID("starter-test-local-resilientsingle-secure-")
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
		"--starter.mode=resilientsingle",
		"--starter.local",
		"--ssl.auto-key",
		createEnvironmentStarterOptions(),
	}, " "))
	defer dockerRun.Close()
	defer removeDockerContainer(t, cID)

	if ok := WaitUntilStarterReady(t, whatResilientSingle, 1, dockerRun); ok {
		t.Logf("ResilientSingle start took %s", time.Since(start))
		testResilientSingle(t, secureStarterEndpoint(0*portIncrement), true, false)
	}

	waitForCallFunction(t,
		ShutdownStarterCall(secureStarterEndpoint(0*portIncrement)))
}
