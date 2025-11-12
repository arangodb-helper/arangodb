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
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/arangodb-helper/arangodb/client"
	"github.com/arangodb-helper/arangodb/service"
	"github.com/stretchr/testify/require"
)

// createTestCertificate creates an SSL certificate in the given path.
func createTestCertificate(path, org string) error {
	opts := service.CreateCertificateOptions{
		Hosts:        []string{"localhost", "127.0.0.1"},
		ValidFor:     time.Hour * 24 * 365,
		Organization: org,
	}

	certPath, err := service.CreateCertificate(opts, filepath.Dir(path))
	if err != nil {
		return err
	}

	data, err := os.ReadFile(certPath)
	if err != nil {
		return err
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return err
	}

	if certPath != path {
		_ = os.Remove(certPath)
	}

	return nil
}

// TestProcessClusterReplaceCert verifies SSL certificate replacement and reload after restart.
//
// IMPORTANT: This test intentionally reuses the same data directories across restarts to verify
// that certificate changes work with persistent data.
//
// KNOWN LIMITATION: RocksDB file locks in reused data directories are not released reliably
// in certain environments even after graceful process termination. The test works fine in:
//   - Native Linux (not WSL2, not containers)
//   - But NOT:
//   - Docker/containers (on any host)
//   - WSL2 (even native, without Docker)
//
// For certificate rotation testing in containers or WSL2, use TestProcessClusterSSLCertRotationHotReload
// instead, which performs hot reload without requiring cluster restart and works on all platforms.
func TestProcessClusterReplaceCert(t *testing.T) {
	removeArangodProcesses(t)
	testMatch(t, testModeProcess, starterModeCluster, false)

	// Check if running in a container - RocksDB file locking with reused data dirs is unreliable in containers
	inContainer := false
	if _, err := os.Stat("/.dockerenv"); err == nil {
		inContainer = true
	} else if data, err := os.ReadFile("/proc/1/cgroup"); err == nil {
		inContainer = strings.Contains(string(data), "docker") ||
			strings.Contains(string(data), "lxc") ||
			strings.Contains(string(data), "containerd")
	}

	// Detect WSL2 environment and log system information
	isWSL := false
	if data, err := os.ReadFile("/proc/version"); err == nil {
		version := strings.ToLower(string(data))
		isWSL = strings.Contains(version, "microsoft") || strings.Contains(version, "wsl")
		t.Logf("System: %s", strings.TrimSpace(version))
		t.Logf("Container: %v, WSL: %v", inContainer, isWSL)
	} else {
		t.Logf("Container: %v", inContainer)
	}

	// Skip if running in containers OR on WSL2
	if inContainer || isWSL {
		t.Skip("Skipping TestProcessClusterReplaceCert in containerized or WSL2 environment - RocksDB file locks in reused data directories are not released reliably in these environments. This test works fine on native Linux only. For certificate rotation testing in containers/WSL2, use TestProcessClusterSSLCertRotationHotReload instead, which performs hot reload without requiring cluster restart.")
	}

	// 1. Create temp dir for certificates
	certDir, err := os.MkdirTemp("", "ssl-cert-path-test")
	require.NoError(t, err, "Failed to create temp directory")
	defer os.RemoveAll(certDir)

	// 2. Create first certificate
	cert1Path := filepath.Join(certDir, "server1.keyfile")
	require.NoError(t, createTestCertificate(cert1Path, "cert-path-1"))

	dataDirMaster := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDirMaster)

	start := time.Now()

	// 3. Start master node
	masterCmd := fmt.Sprintf("${STARTER} --ssl.keyfile=%s %s",
		cert1Path, createEnvironmentStarterOptions())
	master := Spawn(t, masterCmd)
	defer master.Close()

	// 4. Start slave 1
	dataDirSlave1 := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDirSlave1)
	slave1Cmd := fmt.Sprintf("${STARTER} --starter.join 127.0.0.1 --ssl.keyfile=%s %s",
		cert1Path, createEnvironmentStarterOptions())
	slave1 := Spawn(t, slave1Cmd)
	defer slave1.Close()

	// 5. Start slave 2
	dataDirSlave2 := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDirSlave2)
	slave2Cmd := fmt.Sprintf("${STARTER} --starter.join 127.0.0.1 --ssl.keyfile=%s %s",
		cert1Path, createEnvironmentStarterOptions())
	slave2 := Spawn(t, slave2Cmd)
	defer slave2.Close()

	// 6. Wait for cluster readiness
	if ok := WaitUntilStarterReady(t, whatCluster, 3, master, slave1, slave2); ok {
		t.Logf(" Cluster started with SSL in %s", time.Since(start))
		testCluster(t, secureStarterEndpoint(0*portIncrement), true)
		testCluster(t, secureStarterEndpoint(1*portIncrement), true)
		testCluster(t, secureStarterEndpoint(2*portIncrement), true)
	}

	// 7. Record initial cert serials
	t.Log("Checking initial certificates")
	initialCertSerials := getCertificateSerials(t, secureStarterEndpoint(0*portIncrement))

	// 8. Gracefully shutdown all starters
	t.Log("Shutting down cluster for certificate replacement")
	waitForCallFunction(t,
		ShutdownStarterCall(secureStarterEndpoint(0*portIncrement)),
		ShutdownStarterCall(secureStarterEndpoint(1*portIncrement)),
		ShutdownStarterCall(secureStarterEndpoint(2*portIncrement)),
	)

	// Cleanup and prepare for restart with data persistence
	// Re-detect WSL for cleanup logic (we already checked for skip condition above)
	isWSL = false
	if data, err := os.ReadFile("/proc/version"); err == nil {
		version := strings.ToLower(string(data))
		isWSL = strings.Contains(version, "microsoft") || strings.Contains(version, "wsl")
		t.Logf("/proc/version: %s", strings.TrimSpace(version))
		t.Logf("WSL detected: %v", isWSL)
	}

	if isWSL {
		t.Log("Detected WSL environment - using optimized cleanup (~30 seconds)")
		t.Log("Waiting 12s for initial graceful shutdown...")
		time.Sleep(12 * time.Second)

		// Aggressive process cleanup for WSL (multiple rounds to be thorough)
		t.Log("Killing all arangod/arangodb processes (2 rounds)...")
		for i := 0; i < 2; i++ {
			exec.Command("pkill", "-9", "arangod").Run()
			exec.Command("pkill", "-9", "arangodb").Run()
			time.Sleep(1 * time.Second)
		}

		// Force filesystem sync
		exec.Command("sync").Run()
		time.Sleep(2 * time.Second)

		t.Log("Removing ALL RocksDB LOCK files...")
		for _, dataDir := range []string{dataDirMaster, dataDirSlave1, dataDirSlave2} {
			exec.Command("sh", "-c", fmt.Sprintf("find %s -name LOCK -type f -delete 2>/dev/null || true", dataDir)).Run()
			exec.Command("sh", "-c", fmt.Sprintf("find %s -name '*.lock' -type f -delete 2>/dev/null || true", dataDir)).Run()
		}

		// Another sync and wait for filesystem (WSL2 needs extra time)
		exec.Command("sync").Run()
		t.Log("Waiting 45s for WSL filesystem to release locks...")
		time.Sleep(45 * time.Second)
	} else {
		t.Log("Detected native Linux - using quick cleanup (~15 seconds)")
		t.Log("Waiting 10s for graceful shutdown...")
		time.Sleep(10 * time.Second)

		// Quick cleanup for native Linux (locks release properly)
		t.Log("Killing any remaining processes...")
		exec.Command("pkill", "-9", "arangod").Run()
		exec.Command("pkill", "-9", "arangodb").Run()
		time.Sleep(2 * time.Second)

		t.Log("Removing RocksDB LOCK files (preserving data)...")
		for _, dataDir := range []string{dataDirMaster, dataDirSlave1, dataDirSlave2} {
			exec.Command("sh", "-c", fmt.Sprintf("find %s -name LOCK -type f -delete 2>/dev/null || true", dataDir)).Run()
		}

		exec.Command("sync").Run()
		t.Log("Waiting 3s for filesystem sync...")
		time.Sleep(3 * time.Second)
	}

	// 9. Replace certificate
	t.Log("Replacing certificate file with new one")
	require.NoError(t, createTestCertificate(cert1Path, "ReplacedCert"))
	t.Log(" Certificate replaced on disk")

	// 10. Restart all nodes with SAME data directories (data persists)
	t.Log("Reusing existing data directories to PRESERVE DATA across restart")
	t.Logf("Master: %s", dataDirMaster)
	t.Logf("Slave1: %s", dataDirSlave1)
	t.Logf("Slave2: %s", dataDirSlave2)
	t.Log("Restarting cluster with SAME data dirs (preserving data) and new certificate")
	master = Spawn(t, masterCmd)
	defer master.Close()
	slave1 = Spawn(t, slave1Cmd)
	defer slave1.Close()
	slave2 = Spawn(t, slave2Cmd)
	defer slave2.Close()

	// Give starters time to stabilize after restart (especially important on WSL2)
	t.Log("Waiting 20s for starters to stabilize after restart...")
	time.Sleep(20 * time.Second)

	if ok := WaitUntilStarterReady(t, whatCluster, 3, master, slave1, slave2); ok {
		t.Logf(" Cluster restarted with new certs in %s", time.Since(start))
		testCluster(t, secureStarterEndpoint(0*portIncrement), true)
		testCluster(t, secureStarterEndpoint(1*portIncrement), true)
		testCluster(t, secureStarterEndpoint(2*portIncrement), true)

		// 11. Verify cert rotation
		t.Log("Verifying that certificates changed")
		newCertSerials := getCertificateSerials(t, secureStarterEndpoint(0*portIncrement))

		for serverType, newSerial := range newCertSerials {
			oldSerial := initialCertSerials[serverType]
			if oldSerial == "" {
				t.Errorf("No initial certificate found for %s", serverType)
				continue
			}
			if newSerial == "" {
				t.Errorf("No new certificate found for %s", serverType)
				continue
			}
			if oldSerial == newSerial {
				t.Errorf(" Certificate NOT replaced on %s: serial remained %s", serverType, oldSerial)
			} else {
				t.Logf(" Certificate replaced on %s: %s → %s", serverType, oldSerial[:8], newSerial[:8])
			}
		}

		require.NotEqual(t, initialCertSerials[client.ServerTypeCoordinator],
			newCertSerials[client.ServerTypeCoordinator], "Coordinator certificate should change")
		require.NotEqual(t, initialCertSerials[client.ServerTypeDBServer],
			newCertSerials[client.ServerTypeDBServer], "DBServer certificate should change")
		require.NotEqual(t, initialCertSerials[client.ServerTypeAgent],
			newCertSerials[client.ServerTypeAgent], "Agent certificate should change")

		t.Log(" All certificates successfully rotated after restart")
	}

	// Final cleanup
	if isVerbose {
		t.Log("Waiting for termination")
	}
	SendIntrAndWait(t, master, slave1, slave2)
}

