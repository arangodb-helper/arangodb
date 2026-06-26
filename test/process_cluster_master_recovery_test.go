//
// DISCLAIMER
//
// Copyright 2026 ArangoDB GmbH, Cologne, Germany
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
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"syscall"
	"testing"
	"time"
)

const recoveryFailurePattern = `(?i)(Failed to recover|Cannot get cluster configuration from remaining starters|no remaining starter addresses in --starter\.join)`

// logSubProcessOutput logs captured stdout/stderr from a starter subprocess (for debugging tests).
func logSubProcessOutput(t *testing.T, label string, sp *SubProcess) {
	t.Helper()
	t.Logf("--- %s output ---\n%s", label, sp.Output())
}

func spawnProcessClusterMember(t *testing.T, dataDir string, port int, joins string) *SubProcess {
	cmd := fmt.Sprintf("${STARTER} --starter.data-dir=%s --starter.port=%d", dataDir, port)
	if joins != "" {
		cmd += " " + joins
	}
	cmd += " " + createEnvironmentStarterOptions()
	return Spawn(t, cmd)
}

func killStarterAndServers(t *testing.T, starter *SubProcess, starterEndpoint string) {
	ctx := context.Background()
	c := NewStarterClient(t, starterEndpoint)
	plist, err := c.Processes(ctx)
	if err != nil {
		t.Fatalf("Processes failed: %s", describe(err))
	}
	starter.Kill()
	for _, s := range plist.Servers {
		if p, err := os.FindProcess(s.ProcessID); err != nil {
			t.Errorf("Cannot find process %d: %s", s.ProcessID, describe(err))
		} else {
			p.Signal(syscall.SIGKILL)
		}
	}
}

func writeRecoveryFile(t *testing.T, dataDir, recoveryAddress string) {
	t.Helper()
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatalf("Failed to create recovery data dir: %s", describe(err))
	}
	recoveryPath := filepath.Join(dataDir, "RECOVERY")
	if err := os.WriteFile(recoveryPath, []byte(recoveryAddress), 0644); err != nil {
		t.Fatalf("Failed to create RECOVERY file: %s", describe(err))
	}
}

// TestProcessClusterMasterRecovery starts a 3-node cluster, destroys the bootstrap master,
// and recovers it using the surviving starters in --starter.join (required workaround).
func TestProcessClusterMasterRecovery(t *testing.T) {
	removeArangodProcesses(t)
	testMatch(t, testModeProcess, starterModeCluster, false)

	start := time.Now()
	log := GetLogger(t)

	dataDirMaster := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDirMaster)
	master := spawnProcessClusterMember(t, dataDirMaster, basePort, "")
	master.label = "Master"
	defer closeProcess(t, master, "Master")

	dataDirSlave1 := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDirSlave1)
	slave1 := spawnProcessClusterMember(t, dataDirSlave1, basePort+100, fmt.Sprintf("--starter.join=127.0.0.1:%d", basePort))
	slave1.label = "Slave1"
	defer closeProcess(t, slave1, "Slave1")

	dataDirSlave2 := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDirSlave2)
	slave2 := spawnProcessClusterMember(t, dataDirSlave2, basePort+200, fmt.Sprintf("--starter.join=127.0.0.1:%d", basePort))
	slave2.label = "Slave2"
	defer closeProcess(t, slave2, "Slave2")

	if ok := WaitUntilStarterReady(t, whatCluster, 3, master, slave1, slave2); ok {
		t.Logf("Cluster start took %s", time.Since(start))
		testCluster(t, insecureStarterEndpoint(0), false)
		testCluster(t, insecureStarterEndpoint(100), false)
		testCluster(t, insecureStarterEndpoint(200), false)
	}

	killStarterAndServers(t, master, insecureStarterEndpoint(0))
	os.RemoveAll(dataDirMaster)

	log.Log("Wait for leader election on surviving starters")
	time.Sleep(35 * time.Second)

	checkpoint := log.Checkpoint()
	checkpoint.Log("Wait for master port to be closed")
	WaitForHttpPortClosed(checkpoint, NewThrottle(time.Second), insecureStarterEndpoint(0)).ExecuteT(t, time.Minute, time.Second)

	recDataDir := SetUniqueDataDir(t)
	defer os.RemoveAll(recDataDir)
	writeRecoveryFile(t, recDataDir, fmt.Sprintf("127.0.0.1:%d", basePort))

	masterRecovery := spawnProcessClusterMember(t, recDataDir, basePort,
		fmt.Sprintf("--starter.join=127.0.0.1:%d,127.0.0.1:%d", basePort+100, basePort+200))
	masterRecovery.label = "Master Recovery"
	defer closeProcess(t, masterRecovery, "Master Recovery")

	if ok := WaitUntilStarterReady(t, whatCluster, 3, masterRecovery, slave1, slave2); ok {
		t.Logf("Cluster start (with master recovery) took %s", time.Since(start))
		testCluster(t, insecureStarterEndpoint(0), false)
		testCluster(t, insecureStarterEndpoint(100), false)
		testCluster(t, insecureStarterEndpoint(200), false)
	}

	startWait := time.Now()
	for {
		if _, err := os.Stat(filepath.Join(recDataDir, "RECOVERY")); os.IsNotExist(err) {
			t.Log("RECOVERY file has vanished, good.")
			break
		}
		time.Sleep(time.Second)
		if time.Since(startWait) > 30*time.Second {
			t.Errorf("Expected RECOVERY file to not exist in %s", recDataDir)
			break
		}
	}

	SendIntrAndWait(t, masterRecovery, slave1, slave2)
}

