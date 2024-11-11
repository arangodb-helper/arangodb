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
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/arangodb-helper/arangodb/service"
)

// TestDockerClusterSync runs 3 arangodb starters in docker with arangosync enabled.
func TestDockerClusterSync(t *testing.T) {
	testMatch(t, testModeDocker, starterModeCluster, true)
	requireArangoSync(t, testModeDocker)

	// Cleanup of left over tests
	removeDockerContainersByLabel(t, "starter-test=true")
	removeStarterCreatedDockerContainers(t)

	// Create certificates
	certs := createSyncCertificates(t, "localhost", true)

	t.Logf("certs dir: %v", certs.Dir)

	volumeIDs, cleanVolumes := createDockerVolumes(t,
		"vol-starter-test-cluster-sync1-",
		"vol-starter-test-cluster-sync2-",
		"vol-starter-test-cluster-sync3-",
	)
	defer cleanVolumes()

	starterArgs := []string{
		"--server.storage-engine=rocksdb",
		"--auth.jwt-secret=/certs/" + filepath.Base(certs.ClusterSecret),
		"--starter.sync",
		"--sync.server.keyfile=/certs/" + filepath.Base(certs.TLS.DCA.Keyfile),
		"--sync.server.client-cafile=/certs/" + filepath.Base(certs.ClientAuth.CACertificate),
		"--sync.master.jwt-secret=/certs/" + filepath.Base(certs.MasterSecret),
		"--sync.monitoring.token=" + syncMonitoringToken,
	}

	processes, cleanup := startClusterInDocker(t, certs.Dir, starterArgs, volumeIDs)
	defer cleanup()

	waitForClusterReadinessAndFinish(t, true, false, processes...)
}

func TestDockerClusterRestartWithSyncOnAndOff(t *testing.T) {
	testMatch(t, testModeDocker, starterModeCluster, true)
	requireArangoSync(t, testModeDocker)

	// Cleanup of left over tests
	removeDockerContainersByLabel(t, "starter-test=true")
	removeStarterCreatedDockerContainers(t)

	// Create certificates
	certs := createSyncCertificates(t, "localhost", true)

	volumeIDs, cleanVolumes := createDockerVolumes(t,
		"vol-starter-test-cluster-sync1-",
		"vol-starter-test-cluster-sync2-",
		"vol-starter-test-cluster-sync3-",
	)
	defer cleanVolumes()

	starterArgs := []string{
		"--auth.jwt-secret=/certs/" + filepath.Base(certs.ClusterSecret),
	}
	{
		logVerbose(t, "Starting cluster with sync disabled")
		processes, cleanup := startClusterInDocker(t, certs.Dir, starterArgs, volumeIDs)
		defer cleanup()

		verifyDockerSyncSetupJson(t, volumeIDs, false, false, processes...)
		waitForClusterReadinessAndFinish(t, false, false, processes...)
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
		processes, cleanup := startClusterInDocker(t, certs.Dir, append(starterArgs, syncArgs...), volumeIDs)
		defer cleanup()

		verifyDockerSyncSetupJson(t, volumeIDs, true, true, processes...)
		waitForClusterReadinessAndFinish(t, true, true, processes...)
	}
	{
		logVerbose(t, "Starting cluster again with sync disabled")
		processes, cleanup := startClusterInDocker(t, certs.Dir, starterArgs, volumeIDs)
		defer cleanup()

		verifyDockerSyncSetupJson(t, volumeIDs, false, true, processes...)
		waitForClusterReadinessAndFinish(t, false, true, processes...)
	}
}

