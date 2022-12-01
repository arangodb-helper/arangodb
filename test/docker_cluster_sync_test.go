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
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestDockerClusterSync runs 3 arangodb starters in docker with arangosync enabled.
func TestDockerClusterSync(t *testing.T) {
	needTestMode(t, testModeDocker)
	needStarterMode(t, starterModeCluster)
	needEnterprise(t)
	ip := os.Getenv("IP")
	if ip == "" {
		t.Fatal("IP envvar must be set to IP address of this machine")
	}

	// Create certificates
	certs := createSyncCertificates(t, ip, true)

	volumeIDs, cleanVolumes := createDockerVolumes(t,
		"vol-starter-test-cluster-sync1-",
		"vol-starter-test-cluster-sync2-",
		"vol-starter-test-cluster-sync3-",
	)
	defer cleanVolumes()

	// Cleanup of left over tests
	removeDockerContainersByLabel(t, "starter-test=true")
	removeStarterCreatedDockerContainers(t)

	start := time.Now()

	baseStarterArgs := []string{
		"--starter.address=$IP",
		"--server.storage-engine=rocksdb",
		"--auth.jwt-secret=/certs/" + filepath.Base(certs.ClusterSecret),
		createEnvironmentStarterOptions(),
	}
	syncArgs := []string{
		"--starter.sync",
		"--sync.server.keyfile=/certs/" + filepath.Base(certs.TLS.DCA.Keyfile),
		"--sync.server.client-cafile=/certs/" + filepath.Base(certs.ClientAuth.CACertificate),
		"--sync.master.jwt-secret=/certs/" + filepath.Base(certs.MasterSecret),
		"--sync.monitoring.token=" + syncMonitoringToken,
	}
	prepareStarterContainerArgs := func(master bool, containerName, volumeID string, exposedPort int, moreArgs ...string) []string {
		args := []string{
			"docker run -i",
			"--label starter-test=true",
			"--name=" + containerName,
			"--rm",
			createLicenseKeyOption(),
			fmt.Sprintf("-p %d:%d", exposedPort, basePort),
			fmt.Sprintf("-v %s:/data", volumeID),
			fmt.Sprintf("-v %s:/certs", certs.Dir),
			"-v /var/run/docker.sock:/var/run/docker.sock",
			"arangodb/arangodb-starter",
			"--docker.container=" + containerName,
		}
		if !master {
			args = append(args, fmt.Sprintf("--starter.join=$IP:%d", basePort))
		}
		return append(args, moreArgs...)
	}

	cID1 := createDockerID("starter-test-cluster-sync1-")
	args1 := append(prepareStarterContainerArgs(true, cID1, volumeIDs[0], basePort, baseStarterArgs...), syncArgs...)
	dockerRun1 := Spawn(t, strings.Join(args1, " "))
	defer dockerRun1.Close()
	defer removeDockerContainer(t, cID1)

	cID2 := createDockerID("starter-test-cluster-sync2-")
	args2 := append(prepareStarterContainerArgs(false, cID2, volumeIDs[1], basePort+(1*portIncrement), baseStarterArgs...), syncArgs...)
	dockerRun2 := Spawn(t, strings.Join(args2, " "))
	defer dockerRun2.Close()
	defer removeDockerContainer(t, cID2)

	cID3 := createDockerID("starter-test-cluster-sync3-")
	args3 := append(prepareStarterContainerArgs(false, cID3, volumeIDs[2], basePort+(2*portIncrement), baseStarterArgs...), syncArgs...)
	dockerRun3 := Spawn(t, strings.Join(args3, " "))
	defer dockerRun3.Close()
	defer removeDockerContainer(t, cID3)

	if ok := WaitUntilStarterReady(t, whatCluster, 3, dockerRun1, dockerRun2, dockerRun3); ok {
		t.Logf("Cluster start took %s", time.Since(start))
		testClusterWithSync(t, insecureStarterEndpoint(0*portIncrement), false)
		testClusterWithSync(t, insecureStarterEndpoint(1*portIncrement), false)
		testClusterWithSync(t, insecureStarterEndpoint(2*portIncrement), false)
	}

	waitForCallFunction(t,
		ShutdownStarterCall(insecureStarterEndpoint(0*portIncrement)),
		ShutdownStarterCall(insecureStarterEndpoint(1*portIncrement)),
		ShutdownStarterCall(insecureStarterEndpoint(2*portIncrement)))
}
