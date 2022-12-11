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