// Helper function to reload certificates via /_admin/server/tls API on all server types
func reloadCertificatesViaAPI(t *testing.T, starterEndpoint string) error {
	c := NewStarterClient(t, starterEndpoint)
	ctx := context.Background()

	processes, err := c.Processes(ctx)
	if err != nil {
		return fmt.Errorf("failed to get processes: %w", err)
	}

	// Reload certificates on all server types
	serverTypes := []client.ServerType{
		client.ServerTypeCoordinator,
		client.ServerTypeDBServer,
		client.ServerTypeAgent,
	}

	for _, serverType := range serverTypes {
		sp, ok := processes.ServerByType(serverType)
		if !ok {
			t.Logf("Server type %s not found, skipping", serverType)
			continue
		}

		scheme := "http"
		if sp.IsSecure {
			scheme = "https"
		}
		fmt.Printf("port %d\n", sp.Port)
		url := fmt.Sprintf("%s://%s:%d/_admin/server/tls", scheme, sp.IP, sp.Port)
		t.Logf("Reloading certificate on %s at %s", serverType, url)

		req, err := http.NewRequest("POST", url, nil)
		if err != nil {
			return fmt.Errorf("failed to create request for %s: %w", serverType, err)
		}

		resp, err := httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("failed to reload certificate on %s: %w", serverType, err)
		}
		defer resp.Body.Close()
		t.Logf("resp ststus code %d", resp.StatusCode)
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("failed to reload certificate on %s: status %d, body: %s",
				serverType, resp.StatusCode, string(body))
		}

		t.Logf("Successfully reloaded certificate on %s", serverType)
	}

	return nil
}

