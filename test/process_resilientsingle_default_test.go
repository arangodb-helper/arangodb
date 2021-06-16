//
// DISCLAIMER
//
// Copyright 2017 ArangoDB GmbH, Cologne, Germany
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
	"time"
)

// TestProcessResilientSingleDefault starts a master starter, followed by 2 slave starters.
// All are started in resilientsingle mode.
func TestProcessResilientSingleDefault(t *testing.T) {
	removeArangodProcesses(t)
	needTestMode(t, testModeProcess)
	needStarterMode(t, starterModeActiveFailover)
	dataDirMaster := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDirMaster)

	start := time.Now()

	master := Spawn(t, "${STARTER} --starter.mode=resilientsingle "+createEnvironmentStarterOptions())
	defer master.Close()

	dataDirSlave1 := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDirSlave1)
	slave1 := Spawn(t, "${STARTER} --starter.port=8538 --starter.mode=resilientsingle --starter.join 127.0.0.1 "+createEnvironmentStarterOptions())
	defer slave1.Close()

	dataDirSlave2 := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDirSlave2)
	slave2 := Spawn(t, "${STARTER} --starter.port=8548 --starter.mode=resilientsingle --cluster.start-single=false --starter.join 127.0.0.1 "+createEnvironmentStarterOptions())
	defer slave2.Close()

	if ok := WaitUntilStarterReady(t, whatResilientSingle, 1, master, slave1 /* not slave2 */); ok {
		t.Logf("ResilientSingle start took %s", time.Since(start))
		testResilientSingle(t, insecureStarterEndpoint(0*portIncrement), false, false)
		testResilientSingle(t, insecureStarterEndpoint(1*portIncrement), false, false)
		testResilientSingle(t, insecureStarterEndpoint(2*portIncrement), false, true) // due to --cluster.start-single=false
	}

	if isVerbose {
		t.Log("Waiting for termination")
	}
	SendIntrAndWait(t, master, slave1, slave2)
}

// TestProcessResilientSingleDefaultShutdownViaAPI starts a master starter, followed by 2 slave starters, shutting all down through the API.
// All are started in resilientsingle mode.
func TestProcessResilientSingleDefaultShutdownViaAPI(t *testing.T) {
	removeArangodProcesses(t)
	needTestMode(t, testModeProcess)
	needStarterMode(t, starterModeActiveFailover)
	dataDirMaster := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDirMaster)

	start := time.Now()

	master := Spawn(t, "${STARTER} --starter.mode=resilientsingle "+createEnvironmentStarterOptions())
	defer master.Close()

	dataDirSlave1 := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDirSlave1)
	slave1 := Spawn(t, "${STARTER} --starter.port=8538 --starter.mode=resilientsingle --starter.join 127.0.0.1 "+createEnvironmentStarterOptions())
	defer slave1.Close()

	dataDirSlave2 := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDirSlave2)
	slave2 := Spawn(t, "${STARTER} --starter.port=8548 --starter.mode=resilientsingle --starter.join 127.0.0.1 "+createEnvironmentStarterOptions())
	defer slave2.Close()

	if ok := WaitUntilStarterReady(t, whatResilientSingle, 1, master, slave1, slave2); ok {
		t.Logf("ResilientSingle start took %s", time.Since(start))
		testResilientSingle(t, insecureStarterEndpoint(0*portIncrement), false, false)
		testResilientSingle(t, insecureStarterEndpoint(1*portIncrement), false, false)
		testResilientSingle(t, insecureStarterEndpoint(2*portIncrement), false, false)
	}

	waitForCallFunction(t,
		ShutdownStarterCall(insecureStarterEndpoint(0*portIncrement)),
		ShutdownStarterCall(insecureStarterEndpoint(1*portIncrement)),
		ShutdownStarterCall(insecureStarterEndpoint(2*portIncrement)))
}