// TestProcessClusterMasterRecoverySelfJoinOnly reproduces the customer bug on a live 3-node cluster:
// after the bootstrap master is destroyed, recovery with only the replaced address in --starter.join
// must fail fast and must not rejoin the cluster as a new peer.
func TestProcessClusterMasterRecoverySelfJoinOnly(t *testing.T) {
	removeArangodProcesses(t)
	testMatch(t, testModeProcess, starterModeCluster, false)

	log := GetLogger(t)

	dataDirMaster := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDirMaster)
	master := spawnProcessClusterMember(t, dataDirMaster, basePort, "")
	master.label = "Master"
	defer closeProcess(t, master, "Master")

	dataDirSlave1 := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDirSlave1)
	slave1 := spawnProcessClusterMember(t, dataDirSlave1, basePort+100, fmt.Sprintf("--starter.join=127.0.0.1:%d", basePort))
	slave1.label = "Slave1"
	defer closeProcess(t, slave1, "Slave1")

	dataDirSlave2 := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDirSlave2)
	slave2 := spawnProcessClusterMember(t, dataDirSlave2, basePort+200, fmt.Sprintf("--starter.join=127.0.0.1:%d", basePort))
	slave2.label = "Slave2"
	defer closeProcess(t, slave2, "Slave2")

	if !WaitUntilStarterReady(t, whatCluster, 3, master, slave1, slave2) {
		SendIntrAndWait(t, master, slave1, slave2)
		return
	}

	killStarterAndServers(t, master, insecureStarterEndpoint(0))
	os.RemoveAll(dataDirMaster)

	log.Log("Wait for leader election on surviving starters")
	time.Sleep(35 * time.Second)

	checkpoint := log.Checkpoint()
	checkpoint.Log("Wait for master port to be closed")
	WaitForHttpPortClosed(checkpoint, NewThrottle(time.Second), insecureStarterEndpoint(0)).ExecuteT(t, time.Minute, time.Second)

	recDataDir := SetUniqueDataDir(t)
	defer os.RemoveAll(recDataDir)
	writeRecoveryFile(t, recDataDir, fmt.Sprintf("127.0.0.1:%d", basePort))

	masterRecovery := spawnProcessClusterMember(t, recDataDir, basePort,
		fmt.Sprintf("--starter.join=127.0.0.1:%d", basePort))
	masterRecovery.label = "Master Recovery (self join)"
	defer closeProcess(t, masterRecovery, "Master Recovery (self join)")

	// ExpectTimeout returns nil when the regex MATCHES in process logs (success for this test).
	// It returns err only on timeout — meaning the expected failure message never appeared.
	if err := masterRecovery.ExpectTimeout(
		context.Background(),
		30*time.Second,
		regexp.MustCompile(recoveryFailurePattern),
		"self-join recovery",
	); err != nil {
		logSubProcessOutput(t, "master recovery (self-join, on timeout)", masterRecovery)
		t.Fatalf("expected recovery failure log line within 30s, but timed out: %s", describe(err))
	}
	logSubProcessOutput(t, "master recovery (self-join, matched failure)", masterRecovery)

	if regexp.MustCompile(`Your cluster can now be accessed`).Match(masterRecovery.Output()) {
		logSubProcessOutput(t, "master recovery (unexpected cluster ready)", masterRecovery)
		t.Fatal("recovery starter joined/bootstrapped the cluster instead of failing on self-only join")
	}

	// After log.Fatal in main, the recovery process exits with a non-zero code.
	WaitUntilStarterExit(t, 10*time.Second, 1, masterRecovery)
	t.Log("recovery starter exited as expected after failure")

	// Survivors must still serve the degraded 2-starter cluster.
	testCluster(t, insecureStarterEndpoint(100), false)
	testCluster(t, insecureStarterEndpoint(200), false)

	SendIntrAndWait(t, slave1, slave2)
}
