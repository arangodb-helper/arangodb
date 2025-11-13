// DISCLAIMER
//
// # Copyright 2024 ArangoDB GmbH, Cologne, Germany
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
package test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/arangodb-helper/arangodb/client"
	"github.com/stretchr/testify/require"
)

// TestDockerClusterSSLCertRotationHotReload tests certificate hot reload without restart in Docker containers
// This test validates Scenario 2: Changing certificate content only via /_admin/server/tls endpoint
// This test runs 3 arangodb starters in Docker containers with SSL enabled
//
// NOTE: This test uses --net=host which has known limitations on WSL2:
// - Docker containers can communicate internally (proven via manual testing)
// - But WSL2 host cannot connect to --net=host containers due to networking architecture
// - Works fine on native Linux (CircleCI)
//
// If this test fails with "connection refused" on WSL2, use process-mode tests instead:
// - TestProcessClusterSSLCertRotationHotReload
func TestDockerClusterSSLCertRotationHotReload(t *testing.T) {
	// Detect WSL2 and skip if detected (networking issues with --net=host)
	if data, err := os.ReadFile("/proc/version"); err == nil {
		version := strings.ToLower(string(data))
		if strings.Contains(version, "microsoft") || strings.Contains(version, "wsl") {
			t.Skip("Skipping Docker mode test on WSL2 - Docker --net=host networking doesn't work properly in WSL2. " +
				"The test works on native Linux (CircleCI). Use TestProcessClusterSSLCertRotationHotReload instead.")
		}
	}

	testMatch(t, testModeDocker, starterModeCluster, false)

	// Create temporary directory for certificates on host
	certDir, err := os.MkdirTemp("", "ssl-cert-test")
	require.NoError(t, err, "Failed to create temp directory")
	defer os.RemoveAll(certDir)

	// Create first certificate
	cert1Path := filepath.Join(certDir, "server.keyfile")
	err = createTestCertificate(cert1Path, "initial-cert")
	require.NoError(t, err, "Failed to create initial certificate")

	// Create Docker volumes for data persistence
	cID1 := createDockerID("starter-test-ssl-hotreload1-")
	createDockerVolume(t, cID1)
	defer removeDockerVolume(t, cID1)

	cID2 := createDockerID("starter-test-ssl-hotreload2-")
	createDockerVolume(t, cID2)
	defer removeDockerVolume(t, cID2)

	cID3 := createDockerID("starter-test-ssl-hotreload3-")
	createDockerVolume(t, cID3)
	defer removeDockerVolume(t, cID3)

	// Cleanup leftover containers
	removeDockerContainersByLabel(t, "starter-test=true")
	removeStarterCreatedDockerContainers(t)

	// Mount certificate directory into containers
	certMount := fmt.Sprintf("-v %s:/certs", certDir)
	certArg := "--ssl.keyfile=/certs/server.keyfile"

	joins := fmt.Sprintf("localhost:%d,localhost:%d,localhost:%d",
		basePort, basePort+(1*portIncrement), basePort+(2*portIncrement))

	start := time.Now()

	// Start 3 Docker containers with SSL
	dockerRun1 := spawnMemberInDocker(t, basePort, cID1, joins, certArg, certMount)
	defer dockerRun1.Close()
	defer removeDockerContainer(t, cID1)

	dockerRun2 := spawnMemberInDocker(t, basePort+(1*portIncrement), cID2, joins, certArg, certMount)
	defer dockerRun2.Close()
	defer removeDockerContainer(t, cID2)

	dockerRun3 := spawnMemberInDocker(t, basePort+(2*portIncrement), cID3, joins, certArg, certMount)
	defer dockerRun3.Close()
	defer removeDockerContainer(t, cID3)

	// Wait for cluster to be ready
	if ok := WaitUntilStarterReady(t, whatCluster, 3, dockerRun1, dockerRun2, dockerRun3); ok {
		t.Logf("Cluster start with SSL took %s", time.Since(start))

		// In Docker mode with SSL, give extra time for SSL endpoints to fully initialize
		t.Log("Waiting 30 seconds for SSL endpoints to be ready...")
		time.Sleep(30 * time.Second)

		testCluster(t, secureStarterEndpoint(0*portIncrement), true)
		testCluster(t, secureStarterEndpoint(1*portIncrement), true)
		testCluster(t, secureStarterEndpoint(2*portIncrement), true)
	}

	// Get the certificate serial number from all server types before rotation
	t.Log("Checking initial certificate on all server types")
	initialCertSerials := getCertificateSerials(t, secureStarterEndpoint(0*portIncrement))
	require.NotEmpty(t, initialCertSerials, "Should have initial certificate serials")

	// Create and write new certificate to the SAME path on host (mounted into containers)
	t.Log("Creating new certificate at the same path (mounted into containers)")
	err = createTestCertificate(cert1Path, "rotated-cert")
	require.NoError(t, err, "Failed to create rotated certificate")

	// Force filesystem sync to ensure certificate file is written
	t.Log("Forcing filesystem sync")
	syncCmd := exec.Command("sync")
	if err := syncCmd.Run(); err != nil {
		t.Logf("Warning: sync command failed: %v", err)
	}
	// Give filesystem time to propagate changes through Docker volume mounts and nested containers
	// Docker has multiple layers: host -> starter container -> nested ArangoDB containers
	t.Log("Waiting 30 seconds for filesystem to propagate changes through Docker layers...")
	time.Sleep(30 * time.Second)

	// Trigger hot reload on all Docker nodes with retries
	t.Log("Triggering hot reload on all Docker nodes (with retries)...")
	for i := 0; i < 3; i++ {
		endpoint := secureStarterEndpoint(i * portIncrement)
		t.Logf("Attempting hot reload on node-%d (%s)", i+1, endpoint)
		reloadCertificatesWithRetry(t, endpoint, 3, 5*time.Second)
	}

	// Give servers time to reload - Docker OverlayFS caching means this takes longer than process mode
	// Coordinators and DBServers need more time than agents to reload in nested containers
	t.Log("Waiting 120 seconds for certificates to be reloaded (Docker OverlayFS propagation delay)...")
	time.Sleep(120 * time.Second)

	// First check: Verify certificates were reloaded
	t.Log("First verification: Checking if certificates were reloaded")
	newCertSerials := getCertificateSerials(t, secureStarterEndpoint(0*portIncrement))
	require.NotEmpty(t, newCertSerials, "Should have new certificate serials")

	// Check which servers have rotated
	rotatedServers := make(map[client.ServerType]bool)
	for serverType, newSerial := range newCertSerials {
		initialSerial := initialCertSerials[serverType]
		if newSerial != initialSerial {
			t.Logf("Certificate rotated successfully on %s: %s -> %s", serverType, initialSerial, newSerial)
			rotatedServers[serverType] = true
		} else {
			t.Logf("Certificate NOT YET rotated on %s: serial remained %s", serverType, initialSerial)
		}
	}

	// If not all servers have rotated, wait longer and check again
	if !rotatedServers[client.ServerTypeCoordinator] || !rotatedServers[client.ServerTypeDBServer] {
		t.Log("Some servers haven't rotated yet. Waiting additional 60 seconds for slower Docker OverlayFS propagation...")
		time.Sleep(60 * time.Second)

		// Second check: Re-verify certificates after additional wait
		t.Log("Second verification: Re-checking certificates after extended wait")
		newCertSerials = getCertificateSerials(t, secureStarterEndpoint(0*portIncrement))

		// Update rotatedServers with new check
		for serverType, newSerial := range newCertSerials {
			initialSerial := initialCertSerials[serverType]
			if newSerial != initialSerial {
				if !rotatedServers[serverType] {
					t.Logf("Certificate NOW rotated on %s (after extended wait): %s -> %s", serverType, initialSerial, newSerial)
				}
				rotatedServers[serverType] = true
			} else {
				t.Errorf("Certificate STILL NOT rotated on %s after 180s total wait: serial remained %s", serverType, initialSerial)
			}
		}
	}

	// Verify all server types rotated successfully
	require.True(t, rotatedServers[client.ServerTypeCoordinator], "Coordinator should have successfully rotated certificate")
	require.True(t, rotatedServers[client.ServerTypeDBServer], "DBServer should have successfully rotated certificate")
	require.True(t, rotatedServers[client.ServerTypeAgent], "Agent should have successfully rotated certificate")

	// Verify cluster still works after rotation
	t.Log("Verifying cluster functionality after certificate rotation")
	testCluster(t, secureStarterEndpoint(0*portIncrement), true)

	// Graceful shutdown
	waitForCallFunction(t,
		ShutdownStarterCall(secureStarterEndpoint(0*portIncrement)),
		ShutdownStarterCall(secureStarterEndpoint(1*portIncrement)),
		ShutdownStarterCall(secureStarterEndpoint(2*portIncrement)))
}

// Helper with retry logic for reloading certificates
func reloadCertificatesWithRetry(t *testing.T, endpoint string, retries int, delay time.Duration) {
	for i := 0; i < retries; i++ {
		err := reloadCertificatesViaAPI(t, endpoint)
		if err == nil {
			t.Logf("Reload successful on %s (attempt %d/%d)", endpoint, i+1, retries)
			return
		}
		t.Logf("Reload failed on %s (attempt %d/%d): %v", endpoint, i+1, retries, err)
		time.Sleep(delay)
	}
	t.Fatalf("Failed to reload certificate at %s after %d retries", endpoint, retries)
}