func TestDockerLocalClusterRestartWithSyncOnAndOff(t *testing.T) {
	testMatch(t, testModeDocker, starterModeCluster, true)
	requireArangoSync(t, testModeDocker)

	// Cleanup of left over tests
	removeDockerContainersByLabel(t, "starter-test=true")
	removeStarterCreatedDockerContainers(t)

	// Create certificates
	certs := createSyncCertificates(t, "localhost", true)

	volumeIDs, cleanVolumes := createDockerVolumes(t, "vol-starter-test-cluster-sync-")
	defer cleanVolumes()

	starterArgs := []string{
		"--starter.local",
		"--auth.jwt-secret=/certs/" + filepath.Base(certs.ClusterSecret),
	}
	{
		logVerbose(t, "Starting cluster with sync disabled")
		processes, cleanup := startClusterInDocker(t, certs.Dir, starterArgs, volumeIDs)
		defer cleanup()
		waitForClusterReadinessAndFinish(t, false, false, processes...)
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
		processes, cleanup := startClusterInDocker(t, certs.Dir, append(starterArgs, syncArgs...), volumeIDs)
		defer cleanup()

		waitForClusterReadinessAndFinish(t, true, true, processes...)
	}
	{
		logVerbose(t, "Starting cluster again with sync disabled")
		processes, cleanup := startClusterInDocker(t, certs.Dir, starterArgs, volumeIDs)
		defer cleanup()
		waitForClusterReadinessAndFinish(t, false, true, processes...)
	}
}

func verifyDockerSyncSetupJson(t *testing.T, volumes []string, syncEnabled, isRelaunch bool, processes ...*SubProcess) {
	waitForClusterReadiness(t, syncEnabled, isRelaunch, processes...)

	t.Logf("Waiting for setup.json to be updated")
	time.Sleep(60 * time.Second)

	for _, vol := range volumes {
		c := Spawn(t, fmt.Sprintf("docker exec %s cat /data/setup.json", vol))
		require.NoError(t, c.Wait())
		require.NoError(t, c.Close())

		cfg, _, err := service.VerifySetupConfig(zerolog.New(zerolog.NewConsoleWriter()), c.Output())
		require.NoError(t, err, "Failed to read setup.json, member: %s", vol)

		t.Logf("Verify setup.json for member: %s", vol)
		for _, p := range cfg.Peers.AllPeers {
			logVerbose(t, "checking dir %s, peer %s:, syncMode: %v", p.DataDir, p.ID, syncEnabled)
			require.Equal(t, syncEnabled, p.HasSyncMaster(), "dir %s", p.DataDir)
			require.Equal(t, syncEnabled, p.HasSyncWorker(), "dir %s", p.DataDir)
		}
	}
}

func startClusterInDocker(t *testing.T, certsDir string, args []string, volumeIDs []string) ([]*SubProcess, func()) {
	var processes []*SubProcess
	var containers []string

	joins := fmt.Sprintf("localhost:%d,localhost:%d,localhost:%d", basePort, basePort+(1*portIncrement), basePort+(2*portIncrement))

	for i, volumeID := range volumeIDs {
		proc := spawnMemberInDocker(t, basePort+(i*portIncrement), volumeID, joins, strings.Join(args, " "), fmt.Sprintf("-v %s:/certs", certsDir))
		processes = append(processes, proc)
		containers = append(containers, volumeID)
	}
	return processes, func() {
		for _, p := range processes {
			p.Close()
		}
		for _, c := range containers {
			removeDockerContainer(t, c)
		}
	}
}

func waitForClusterReadiness(t *testing.T, syncEnabled, isRelaunch bool, procs ...*SubProcess) {
	WaitUntilStarterReady(t, whatCluster, len(procs), procs...)
	var timeout time.Duration
	if syncEnabled {
		timeout = time.Minute
	}
	if isRelaunch {
		timeout = time.Second * 5
	}
	for i := range procs {
		testClusterPeer(t, insecureStarterEndpoint(i*portIncrement), false, syncEnabled, timeout)
	}
}

func waitForClusterReadinessAndFinish(t *testing.T, syncEnabled, isRelaunch bool, procs ...*SubProcess) {
	waitForClusterReadiness(t, syncEnabled, isRelaunch, procs...)
	logVerbose(t, "Waiting for termination")
	SendIntrAndWait(t, procs...)
}
