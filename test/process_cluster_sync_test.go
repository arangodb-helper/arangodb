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
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/arangodb/go-driver"

	"github.com/arangodb-helper/arangodb/client"
	"github.com/arangodb-helper/arangodb/service"
)

// TestProcessClusterSync starts a master starter, followed by 2 slave starters,
// all with datacenter to datacenter replication enabled.
func TestProcessClusterSync(t *testing.T) {
	removeArangodProcesses(t)
	testMatch(t, testModeProcess, starterModeCluster, true)
	requireArangoSync(t, testModeProcess)

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
	testMatch(t, testModeProcess, starterModeCluster, true)
	requireArangoSync(t, testModeProcess)

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

		checkSyncInSetupJson(t, procs, peerDirs, false, false)
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

		checkSyncInSetupJson(t, procs, peerDirs, true, true)
		waitForClusterReadinessAndFinish(t, true, true, procs...)
	}
	{
		logVerbose(t, "Starting cluster again with sync disabled")
		procs, cleanup := startCluster(t, ip, starterArgs, peerDirs)
		defer cleanup()

		checkSyncInSetupJson(t, procs, peerDirs, false, true)
		waitForClusterReadinessAndFinish(t, false, true, procs...)
	}
}

// TestProcessLocalClusterRestartWithSyncOnAndOff starts a local cluster without sync then restarts it with sync enabled and disabled again.
func TestProcessLocalClusterRestartWithSyncOnAndOff(t *testing.T) {
	removeArangodProcesses(t)
	testMatch(t, testModeProcess, starterModeCluster, true)
	requireArangoSync(t, testModeProcess)

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
			"--args.syncmasters.debug.profile=true",
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

// TestProcessClusterRestartWithSyncDisabledThenUpgrade scenario:
// - start with sync enabled
// - restart with sync disabled
// - run upgrade
func TestProcessClusterRestartWithSyncDisabledThenUpgrade(t *testing.T) {
	removeArangodProcesses(t)
	testMatch(t, testModeProcess, starterModeCluster, true)
	requireArangoSync(t, testModeProcess)

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
		logVerbose(t, "Starting with sync enabled")
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

		checkSyncInSetupJson(t, procs, peerDirs, false, true)

		waitForClusterHealthy(t, insecureStarterEndpoint(0*portIncrement), time.Second*15)

		testUpgradeProcess(t, insecureStarterEndpoint(0*portIncrement))
		waitForClusterReadinessAndFinish(t, false, true, procs...)
	}
}

func checkSyncInSetupJson(t *testing.T, procs []*SubProcess, peerDirs []string, syncEnabled, isRelaunch bool) {
	waitForClusterReadiness(t, syncEnabled, isRelaunch, procs...)

	t.Logf("Waiting for setup.json to be updated")
	time.Sleep(60 * time.Second)

	for _, dir := range peerDirs {
		config, _, err := service.ReadSetupConfig(zerolog.Logger{}, dir)
		require.NoError(t, err)

		for _, peer := range config.Peers.AllPeers {
			logVerbose(t, "checking dir %s, peer %s:, syncMode: %v", dir, peer.ID, syncEnabled)
			require.Equal(t, syncEnabled, peer.HasSyncMaster(), "dir %s", dir)
			require.Equal(t, syncEnabled, peer.HasSyncWorker(), "dir %s", dir)
		}
	}
}

// startCluster runs starter instance for each entry in peerDirs slice.
func startCluster(t *testing.T, ip string, args []string, peerDirs []string) ([]*SubProcess, func()) {
	var procs []*SubProcess
	for i, p := range peerDirs {
		require.NoError(t, os.Setenv("DATA_DIR", p))
		args := args
		if i > 0 {
			// run slaves
			args = append(args, "--starter.join="+ip)
		}
		procs = append(procs, Spawn(t, strings.Join(args, " ")))
	}
	return procs, func() {
		for _, p := range procs {
			p.Close()
		}
	}
}

func waitForClusterHealthy(t *testing.T, endpoint string, timeout time.Duration) {
	auth := driver.BasicAuthentication("root", "")
	client, err := CreateClient(t, endpoint, client.ServerTypeCoordinator, auth)
	require.NoError(t, err)
	ctx := context.Background()
	clusterClient, err := client.Cluster(ctx)
	require.NoError(t, err)
	logVerbose(t, "Starting wait for cluster healthy")
	start := time.Now()
	for {
		if ctx.Err() != nil {
			return
		}
		h := getHealth(t, ctx, clusterClient)
		allHealthy := true
		for serverID, sh := range h.Health {
			statusGood := sh.Status == driver.ServerStatusGood
			if !statusGood && time.Since(start) > timeout {
				t.Fatalf("Cluster unhealthy after %s: server %s status is %s", timeout.String(), serverID, sh.Status)
				return
			}
			allHealthy = allHealthy && statusGood
		}
		if allHealthy {
			logVerbose(t, "Cluster is healthy!")
			return
		}
		time.Sleep(time.Second)
	}
}
func getHealth(t *testing.T, ctx context.Context, client driver.Cluster) driver.ClusterHealth {
	reqCtx, cancel := context.WithTimeout(ctx, time.Second*2)
	defer cancel()
	h, err := client.Health(reqCtx)
	require.NoError(t, err)
	return h
}
