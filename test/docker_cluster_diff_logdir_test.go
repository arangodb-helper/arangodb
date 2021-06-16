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
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestDockerClusterDifferentLogDir runs 3 arangodb starters in docker with a custom log dir.
func TestDockerClusterDifferentLogDir(t *testing.T) {
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
			-v something:something \
			arangodb/arangodb-starter \
			--docker.container=adb1 \
			--starter.address=$IP
			--log.dir=something
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

	logDir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatalf("TempDir failed: %v", err)
	}

	// Cleanup of left over tests
	removeDockerContainersByLabel(t, "starter-test=true")
	removeStarterCreatedDockerContainers(t)

	start := time.Now()

	masterLogDir := filepath.Join(logDir, "master")
	cID1 := createDockerID("starter-test-cluster-default1-")
	dockerRun1 := Spawn(t, strings.Join([]string{
		"docker run -i",
		"--label starter-test=true",
		"--name=" + cID1,
		"--rm",
		createLicenseKeyOption(),
		fmt.Sprintf("-p %d:%d", basePort, basePort),
		fmt.Sprintf("-v %s:/data", volID1),
		fmt.Sprintf("-v %s:%s", masterLogDir, masterLogDir),
		"-v /var/run/docker.sock:/var/run/docker.sock",
		"arangodb/arangodb-starter",
		"--docker.container=" + cID1,
		"--starter.address=$IP",
		"--log.dir=" + masterLogDir,
		createEnvironmentStarterOptions(),
	}, " "))
	defer dockerRun1.Close()
	defer removeDockerContainer(t, cID1)

	slave1LogDir := filepath.Join(logDir, "slave1")
	cID2 := createDockerID("starter-test-cluster-default2-")
	dockerRun2 := Spawn(t, strings.Join([]string{
		"docker run -i",
		"--label starter-test=true",
		"--name=" + cID2,
		"--rm",
		createLicenseKeyOption(),
		fmt.Sprintf("-p %d:%d", basePort+(1*portIncrement), basePort),
		fmt.Sprintf("-v %s:/data", volID2),
		fmt.Sprintf("-v %s:%s", slave1LogDir, slave1LogDir),
		"-v /var/run/docker.sock:/var/run/docker.sock",
		"arangodb/arangodb-starter",
		"--docker.container=" + cID2,
		"--starter.address=$IP",
		"--log.dir=" + slave1LogDir,
		createEnvironmentStarterOptions(),
		fmt.Sprintf("--starter.join=$IP:%d", basePort),
	}, " "))
	defer dockerRun2.Close()
	defer removeDockerContainer(t, cID2)

	slave2LogDir := filepath.Join(logDir, "slave2")
	cID3 := createDockerID("starter-test-cluster-default3-")
	dockerRun3 := Spawn(t, strings.Join([]string{
		"docker run -i",
		"--label starter-test=true",
		"--name=" + cID3,
		"--rm",
		createLicenseKeyOption(),
		fmt.Sprintf("-p %d:%d", basePort+(2*portIncrement), basePort),
		fmt.Sprintf("-v %s:/data", volID3),
		fmt.Sprintf("-v %s:%s", slave2LogDir, slave2LogDir),
		"-v /var/run/docker.sock:/var/run/docker.sock",
		"arangodb/arangodb-starter",
		"--docker.container=" + cID3,
		"--starter.address=$IP",
		"--log.dir=" + slave2LogDir,
		createEnvironmentStarterOptions(),
		fmt.Sprintf("--starter.join=$IP:%d", basePort),
	}, " "))
	defer dockerRun3.Close()
	defer removeDockerContainer(t, cID3)

	if ok := WaitUntilStarterReady(t, whatCluster, 3, dockerRun1, dockerRun2, dockerRun3); ok {
		t.Logf("Cluster start took %s", time.Since(start))
		testCluster(t, insecureStarterEndpoint(0*portIncrement), false)
		testCluster(t, insecureStarterEndpoint(1*portIncrement), false)
		testCluster(t, insecureStarterEndpoint(2*portIncrement), false)
	}

	waitForCallFunction(t,
		ShutdownStarterCall(insecureStarterEndpoint(0*portIncrement)),
		ShutdownStarterCall(insecureStarterEndpoint(1*portIncrement)),
		ShutdownStarterCall(insecureStarterEndpoint(2*portIncrement)))

	check := func(rootDir string, expectedFileCount int) {
		files, err := getRecursiveLogFiles(rootDir)
		if err != nil {
			t.Errorf("Failed to get log files in %s: %v", rootDir, err)
		} else if len(files) != expectedFileCount {
			t.Errorf("Expected %d log files in %s, got %d (%v)", expectedFileCount, rootDir, len(files), files)
		}
	}
	check(masterLogDir, 3+1) // +1 == arangodb.log (starter log)
	check(slave1LogDir, 3+1)
	check(slave2LogDir, 3+1)
}

