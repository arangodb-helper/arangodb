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
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProcessClusterDifferentLogDir(t *testing.T) {
	removeArangodProcesses(t)
	needTestMode(t, testModeProcess)
	needStarterMode(t, starterModeCluster)
	dataDirMaster := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDirMaster)

	start := time.Now()

	logDir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatalf("TempDir failed: %v", err)
	}

	masterLogDir := filepath.Join(logDir, "master")
	master := Spawn(t, "${STARTER} --log.dir="+masterLogDir+" "+createEnvironmentStarterOptions())
	defer master.Close()

	slave1LogDir := filepath.Join(logDir, "slave1")
	dataDirSlave1 := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDirSlave1)
	slave1 := Spawn(t, "${STARTER} --starter.join 127.0.0.1 --log.dir="+slave1LogDir+" "+createEnvironmentStarterOptions())
	defer slave1.Close()

	slave2LogDir := filepath.Join(logDir, "slave2")
	dataDirSlave2 := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDirSlave2)
	slave2 := Spawn(t, "${STARTER} --starter.join 127.0.0.1 --log.dir="+slave2LogDir+" "+createEnvironmentStarterOptions())
	defer slave2.Close()

	if ok := WaitUntilStarterReady(t, whatCluster, 3, master, slave1, slave2); ok {
		t.Logf("Cluster start took %s", time.Since(start))
		testCluster(t, insecureStarterEndpoint(0*portIncrement), false)
		testCluster(t, insecureStarterEndpoint(1*portIncrement), false)
		testCluster(t, insecureStarterEndpoint(2*portIncrement), false)
	}

	check := func(rootDir string, expectedFileCount int) {
		files, err := getRecursiveLogFiles(rootDir)
		if err != nil {
			t.Errorf("Failed to get log files in %s: %v", rootDir, err)
		} else if len(files) != expectedFileCount {
			t.Errorf("Expected %d log files in %s, got %d (%v)", expectedFileCount, rootDir, len(files), files)
		}
	}
	check(dataDirMaster, 0)
	check(dataDirSlave1, 0)
	check(dataDirSlave2, 0)
	check(masterLogDir, 3)
	check(slave1LogDir, 3)
	check(slave2LogDir, 3)

	if isVerbose {
		t.Log("Waiting for termination")
	}
	SendIntrAndWait(t, master, slave1, slave2)
}

// getRecursiveLogFiles returns a list of all files (their path) starting with 'arangd' and ending with '.log'
func getRecursiveLogFiles(rootDir string) ([]string, error) {
	entries, err := ioutil.ReadDir(rootDir)
	if err != nil {
		return nil, maskAny(err)
	}
	var result []string
	for _, entry := range entries {
		if entry.IsDir() {
			childFiles, err := getRecursiveLogFiles(filepath.Join(rootDir, entry.Name()))
			if err != nil {
				return nil, maskAny(err)
			}
			result = append(result, childFiles...)
		} else if strings.HasPrefix(entry.Name(), "arangod") && strings.HasSuffix(entry.Name(), ".log") {
			result = append(result, filepath.Join(rootDir, entry.Name()))
		}
	}
	return result, nil
}
