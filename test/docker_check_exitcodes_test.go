//
// DISCLAIMER
//
// Copyright 2017-2022 ArangoDB GmbH, Cologne, Germany
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
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestDockerErrExitCodeHandler runs in 'single' mode with invalid ArangoD configuration and
// checks that exit code is recognized by starter
func TestDockerErrExitCodeHandler(t *testing.T) {
	needTestMode(t, testModeDocker)
	needStarterMode(t, starterModeSingle)
	if os.Getenv("IP") == "" {
		t.Fatal("IP envvar must be set to IP address of this machine")
	}

	volID := createDockerID("vol-starter-test-single-")
	createDockerVolume(t, volID)
	defer removeDockerVolume(t, volID)

	// Cleanup of left over tests
	removeDockerContainersByLabel(t, "starter-test=true")
	removeStarterCreatedDockerContainers(t)

	cID := createDockerID("starter-test-single-")
	dockerRun := Spawn(t, strings.Join([]string{
		"docker run -i",
		"--label starter-test=true",
		"--name=" + cID,
		"--rm",
		createLicenseKeyOption(),
		fmt.Sprintf("-p %d:%d", basePort, basePort),
		fmt.Sprintf("-v %s:/data", volID),
		"-v /var/run/docker.sock:/var/run/docker.sock",
		"arangodb/arangodb-starter",
		"--docker.container=" + cID,
		"--starter.address=$IP",
		"--starter.mode=single",
		"--args.all.config=invalidvalue",
		createEnvironmentStarterOptions(),
	}, " "))
	defer dockerRun.Close()
	defer removeDockerContainer(t, cID)

	re := regexp.MustCompile("has failed 1 times, giving up")
	require.Error(t, dockerRun.ExpectTimeout(context.Background(), time.Second*20, re, ""))

	require.NoError(t, dockerRun.WaitTimeout(time.Second*10), "Starter is not stopped in time")

	waitForCallFunction(t,
		ShutdownStarterCall(insecureStarterEndpoint(0*portIncrement)))
}
