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
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestDockerClusterDifferentLogDir runs 3 arangodb starters in docker with a custom log dir.
func TestDockerClusterDifferentLogDir(t *testing.T) {
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

	logDir, err := os.MkdirTemp("", "")
	require.NoError(t, err)

	// Cleanup of left over tests
	removeDockerContainersByLabel(t, "starter-test=true")
	removeStarterCreatedDockerContainers(t)

	start := time.Now()

	joins := fmt.Sprintf("localhost:%d,localhost:%d,localhost:%d", basePort, basePort+(1*portIncrement), basePort+(2*portIncrement))

	masterLogDir := filepath.Join(logDir, "master")
	require.NoError(t, os.Mkdir(masterLogDir, 0755))
	defer os.RemoveAll(masterLogDir)

	dockerRun1 := spawnMemberInDocker(t, basePort, cID1, joins, "--log.dir="+masterLogDir, fmt.Sprintf("-v %s:%s", masterLogDir, masterLogDir))
	defer dockerRun1.Close()
	defer removeDockerContainer(t, cID1)

	slave1LogDir := filepath.Join(logDir, "slave1")
	require.NoError(t, os.Mkdir(slave1LogDir, 0755))

	dockerRun2 := spawnMemberInDocker(t, basePort+(1*portIncrement), cID2, joins, "--log.dir="+slave1LogDir, fmt.Sprintf("-v %s:%s", slave1LogDir, slave1LogDir))
	defer dockerRun2.Close()
	defer removeDockerContainer(t, cID2)

	slave2LogDir := filepath.Join(logDir, "slave2")
	require.NoError(t, os.Mkdir(slave2LogDir, 0755))

	dockerRun3 := spawnMemberInDocker(t, basePort+(2*portIncrement), cID3, joins, "--log.dir="+slave2LogDir, fmt.Sprintf("-v %s:%s", slave2LogDir, slave2LogDir))
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

	logDir, err := os.MkdirTemp("", "")
	require.NoError(t, err)

	// Cleanup of left over tests
	removeDockerContainersByLabel(t, "starter-test=true")
	removeStarterCreatedDockerContainers(t)

	start := time.Now()

	joins := fmt.Sprintf("localhost:%d,localhost:%d,localhost:%d", basePort, basePort+(1*portIncrement), basePort+(2*portIncrement))

	masterLogDir := filepath.Join(logDir, "master")
	require.NoError(t, os.Mkdir(masterLogDir, 0755))
	defer os.RemoveAll(masterLogDir)

	dockerRun1 := spawnMemberInDocker(t, basePort, cID1, joins, "--log.file=false --log.dir="+masterLogDir, fmt.Sprintf("-v %s:%s", masterLogDir, masterLogDir))
	defer dockerRun1.Close()
	defer removeDockerContainer(t, cID1)

	slave1LogDir := filepath.Join(logDir, "slave1")
	require.NoError(t, os.Mkdir(slave1LogDir, 0755))

	dockerRun2 := spawnMemberInDocker(t, basePort+(1*portIncrement), cID2, joins, "--log.file=false --log.dir="+slave1LogDir, fmt.Sprintf("-v %s:%s", slave1LogDir, slave1LogDir))
	defer dockerRun2.Close()
	defer removeDockerContainer(t, cID2)

	slave2LogDir := filepath.Join(logDir, "slave2")
	require.NoError(t, os.Mkdir(slave2LogDir, 0755))

	dockerRun3 := spawnMemberInDocker(t, basePort+(2*portIncrement), cID3, joins, "--log.file=false --log.dir="+slave2LogDir, fmt.Sprintf("-v %s:%s", slave2LogDir, slave2LogDir))
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
