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
	"os"
	"strings"
	"testing"
	"time"
)

// TestProcessClusterSync starts a master starter, followed by 2 slave starters,
// all with datacenter to datacenter replication enabled.
func TestProcessClusterSync(t *testing.T) {
	removeArangodProcesses(t)
	needTestMode(t, testModeProcess)
	needStarterMode(t, starterModeCluster)
	needEnterprise(t)

	// Create certificates
	ip := "127.0.0.1"
	certs := createSyncCertificates(t, ip, false)

	dataDirMaster := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDirMaster)

	start := time.Now()

	master := Spawn(t, strings.Join([]string{
		"${STARTER}",
		"--starter.address=" + ip,
		"--server.storage-engine=rocksdb",
		"--starter.sync",
		"--sync.server.keyfile=" + certs.TLS.DCA.Keyfile,
		"--sync.server.client-cafile=" + certs.ClientAuth.CACertificate,
		"--sync.master.jwt-secret=" + certs.MasterSecret,
		"--auth.jwt-secret=" + certs.ClusterSecret,
		"--sync.monitoring.token=" + syncMonitoringToken,
		createEnvironmentStarterOptions(),
	}, " "))
	defer master.Close()

	dataDirSlave1 := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDirSlave1)
	slave1 := Spawn(t, strings.Join([]string{
		"${STARTER}",
		"--starter.join=" + ip,
		"--starter.address=" + ip,
		"--server.storage-engine=rocksdb",
		"--starter.sync",
		"--sync.server.keyfile=" + certs.TLS.DCA.Keyfile,
		"--sync.server.client-cafile=" + certs.ClientAuth.CACertificate,
		"--sync.master.jwt-secret=" + certs.MasterSecret,
		"--auth.jwt-secret=" + certs.ClusterSecret,
		"--sync.monitoring.token=" + syncMonitoringToken,
		createEnvironmentStarterOptions(),
	}, " "))
	defer slave1.Close()

	dataDirSlave2 := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDirSlave2)
	slave2 := Spawn(t, strings.Join([]string{
		"${STARTER}",
		"--starter.join=" + ip,
		"--starter.address=" + ip,
		"--server.storage-engine=rocksdb",
		"--starter.sync",
		"--sync.server.keyfile=" + certs.TLS.DCA.Keyfile,
		"--sync.server.client-cafile=" + certs.ClientAuth.CACertificate,
		"--sync.master.jwt-secret=" + certs.MasterSecret,
		"--auth.jwt-secret=" + certs.ClusterSecret,
		"--sync.monitoring.token=" + syncMonitoringToken,
		createEnvironmentStarterOptions(),
	}, " "))
	defer slave2.Close()

	if ok := WaitUntilStarterReady(t, whatCluster, 3, master, slave1, slave2); ok {
		t.Logf("Cluster start took %s", time.Since(start))
		testClusterWithSync(t, insecureStarterEndpoint(0*portIncrement), false)
		testClusterWithSync(t, insecureStarterEndpoint(1*portIncrement), false)
		testClusterWithSync(t, insecureStarterEndpoint(2*portIncrement), false)
	}

	if isVerbose {
		t.Log("Waiting for termination")
	}
	SendIntrAndWait(t, master, slave1, slave2)
}
