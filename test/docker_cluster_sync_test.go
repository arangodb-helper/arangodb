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

	volID1 := createDockerID("vol-starter-test-cluster-sync1-")
	createDockerVolume(t, volID1)
	defer removeDockerVolume(t, volID1)

	volID2 := createDockerID("vol-starter-test-cluster-sync2-")
	createDockerVolume(t, volID2)
	defer removeDockerVolume(t, volID2)

	volID3 := createDockerID("vol-starter-test-cluster-sync3-")
	createDockerVolume(t, volID3)
	defer removeDockerVolume(t, volID3)

	// Cleanup of left over tests
	removeDockerContainersByLabel(t, "starter-test=true")
	removeStarterCreatedDockerContainers(t)

	start := time.Now()

	cID1 := createDockerID("starter-test-cluster-sync1-")
	dockerRun1 := Spawn(t, strings.Join([]string{
		"docker run -i",
		"--label starter-test=true",
		"--name=" + cID1,
		"--rm",
		createLicenseKeyOption(),
		fmt.Sprintf("-p %d:%d", basePort, basePort),
		fmt.Sprintf("-v %s:/data", volID1),
		fmt.Sprintf("-v %s:/certs", certs.Dir),
		"-v /var/run/docker.sock:/var/run/docker.sock",
		"arangodb/arangodb-starter",
		"--docker.container=" + cID1,
		"--starter.address=$IP",
		"--server.storage-engine=rocksdb",
		"--starter.sync",
		"--sync.server.keyfile=/certs/" + filepath.Base(certs.TLS.DCA.Keyfile),
		"--sync.server.client-cafile=/certs/" + filepath.Base(certs.ClientAuth.CACertificate),
		"--sync.master.jwt-secret=/certs/" + filepath.Base(certs.MasterSecret),
		"--auth.jwt-secret=/certs/" + filepath.Base(certs.ClusterSecret),
		"--sync.monitoring.token=" + syncMonitoringToken,
		createEnvironmentStarterOptions(),
	}, " "))
	defer dockerRun1.Close()
	defer removeDockerContainer(t, cID1)

	cID2 := createDockerID("starter-test-cluster-sync2-")
	dockerRun2 := Spawn(t, strings.Join([]string{
		"docker run -i",
		"--label starter-test=true",
		"--name=" + cID2,
		"--rm",
		createLicenseKeyOption(),
		fmt.Sprintf("-p %d:%d", basePort+(1*portIncrement), basePort),
		fmt.Sprintf("-v %s:/data", volID2),
		fmt.Sprintf("-v %s:/certs", certs.Dir),
		"-v /var/run/docker.sock:/var/run/docker.sock",
		"arangodb/arangodb-starter",
		"--docker.container=" + cID2,
		"--starter.address=$IP",
		"--server.storage-engine=rocksdb",
		"--starter.sync",
		"--sync.server.keyfile=/certs/" + filepath.Base(certs.TLS.DCA.Keyfile),
		"--sync.server.client-cafile=/certs/" + filepath.Base(certs.ClientAuth.CACertificate),
		"--sync.master.jwt-secret=/certs/" + filepath.Base(certs.MasterSecret),
		"--auth.jwt-secret=/certs/" + filepath.Base(certs.ClusterSecret),
		"--sync.monitoring.token=" + syncMonitoringToken,
		createEnvironmentStarterOptions(),
		fmt.Sprintf("--starter.join=$IP:%d", basePort),
	}, " "))
	defer dockerRun2.Close()
	defer removeDockerContainer(t, cID2)

	cID3 := createDockerID("starter-test-cluster-sync3-")
	dockerRun3 := Spawn(t, strings.Join([]string{
		"docker run -i",
		"--label starter-test=true",
		"--name=" + cID3,
		"--rm",
		createLicenseKeyOption(),
		fmt.Sprintf("-p %d:%d", basePort+(2*portIncrement), basePort),
		fmt.Sprintf("-v %s:/data", volID3),
		fmt.Sprintf("-v %s:/certs", certs.Dir),
		"-v /var/run/docker.sock:/var/run/docker.sock",
		"arangodb/arangodb-starter",
		"--docker.container=" + cID3,
		"--starter.address=$IP",
		"--server.storage-engine=rocksdb",
		"--starter.sync",
		"--sync.server.keyfile=/certs/" + filepath.Base(certs.TLS.DCA.Keyfile),
		"--sync.server.client-cafile=/certs/" + filepath.Base(certs.ClientAuth.CACertificate),
		"--sync.master.jwt-secret=/certs/" + filepath.Base(certs.MasterSecret),
		"--auth.jwt-secret=/certs/" + filepath.Base(certs.ClusterSecret),
		"--sync.monitoring.token=" + syncMonitoringToken,
		createEnvironmentStarterOptions(),
		fmt.Sprintf("--starter.join=$IP:%d", basePort),
	}, " "))
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
