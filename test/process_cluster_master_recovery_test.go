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
	"syscall"
	"testing"
	"time"
)

// TestProcessClusterMasterRecovery starts a master starter, followed by 2 slave starters.
// Once started, it destroys the master starter and attempts a recovery.
func TestProcessClusterMasterRecovery(t *testing.T) {
	removeArangodProcesses(t)
	testMatch(t, testModeProcess, starterModeCluster, false)

	start := time.Now()

	dataDirMaster := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDirMaster)
	master := Spawn(t, "${STARTER} --starter.port=8528 "+createEnvironmentStarterOptions())
	master.label = "Master"
	defer closeProcess(t, master, "Master")

	dataDirSlave1 := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDirSlave1)
	slave1 := Spawn(t, "${STARTER} --starter.port=8628 --starter.join 127.0.0.1:8528 "+createEnvironmentStarterOptions())
	slave1.label = "Slave1"
	defer closeProcess(t, slave1, "Slave1")

	dataDirSlave2 := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDirSlave2)
	slave2 := Spawn(t, "${STARTER} --starter.port=8728 --starter.join 127.0.0.1:8528 "+createEnvironmentStarterOptions())
	slave2.label = "Slave2"
	defer closeProcess(t, slave2, "Slave2")

	if ok := WaitUntilStarterReady(t, whatCluster, 3, master, slave1, slave2); ok {
		t.Logf("Cluster start took %s", time.Since(start))
		testCluster(t, insecureStarterEndpoint(0), false)
		testCluster(t, insecureStarterEndpoint(100), false)
		testCluster(t, insecureStarterEndpoint(200), false)
	}

	// Kill master starter and all its processes
	ctx := context.Background()
	c := NewStarterClient(t, insecureStarterEndpoint(0))
	plist, err := c.Processes(ctx)
	if err != nil {
		t.Errorf("Processes failed: %s", describe(err))
		SendIntrAndWait(t, master, slave1, slave2)
		return
	}
	master.Kill()
	for _, s := range plist.Servers {
		if p, err := os.FindProcess(s.ProcessID); err != nil {
			t.Errorf("Cannot find process %d: %s", s.ProcessID, describe(err))
		} else {
			p.Signal(syscall.SIGKILL)
		}
	}
	os.RemoveAll(dataDirMaster)

	// Wait for leader election to move away from the dead master.
	time.Sleep(35 * time.Second)

	os.MkdirAll(dataDirMaster, 0755)
	recoveryContent := fmt.Sprintf("127.0.0.1:%d", basePort)
	if err := os.WriteFile(filepath.Join(dataDirMaster, "RECOVERY"), []byte(recoveryContent), 0644); err != nil {
		t.Errorf("Failed to create RECOVERY file: %s", describe(err))
	}

	os.Setenv("DATA_DIR", dataDirMaster)
	masterRecovery := Spawn(t, "${STARTER} --starter.port=8528 --starter.join 127.0.0.1:8628,127.0.0.1:8728 "+createEnvironmentStarterOptions())
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
		if _, err := os.Stat(filepath.Join(dataDirMaster, "RECOVERY")); os.IsNotExist(err) {
			t.Log("RECOVERY file has vanished, good.")
			break
		}
		time.Sleep(time.Second)
		if time.Since(startWait) > 30*time.Second {
			t.Errorf("Expected RECOVERY file to not-exist, got: %s", describe(err))
		}
	}

	SendIntrAndWait(t, masterRecovery, slave1, slave2)
}
