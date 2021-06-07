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

// TestDockerActiveFailoverDefault runs 3 arangodb starters in docker with mode=activefailover
// and otherwise default settings.
func TestDockerActiveFailoverDefault(t *testing.T) {
	log := GetLogger(t)
	defer log.Clean()

	needTestMode(t, testModeDocker)
	needStarterMode(t, starterModeActiveFailover)
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
			--starter.mode=activefailover
	*/

	volID1 := createDockerID("vol-starter-test-activefailover-default1-")
	createDockerVolume(t, volID1)
	defer removeDockerVolume(t, volID1)

	volID2 := createDockerID("vol-starter-test-activefailover-default2-")
	createDockerVolume(t, volID2)
	defer removeDockerVolume(t, volID2)

	volID3 := createDockerID("vol-starter-test-activefailover-default3-")
	createDockerVolume(t, volID3)
	defer removeDockerVolume(t, volID3)

	// Cleanup of left over tests
	removeDockerContainersByLabel(t, "starter-test=true")
	removeStarterCreatedDockerContainers(t)

	start := time.Now()

	cID1 := createDockerID("starter-test-activefailover-default1-")
	dockerRun1 := Spawn(t, strings.Join([]string{
		"docker run -i",
		"--label starter-test=true",
		"--name=" + cID1,
		"--rm",
		createLicenseKeyOption(),
		fmt.Sprintf("-p %d:%d", basePort, basePort),
		fmt.Sprintf("-v %s:/data", volID1),
		"-v /var/run/docker.sock:/var/run/docker.sock",
		"arangodb/arangodb-starter",
		"--docker.container=" + cID1,
		"--starter.address=$IP",
		"--starter.mode=activefailover",
		createEnvironmentStarterOptions(),
	}, " "))
	defer dockerRun1.Close()
	defer removeDockerContainer(t, cID1)

	cID2 := createDockerID("starter-test-activefailover-default2-")
	dockerRun2 := Spawn(t, strings.Join([]string{
		"docker run -i",
		"--label starter-test=true",
		"--name=" + cID2,
		"--rm",
		createLicenseKeyOption(),
		fmt.Sprintf("-p %d:%d", basePort+(1*portIncrement), basePort),
		fmt.Sprintf("-v %s:/data", volID2),
		"-v /var/run/docker.sock:/var/run/docker.sock",
		"arangodb/arangodb-starter",
		"--docker.container=" + cID2,
		"--starter.address=$IP",
		"--starter.mode=activefailover",
		createEnvironmentStarterOptions(),
		fmt.Sprintf("--starter.join=$IP:%d", basePort),
	}, " "))
	defer dockerRun2.Close()
	defer removeDockerContainer(t, cID2)

	cID3 := createDockerID("starter-test-activefailover-default3-")
	dockerRun3 := Spawn(t, strings.Join([]string{
		"docker run -i",
		"--label starter-test=true",
		"--name=" + cID3,
		"--rm",
		createLicenseKeyOption(),
		fmt.Sprintf("-p %d:%d", basePort+(2*portIncrement), basePort),
		fmt.Sprintf("-v %s:/data", volID3),
		"-v /var/run/docker.sock:/var/run/docker.sock",
		"arangodb/arangodb-starter",
		"--docker.container=" + cID3,
		"--starter.address=$IP",
		"--starter.mode=activefailover",
		createEnvironmentStarterOptions(),
		fmt.Sprintf("--starter.join=$IP:%d", basePort),
	}, " "))
	defer dockerRun3.Close()
	defer removeDockerContainer(t, cID3)

	if ok := WaitUntilStarterReady(t, whatResilientSingle, 1, dockerRun1, dockerRun2, dockerRun3); ok {
		t.Logf("ActiveFailover start took %s", time.Since(start))
		testResilientSingle(t, insecureStarterEndpoint(0*portIncrement), false, false)
		testResilientSingle(t, insecureStarterEndpoint(1*portIncrement), false, false)
		testResilientSingle(t, insecureStarterEndpoint(2*portIncrement), false, false)
	}

	waitForCallFunction(t, ShutdownStarterCall(insecureStarterEndpoint(0*portIncrement)),
		ShutdownStarterCall(insecureStarterEndpoint(1*portIncrement)),
		ShutdownStarterCall(insecureStarterEndpoint(2*portIncrement)))
}