// TestDockerClusterDifferentLogDirNoLog2File runs 3 arangodb starters in docker with a custom log dir without writing
// starter log to file.
func TestDockerClusterDifferentLogDirNoLog2File(t *testing.T) {
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
			-v something:something \
			arangodb/arangodb-starter \
			--docker.container=adb1 \
			--starter.address=$IP
			--log.dir=something
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

	logDir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatalf("TempDir failed: %v", err)
	}

	// Cleanup of left over tests
	removeDockerContainersByLabel(t, "starter-test=true")
	removeStarterCreatedDockerContainers(t)

	start := time.Now()

	masterLogDir := filepath.Join(logDir, "master")
	cID1 := createDockerID("starter-test-cluster-default1-")
	dockerRun1 := Spawn(t, strings.Join([]string{
		"docker run -i",
		"--label starter-test=true",
		"--name=" + cID1,
		"--rm",
		createLicenseKeyOption(),
		fmt.Sprintf("-p %d:%d", basePort, basePort),
		fmt.Sprintf("-v %s:/data", volID1),
		fmt.Sprintf("-v %s:%s", masterLogDir, masterLogDir),
		"-v /var/run/docker.sock:/var/run/docker.sock",
		"arangodb/arangodb-starter",
		"--docker.container=" + cID1,
		"--starter.address=$IP",
		"--log.file=false",
		"--log.dir=" + masterLogDir,
		createEnvironmentStarterOptions(),
	}, " "))
	defer dockerRun1.Close()
	defer removeDockerContainer(t, cID1)

	slave1LogDir := filepath.Join(logDir, "slave1")
	cID2 := createDockerID("starter-test-cluster-default2-")
	dockerRun2 := Spawn(t, strings.Join([]string{
		"docker run -i",
		"--label starter-test=true",
		"--name=" + cID2,
		"--rm",
		createLicenseKeyOption(),
		fmt.Sprintf("-p %d:%d", basePort+(1*portIncrement), basePort),
		fmt.Sprintf("-v %s:/data", volID2),
		fmt.Sprintf("-v %s:%s", slave1LogDir, slave1LogDir),
		"-v /var/run/docker.sock:/var/run/docker.sock",
		"arangodb/arangodb-starter",
		"--docker.container=" + cID2,
		"--starter.address=$IP",
		"--log.file=false",
		"--log.dir=" + slave1LogDir,
		createEnvironmentStarterOptions(),
		fmt.Sprintf("--starter.join=$IP:%d", basePort),
	}, " "))
	defer dockerRun2.Close()
	defer removeDockerContainer(t, cID2)

	slave2LogDir := filepath.Join(logDir, "slave2")
	cID3 := createDockerID("starter-test-cluster-default3-")
	dockerRun3 := Spawn(t, strings.Join([]string{
		"docker run -i",
		"--label starter-test=true",
		"--name=" + cID3,
		"--rm",
		createLicenseKeyOption(),
		fmt.Sprintf("-p %d:%d", basePort+(2*portIncrement), basePort),
		fmt.Sprintf("-v %s:/data", volID3),
		fmt.Sprintf("-v %s:%s", slave2LogDir, slave2LogDir),
		"-v /var/run/docker.sock:/var/run/docker.sock",
		"arangodb/arangodb-starter",
		"--docker.container=" + cID3,
		"--starter.address=$IP",
		"--log.file=false",
		"--log.dir=" + slave2LogDir,
		createEnvironmentStarterOptions(),
		fmt.Sprintf("--starter.join=$IP:%d", basePort),
	}, " "))
	defer dockerRun3.Close()
	defer removeDockerContainer(t, cID3)

	if ok := WaitUntilStarterReady(t, whatCluster, 3, dockerRun1, dockerRun2, dockerRun3); ok {
		t.Logf("Cluster start took %s", time.Since(start))
		testCluster(t, insecureStarterEndpoint(0*portIncrement), false)
		testCluster(t, insecureStarterEndpoint(1*portIncrement), false)
		testCluster(t, insecureStarterEndpoint(2*portIncrement), false)
	}

	waitForCallFunction(t,
		ShutdownStarterCall(insecureStarterEndpoint(0*portIncrement)),
		ShutdownStarterCall(insecureStarterEndpoint(1*portIncrement)),
		ShutdownStarterCall(insecureStarterEndpoint(2*portIncrement)))

	check := func(rootDir string, expectedFileCount int) {
		files, err := getRecursiveLogFiles(rootDir)
		if err != nil {
			t.Errorf("Failed to get log files in %s: %v", rootDir, err)
		} else if len(files) != expectedFileCount {
			t.Errorf("Expected %d log files in %s, got %d (%v)", expectedFileCount, rootDir, len(files), files)
		}
	}
	check(masterLogDir, 3)
	check(slave1LogDir, 3)
	check(slave2LogDir, 3)
}
