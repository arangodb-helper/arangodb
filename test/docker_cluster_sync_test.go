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
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDockerClusterSync runs 3 arangodb starters in docker with arangosync enabled.
func TestDockerClusterSync(t *testing.T) {
	testMatch(t, testModeDocker, starterModeCluster, true)
	ip := os.Getenv("IP")
	require.NotEmpty(t, ip, "IP envvar must be set to IP address of this machine")

	// Cleanup of left over tests
	removeDockerContainersByLabel(t, "starter-test=true")
	removeStarterCreatedDockerContainers(t)

	// Create certificates
	certs := createSyncCertificates(t, ip, true)

	volumeIDs, cleanVolumes := createDockerVolumes(t,
		"vol-starter-test-cluster-sync1-",
		"vol-starter-test-cluster-sync2-",
		"vol-starter-test-cluster-sync3-",
	)
	defer cleanVolumes()

	starterArgs := []string{
		"--starter.address=$IP",
		"--server.storage-engine=rocksdb",
		"--auth.jwt-secret=/certs/" + filepath.Base(certs.ClusterSecret),
		"--starter.sync",
		"--sync.server.keyfile=/certs/" + filepath.Base(certs.TLS.DCA.Keyfile),
		"--sync.server.client-cafile=/certs/" + filepath.Base(certs.ClientAuth.CACertificate),
		"--sync.master.jwt-secret=/certs/" + filepath.Base(certs.MasterSecret),
		"--sync.monitoring.token=" + syncMonitoringToken,
		createEnvironmentStarterOptions(),
	}

	procs, cleanup := startClusterInDocker(t, certs.Dir, starterArgs, volumeIDs)
	defer cleanup()

	waitForClusterReadinessAndFinish(t, true, false, procs...)
}

func TestDockerClusterRestartWithSyncOnAndOff(t *testing.T) {
	testMatch(t, testModeDocker, starterModeCluster, true)
	ip := os.Getenv("IP")
	require.NotEmpty(t, ip, "IP envvar must be set to IP address of this machine")

	// Cleanup of left over tests
	removeDockerContainersByLabel(t, "starter-test=true")
	removeStarterCreatedDockerContainers(t)

	// Create certificates
	certs := createSyncCertificates(t, ip, true)

	volumeIDs, cleanVolumes := createDockerVolumes(t,
		"vol-starter-test-cluster-sync1-",
		"vol-starter-test-cluster-sync2-",
		"vol-starter-test-cluster-sync3-",
	)
	defer cleanVolumes()

	starterArgs := []string{
		"--starter.address=$IP",
		"--auth.jwt-secret=/certs/" + filepath.Base(certs.ClusterSecret),
		createEnvironmentStarterOptions(),
	}
	{
		logVerbose(t, "Starting cluster with sync disabled")
		procs, cleanup := startClusterInDocker(t, certs.Dir, starterArgs, volumeIDs)
		defer cleanup()
		waitForClusterReadinessAndFinish(t, false, false, procs...)
	}
	{
		syncArgs := []string{
			"--starter.sync",
			"--sync.server.keyfile=/certs/" + filepath.Base(certs.TLS.DCA.Keyfile),
			"--sync.server.client-cafile=/certs/" + filepath.Base(certs.ClientAuth.CACertificate),
			"--sync.master.jwt-secret=/certs/" + filepath.Base(certs.MasterSecret),
			"--sync.monitoring.token=" + syncMonitoringToken,
		}
		logVerbose(t, "Starting cluster with sync enabled")
		procs, cleanup := startClusterInDocker(t, certs.Dir, append(starterArgs, syncArgs...), volumeIDs)
		defer cleanup()
		waitForClusterReadinessAndFinish(t, true, true, procs...)
	}
	{
		logVerbose(t, "Starting cluster again with sync disabled")
		procs, cleanup := startClusterInDocker(t, certs.Dir, starterArgs, volumeIDs)
		defer cleanup()
		waitForClusterReadinessAndFinish(t, false, true, procs...)
	}
}

func TestDockerLocalClusterRestartWithSyncOnAndOff(t *testing.T) {
	testMatch(t, testModeDocker, starterModeCluster, true)
	ip := os.Getenv("IP")
	require.NotEmpty(t, ip, "IP envvar must be set to IP address of this machine")

	// Cleanup of left over tests
	removeDockerContainersByLabel(t, "starter-test=true")
	removeStarterCreatedDockerContainers(t)

	// Create certificates
	certs := createSyncCertificates(t, ip, true)

	volumeIDs, cleanVolumes := createDockerVolumes(t, "vol-starter-test-cluster-sync-")
	defer cleanVolumes()

	starterArgs := []string{
		"--starter.local",
		"--starter.address=$IP",
		"--auth.jwt-secret=/certs/" + filepath.Base(certs.ClusterSecret),
		createEnvironmentStarterOptions(),
	}
	{
		logVerbose(t, "Starting cluster with sync disabled")
		procs, cleanup := startClusterInDocker(t, certs.Dir, starterArgs, volumeIDs)
		defer cleanup()
		waitForClusterReadinessAndFinish(t, false, false, procs...)
	}
	{
		syncArgs := []string{
			"--starter.sync",
			"--sync.server.keyfile=/certs/" + filepath.Base(certs.TLS.DCA.Keyfile),
			"--sync.server.client-cafile=/certs/" + filepath.Base(certs.ClientAuth.CACertificate),
			"--sync.master.jwt-secret=/certs/" + filepath.Base(certs.MasterSecret),
			"--sync.monitoring.token=" + syncMonitoringToken,
			"--args.syncmasters.debug.profile=true",
		}
		logVerbose(t, "Starting cluster with sync enabled")
		procs, cleanup := startClusterInDocker(t, certs.Dir, append(starterArgs, syncArgs...), volumeIDs)
		defer cleanup()

		waitForClusterReadinessAndFinish(t, true, true, procs...)
	}
	{
		logVerbose(t, "Starting cluster again with sync disabled")
		procs, cleanup := startClusterInDocker(t, certs.Dir, starterArgs, volumeIDs)
		defer cleanup()
		waitForClusterReadinessAndFinish(t, false, true, procs...)
	}
}