// TestDockerActiveFailover2Instance runs 3 arangodb starters in docker with mode=activefailover
// and only 2 servers should start a single server instance.
func TestDockerActiveFailover2Instance(t *testing.T) {
	needTestMode(t, testModeDocker)
	needStarterMode(t, starterModeActiveFailover)
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
			--starter.mode=activefailover
	*/
	volID1 := createDockerID("vol-starter-test-activefailover-default1-")
	createDockerVolume(t, volID1)
	defer removeDockerVolume(t, volID1)

	volID2 := createDockerID("vol-starter-test-activefailover-default2-")
	createDockerVolume(t, volID2)
	defer removeDockerVolume(t, volID2)

	volID3 := createDockerID("vol-starter-test-activefailover-default3-")
	createDockerVolume(t, volID3)
	defer removeDockerVolume(t, volID3)

	// Cleanup of left over tests
	removeDockerContainersByLabel(t, "starter-test=true")
	removeStarterCreatedDockerContainers(t)

	start := time.Now()

	cID1 := createDockerID("starter-test-activefailover-default1-")
	dockerRun1 := Spawn(t, strings.Join([]string{
		"docker run -i",
		"--label starter-test=true",
		"--name=" + cID1,
		"--rm",
		createLicenseKeyOption(),
		fmt.Sprintf("-p %d:%d", basePort, basePort),
		fmt.Sprintf("-v %s:/data", volID1),
		"-v /var/run/docker.sock:/var/run/docker.sock",
		"arangodb/arangodb-starter",
		"--docker.container=" + cID1,
		"--starter.address=$IP",
		"--starter.mode=activefailover",
		createEnvironmentStarterOptions(),
	}, " "))
	defer dockerRun1.Close()
	defer removeDockerContainer(t, cID1)

	cID2 := createDockerID("starter-test-activefailover-default2-")
	dockerRun2 := Spawn(t, strings.Join([]string{
		"docker run -i",
		"--label starter-test=true",
		"--name=" + cID2,
		"--rm",
		createLicenseKeyOption(),
		fmt.Sprintf("-p %d:%d", basePort+(1*portIncrement), basePort),
		fmt.Sprintf("-v %s:/data", volID2),
		"-v /var/run/docker.sock:/var/run/docker.sock",
		"arangodb/arangodb-starter",
		"--docker.container=" + cID2,
		"--starter.address=$IP",
		"--starter.mode=activefailover",
		createEnvironmentStarterOptions(),
		fmt.Sprintf("--starter.join=$IP:%d", basePort),
	}, " "))
	defer dockerRun2.Close()
	defer removeDockerContainer(t, cID2)

	cID3 := createDockerID("starter-test-activefailover-default3-")
	dockerRun3 := Spawn(t, strings.Join([]string{
		"docker run -i",
		"--label starter-test=true",
		"--name=" + cID3,
		"--rm",
		createLicenseKeyOption(),
		fmt.Sprintf("-p %d:%d", basePort+(2*portIncrement), basePort),
		fmt.Sprintf("-v %s:/data", volID3),
		"-v /var/run/docker.sock:/var/run/docker.sock",
		"arangodb/arangodb-starter",
		"--docker.container=" + cID3,
		"--starter.address=$IP",
		"--starter.mode=activefailover",
		"--cluster.start-single=false",
		createEnvironmentStarterOptions(),
		fmt.Sprintf("--starter.join=$IP:%d", basePort),
	}, " "))
	defer dockerRun3.Close()
	defer removeDockerContainer(t, cID3)

	if ok := WaitUntilStarterReady(t, whatResilientSingle, 1, dockerRun1, dockerRun2 /*not docker3*/); ok {
		t.Logf("ActiveFailover start took %s", time.Since(start))
		testResilientSingle(t, insecureStarterEndpoint(0*portIncrement), false, false)
		testResilientSingle(t, insecureStarterEndpoint(1*portIncrement), false, false)
		testResilientSingle(t, insecureStarterEndpoint(2*portIncrement), false, true)
	}

	waitForCallFunction(t,
		ShutdownStarterCall(insecureStarterEndpoint(0*portIncrement)),
		ShutdownStarterCall(insecureStarterEndpoint(1*portIncrement)),
		ShutdownStarterCall(insecureStarterEndpoint(2*portIncrement)))
}