// Helper function to get certificate serial numbers from all server types
func getCertificateSerials(t *testing.T, starterEndpoint string) map[client.ServerType]string {
	c := NewStarterClient(t, starterEndpoint)
	ctx := context.Background()

	processes, err := c.Processes(ctx)
	require.NoError(t, err, "Failed to get processes")

	serials := make(map[client.ServerType]string)
	serverTypes := []client.ServerType{
		client.ServerTypeCoordinator,
		client.ServerTypeDBServer,
		client.ServerTypeAgent,
	}

	for _, serverType := range serverTypes {
		sp, ok := processes.ServerByType(serverType)
		if !ok {
			continue
		}

		scheme := "http"
		if sp.IsSecure {
			scheme = "https"
		}

		url := fmt.Sprintf("%s://%s:%d/_admin/server/availability", scheme, sp.IP, sp.Port)

		// Create a fresh HTTP client for each request to ensure new TLS handshake
		freshClient := &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
				},
				DisableKeepAlives: true, // Force new connection
			},
			Timeout: 10 * time.Second,
		}

		resp, err := freshClient.Get(url)
		if err != nil {
			t.Logf("Warning: failed to get availability for %s: %v", serverType, err)
			continue
		}
		defer resp.Body.Close()

		// Get certificate from TLS connection state
		if resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
			cert := resp.TLS.PeerCertificates[0]
			serials[serverType] = cert.SerialNumber.String()
			t.Logf("Certificate serial on %s: %s (Subject: %s)",
				serverType, serials[serverType], cert.Subject.Organization)
		}
	}

	return serials
}

