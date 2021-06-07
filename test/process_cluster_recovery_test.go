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
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestProcessClusterRecovery starts a master starter, followed by 2 slave starters.
// Once started, it destroys one of the starters and attempts a recovery.
func TestProcessClusterRecovery(t *testing.T) {
	SkipOnTravis(t, "Test does not work on TRAVIS VM") // TODO: Fix needed

	removeArangodProcesses(t)
	needTestMode(t, testModeProcess)
	needStarterMode(t, starterModeCluster)
	dataDirMaster := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDirMaster)

	start := time.Now()

	master := Spawn(t, "${STARTER} --starter.port=8528 "+createEnvironmentStarterOptions())
	defer closeProcess(t, master, "Master")

	dataDirSlave1 := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDirSlave1)
	slave1 := Spawn(t, "${STARTER} --starter.port=8628 --starter.join 127.0.0.1:8528 "+createEnvironmentStarterOptions())
	defer closeProcess(t, slave1, "Slave1")

	dataDirSlave2 := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDirSlave2)
	slave2 := Spawn(t, "${STARTER} --starter.port=8728 --starter.join 127.0.0.1:8528 "+createEnvironmentStarterOptions())
	defer closeProcess(t, slave2, "Slave2")

	if ok := WaitUntilStarterReady(t, whatCluster, 3, master, slave1, slave2); ok {
		t.Logf("Cluster start took %s", time.Since(start))
		testCluster(t, insecureStarterEndpoint(0), false)
		testCluster(t, insecureStarterEndpoint(100), false)
		testCluster(t, insecureStarterEndpoint(200), false)
	}

	if isVerbose {
		t.Log("Start killing slave1 and its servers")
	}

	// Cluster is up.
	// Kill starter slave-1 and all its processes
	ctx := context.Background()
	c := NewStarterClient(t, insecureStarterEndpoint(100))
	plist, err := c.Processes(ctx)
	if err != nil {
		t.Errorf("Processes failed: %s", describe(err))
		SendIntrAndWait(t, master, slave1, slave2)
		return
	}
	// Kill starter
	slave1.Kill()
	// Kill servers
	for _, s := range plist.Servers {
		if p, err := os.FindProcess(s.ProcessID); err != nil {
			t.Errorf("Cannot find process %d: %s", s.ProcessID, describe(err))
		} else {
			p.Signal(syscall.SIGKILL)
		}
	}

	// Remove entire slave-1 datadir
	os.RemoveAll(dataDirSlave1)

	if isVerbose {
		t.Log("Starting recovery...")
	}

	// Create RECOVERY file
	os.MkdirAll(dataDirSlave1, 0755)
	recoveryContent := fmt.Sprintf("127.0.0.1:%d", basePort+(100))
	if err := ioutil.WriteFile(filepath.Join(dataDirSlave1, "RECOVERY"), []byte(recoveryContent), 0644); err != nil {
		t.Errorf("Failed to create RECOVERY file: %s", describe(err))
	}

	// Restart slave1
	os.Setenv("DATA_DIR", dataDirSlave1)
	master = Spawn(t, "${STARTER} --starter.port=8628 --starter.join 127.0.0.1:8528 "+createEnvironmentStarterOptions())
	defer closeProcess(t, master, "Master 2")

	// Wait until recovered
	if ok := WaitUntilStarterReady(t, whatCluster, 3, master, slave1, slave2); ok {
		t.Logf("Cluster start (with recovery) took %s", time.Since(start))
		testCluster(t, insecureStarterEndpoint(0), false)
		testCluster(t, insecureStarterEndpoint(100), false)
		testCluster(t, insecureStarterEndpoint(200), false)
	}

	// RECOVERY file must now be gone within 30s:
	startWait := time.Now()
	for {
		if _, err := os.Stat(filepath.Join(dataDirSlave1, "RECOVERY")); os.IsNotExist(err) {
			t.Log("RECOVERY file has vanished, good.")
			break
		}
		time.Sleep(time.Second)
		if time.Now().Sub(startWait) > 30*time.Second {
			t.Errorf("Expected RECOVERY file to not-exist, got: %s", describe(err))
		}
	}

	if isVerbose {
		t.Log("Waiting for termination")
	}
	SendIntrAndWait(t, master, slave1, slave2)
}
