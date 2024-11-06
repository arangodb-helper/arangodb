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
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestDockerErrExitCodeHandler runs in 'single' mode with invalid ArangoD configuration and
// checks that starter recognizes exit code
func TestDockerErrExitCodeHandler(t *testing.T) {
	testMatch(t, testModeDocker, starterModeSingle, false)

	cID := createDockerID("starter-test-single-")
	createDockerVolume(t, cID)
	defer removeDockerVolume(t, cID)

	// Cleanup of left over tests
	removeDockerContainersByLabel(t, "starter-test=true")
	removeStarterCreatedDockerContainers(t)

	dockerRun := spawnMemberInDocker(t, basePort, cID, "", "--starter.mode=single --args.all.config=invalidvalue", "")
	defer dockerRun.Close()
	defer removeDockerContainer(t, cID)

	re := regexp.MustCompile("has failed 1 times, giving up")
	require.NoError(t, dockerRun.ExpectTimeout(context.Background(), time.Second*20, re, ""))

	require.NoError(t, dockerRun.WaitTimeout(time.Second*10), "Starter is not stopped in time")

	waitForCallFunction(t, ShutdownStarterCall(insecureStarterEndpoint(0*portIncrement)))
}
