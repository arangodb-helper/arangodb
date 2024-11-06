//
// DISCLAIMER
//
// Copyright 2017-2024 ArangoDB GmbH, Cologne, Germany
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
	"testing"
	"time"
)

// TestDockerDatabaseVersion runs the arangodb starter in docker with `--starter.mode=single`
// and tries a `/database-version` request.
func TestDockerDatabaseVersion(t *testing.T) {
	testMatch(t, testModeDocker, starterModeSingle, false)

	cID := createDockerID("starter-test-cluster-default1-")
	createDockerVolume(t, cID)
	defer removeDockerVolume(t, cID)

	// Cleanup of left over tests
	removeDockerContainersByLabel(t, "starter-test=true")
	removeStarterCreatedDockerContainers(t)

	start := time.Now()

	dockerRun := spawnMemberInDocker(t, basePort, cID, "", "--starter.mode=single", "")
	defer dockerRun.Close()
	defer removeDockerContainer(t, cID)

	if ok := WaitUntilStarterReady(t, whatSingle, 1, dockerRun); ok {
		t.Logf("Single server start took %s", time.Since(start))
		testSingle(t, insecureStarterEndpoint(0*portIncrement), false)
	}

	if isVerbose {
		t.Log("Waiting for termination")
	}

	c := NewStarterClient(t, insecureStarterEndpoint(0*portIncrement))
	ctx := context.Background()
	if v, err := c.DatabaseVersion(ctx); err != nil {
		t.Errorf("DatabaseVersion failed: %#v", err)
	} else {
		t.Logf("Got database-version %s", v)
	}

	waitForCallFunction(t,
		ShutdownStarterCall(insecureStarterEndpoint(0*portIncrement)))
}
