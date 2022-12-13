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

	peerDirs := []string{SetUniqueDataDir(t), SetUniqueDataDir(t), SetUniqueDataDir(t)}
	defer func() {
		for _, d := range peerDirs {
			os.RemoveAll(d)
		}
	}()

	starterArgs := []string{
		"${STARTER}",
		"--starter.address=" + ip,
		"--auth.jwt-secret=" + certs.ClusterSecret,
		"--starter.sync",
		"--sync.server.keyfile=" + certs.TLS.DCA.Keyfile,
		"--sync.server.client-cafile=" + certs.ClientAuth.CACertificate,
		"--sync.master.jwt-secret=" + certs.MasterSecret,
		"--sync.monitoring.token=" + syncMonitoringToken,
		createEnvironmentStarterOptions(),
	}
	procs, cleanup := startCluster(t, ip, starterArgs, peerDirs)
	defer cleanup()

	waitForClusterReadinessAndFinish(t, true, false, procs...)
}

// TestProcessClusterRestartWithSyncOnAndOff starts a cluster without sync then restarts it with sync enabled and disabled again.
func TestProcessClusterRestartWithSyncOnAndOff(t *testing.T) {
	removeArangodProcesses(t)
	needTestMode(t, testModeProcess)
	needStarterMode(t, starterModeCluster)
	needEnterprise(t)

	// Create certificates
	ip := "127.0.0.1"
	certs := createSyncCertificates(t, ip, false)

	peerDirs := []string{SetUniqueDataDir(t), SetUniqueDataDir(t), SetUniqueDataDir(t)}
	defer func() {
		for _, d := range peerDirs {
			os.RemoveAll(d)
		}
	}()

	starterArgs := []string{
		"${STARTER}",
		"--starter.address=" + ip,
		"--auth.jwt-secret=" + certs.ClusterSecret,
		createEnvironmentStarterOptions(),
	}
	{
		logVerbose(t, "Starting cluster with sync disabled")
		procs, cleanup := startCluster(t, ip, starterArgs, peerDirs)
		defer cleanup()

		waitForClusterReadinessAndFinish(t, false, false, procs...)
	}
	{
		logVerbose(t, "Starting again with sync enabled")
		syncArgs := []string{
			"--starter.sync",
			"--sync.server.keyfile=" + certs.TLS.DCA.Keyfile,
			"--sync.server.client-cafile=" + certs.ClientAuth.CACertificate,
			"--sync.master.jwt-secret=" + certs.MasterSecret,
			"--sync.monitoring.token=" + syncMonitoringToken,
		}
		starterArgsWithSync := append(starterArgs, syncArgs...)
		procs, cleanup := startCluster(t, ip, starterArgsWithSync, peerDirs)
		defer cleanup()

		waitForClusterReadinessAndFinish(t, true, true, procs...)
	}
	{
		logVerbose(t, "Starting cluster again with sync disabled")
		procs, cleanup := startCluster(t, ip, starterArgs, peerDirs)
		defer cleanup()

		waitForClusterReadinessAndFinish(t, false, true, procs...)
	}
}

// TestProcessLocalClusterRestartWithSyncOnAndOff starts a local cluster without sync then restarts it with sync enabled and disabled again.
func TestProcessLocalClusterRestartWithSyncOnAndOff(t *testing.T) {
	removeArangodProcesses(t)
	needTestMode(t, testModeProcess)
	needStarterMode(t, starterModeCluster)
	needEnterprise(t)

	// Create certificates
	ip := "127.0.0.1"
	certs := createSyncCertificates(t, ip, false)

	dataDirMaster := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDirMaster)

	starterArgs := []string{
		"${STARTER}",
		"--starter.local",
		"--starter.address=" + ip,
		"--auth.jwt-secret=" + certs.ClusterSecret,
		createEnvironmentStarterOptions(),
	}
	{
		logVerbose(t, "Starting cluster with sync disabled")
		master := Spawn(t, strings.Join(starterArgs, " "))
		defer master.Close()

		waitForClusterReadinessAndFinish(t, false, false, master)
	}
	{
		logVerbose(t, "Starting again with sync enabled")
		syncArgs := []string{
			"--starter.sync",
			"--sync.server.keyfile=" + certs.TLS.DCA.Keyfile,
			"--sync.server.client-cafile=" + certs.ClientAuth.CACertificate,
			"--sync.master.jwt-secret=" + certs.MasterSecret,
			"--sync.monitoring.token=" + syncMonitoringToken,
		}
		starterArgsWithSync := append(starterArgs, syncArgs...)
		master := Spawn(t, strings.Join(starterArgsWithSync, " "))
		defer master.Close()

		waitForClusterReadinessAndFinish(t, true, true, master)
	}
	{
		logVerbose(t, "Starting cluster again with sync disabled")
		master := Spawn(t, strings.Join(starterArgs, " "))
		defer master.Close()

		waitForClusterReadinessAndFinish(t, false, true, master)
	}
}