// TestProcessClusterSSLCertRotationHotReload tests certificate hot reload without restart
// This test validates Scenario 2: Changing certificate content only via /_admin/server/tls endpoint
func TestProcessClusterSSLCertRotationHotReload(t *testing.T) {
	removeArangodProcesses(t)
	testMatch(t, testModeProcess, starterModeCluster, false)

	// Create temporary directory for certificates
	certDir, err := os.MkdirTemp("", "ssl-cert-test")
	require.NoError(t, err, "Failed to create temp directory")
	defer os.RemoveAll(certDir)

	// Create first certificate
	cert1Path := filepath.Join(certDir, "server.keyfile")
	err = createTestCertificate(cert1Path, "initial-cert")
	require.NoError(t, err, "Failed to create initial certificate")

	dataDirMaster := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDirMaster)

	start := time.Now()

	// Start cluster with SSL enabled using first certificate
	masterCmd := fmt.Sprintf("${STARTER} --ssl.keyfile=%s %s", cert1Path, createEnvironmentStarterOptions())
	master := Spawn(t, masterCmd)
	defer master.Close()

	dataDirSlave1 := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDirSlave1)
	slave1Cmd := fmt.Sprintf("${STARTER} --starter.join 127.0.0.1 --ssl.keyfile=%s %s", cert1Path, createEnvironmentStarterOptions())
	slave1 := Spawn(t, slave1Cmd)
	defer slave1.Close()

	dataDirSlave2 := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDirSlave2)
	slave2Cmd := fmt.Sprintf("${STARTER} --starter.join 127.0.0.1 --ssl.keyfile=%s %s", cert1Path, createEnvironmentStarterOptions())
	slave2 := Spawn(t, slave2Cmd)
	defer slave2.Close()

	// Wait for cluster to be ready
	if ok := WaitUntilStarterReady(t, whatCluster, 3, master, slave1, slave2); ok {
		t.Logf("Cluster start with SSL took %s", time.Since(start))
		testCluster(t, secureStarterEndpoint(0*portIncrement), true)
		testCluster(t, secureStarterEndpoint(1*portIncrement), true)
		testCluster(t, secureStarterEndpoint(2*portIncrement), true)
	}

	// Get the certificate serial number from all server types before rotation
	t.Log("Checking initial certificate on all server types")
	initialCertSerials := getCertificateSerials(t, secureStarterEndpoint(0*portIncrement))
	require.NotEmpty(t, initialCertSerials, "Should have initial certificate serials")

	// Create and write new certificate to the SAME path
	t.Log("Creating new certificate at the same path")
	err = createTestCertificate(cert1Path, "rotated-cert")
	require.NoError(t, err, "Failed to create rotated certificate")

	// Force filesystem sync to ensure certificate file is written (important in containers)
	t.Log("Forcing filesystem sync")
	syncCmd := exec.Command("sync")
	if err := syncCmd.Run(); err != nil {
		t.Logf("Warning: sync command failed: %v", err)
	}
	// Give filesystem time to propagate changes (especially in container overlayfs)
	time.Sleep(10 * time.Second)

	// Hot reload certificates via REST API on all server types
	t.Log("Triggering hot reload on all server types via /_admin/server/tls")
	err = reloadCertificatesViaAPI(t, secureStarterEndpoint(0*portIncrement))
	require.NoError(t, err, "Failed to reload certificates via API")
	err = reloadCertificatesViaAPI(t, secureStarterEndpoint(1*portIncrement))
	require.NoError(t, err, "Failed to reload certificates via API")
	err = reloadCertificatesViaAPI(t, secureStarterEndpoint(2*portIncrement))
	require.NoError(t, err, "Failed to reload certificates via API")

	// Give servers time to reload - DBServers need more time than coordinators/agents
	// In container environments (CircleCI), filesystem caching can delay the reload
	t.Log("Waiting 30 seconds for certificates to be reloaded...")
	time.Sleep(30 * time.Second)

	// Verify certificates were reloaded by checking serial numbers changed
	t.Log("Verifying certificates were reloaded")
	newCertSerials := getCertificateSerials(t, secureStarterEndpoint(0*portIncrement))
	require.NotEmpty(t, newCertSerials, "Should have new certificate serials")

	// Verify the serials are different (certificate was actually rotated)
	// All server types should successfully reload certificates via hot reload
	rotatedServers := make(map[client.ServerType]bool)
	for serverType, newSerial := range newCertSerials {
		initialSerial := initialCertSerials[serverType]
		if newSerial != initialSerial {
			t.Logf("Certificate rotated successfully on %s: %s -> %s", serverType, initialSerial, newSerial)
			rotatedServers[serverType] = true
		} else {
			t.Errorf("Certificate NOT rotated on %s: serial remained %s", serverType, initialSerial)
		}
	}

	// Verify all server types rotated successfully
	require.True(t, rotatedServers[client.ServerTypeCoordinator], "Coordinator should have successfully rotated certificate")
	require.True(t, rotatedServers[client.ServerTypeDBServer], "DBServer should have successfully rotated certificate")
	require.True(t, rotatedServers[client.ServerTypeAgent], "Agent should have successfully rotated certificate")

	// t.Log("NOTE: Hot reload works reliably on agents, but coordinators/dbservers may require restart for full certificate rotation")

	// Verify cluster still works after rotation
	t.Log("Verifying cluster functionality after certificate rotation")
	testCluster(t, secureStarterEndpoint(0*portIncrement), true)

	if isVerbose {
		t.Log("Waiting for termination")
	}
	SendIntrAndWait(t, master, slave1, slave2)
}
