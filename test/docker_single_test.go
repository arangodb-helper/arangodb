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
	"fmt"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	driver "github.com/arangodb/go-driver/v2/arangodb"
)

// TestDockerSingleAutoKeyFile runs the arangodb starter in docker with `--starter.mode=single` && `--ssl.auto-key`
func TestDockerSingleAutoKeyFile(t *testing.T) {
	testMatch(t, testModeDocker, starterModeSingle, false)

	cID := createDockerID("starter-test-cluster-default1-")
	createDockerVolume(t, cID)
	defer removeDockerVolume(t, cID)

	// Cleanup of left over tests
	removeDockerContainersByLabel(t, "starter-test=true")
	removeStarterCreatedDockerContainers(t)

	joins := fmt.Sprintf("localhost:%d,localhost:%d,localhost:%d", basePort, basePort+(1*portIncrement), basePort+(2*portIncrement))

	start := time.Now()

	dockerRun := spawnMemberInDocker(t, basePort, cID, joins, "--starter.mode=single --ssl.auto-key", "")
	defer dockerRun.Close()
	defer removeDockerContainer(t, cID)

	if ok := WaitUntilStarterReady(t, whatSingle, 1, dockerRun); ok {
		t.Logf("Single server start took %s", time.Since(start))
		testSingle(t, secureStarterEndpoint(0*portIncrement), true)
	}

	waitForCallFunction(t,
		ShutdownStarterCall(secureStarterEndpoint(0*portIncrement)))
}

// TestDockerSingleAutoRocksdb runs the arangodb starter in docker with `--server.storage-engine=rocksdb` and a 3.2+ image.
func TestDockerSingleAutoRocksdb(t *testing.T) {
	testMatch(t, testModeDocker, starterModeSingle, false)

	cID := createDockerID("starter-test-cluster-default1-")
	createDockerVolume(t, cID)
	defer removeDockerVolume(t, cID)

	// Cleanup of left over tests
	removeDockerContainersByLabel(t, "starter-test=true")
	removeStarterCreatedDockerContainers(t)

	joins := fmt.Sprintf("localhost:%d,localhost:%d,localhost:%d", basePort, basePort+(1*portIncrement), basePort+(2*portIncrement))

	start := time.Now()

	dockerRun := spawnMemberInDocker(t, basePort, cID, joins, "--starter.mode=single --server.storage-engine=rocksdb", "")
	defer dockerRun.Close()
	defer removeDockerContainer(t, cID)

	if ok := WaitUntilStarterReady(t, whatSingle, 1, dockerRun); ok {
		t.Logf("Single server start took %s", time.Since(start))
		testSingle(t, insecureStarterEndpoint(0*portIncrement), false)
	}

	waitForCallFunction(t,
		ShutdownStarterCall(insecureStarterEndpoint(0*portIncrement)))
}

// TestDockerSingleCheckPersistentOptions runs arangodb in single mode and
// checks that overriding a persistent option results in an error message
func TestDockerSingleCheckPersistentOptions(t *testing.T) {
	testMatch(t, testModeDocker, starterModeSingle, false)

	re, err := regexp.Compile("ERROR.*it is impossible to change persistent option")
	require.NoError(t, err)

	testCases := map[string]struct {
		minVersion    driver.Version
		oldOption     string
		newOption     string
		shouldHaveErr bool
	}{
		"no-err-change-false-to-true": {
			oldOption:     "--args.dbservers.database.extended-names-databases=false",
			newOption:     "--args.dbservers.database.extended-names-databases=true",
			shouldHaveErr: false,
		},
		"no-err-change-false-to-false": {
			oldOption:     "--args.dbservers.database.extended-names-databases=false",
			newOption:     "--args.dbservers.database.extended-names-databases=false",
			shouldHaveErr: false,
		},
		"no-err-change-true-to-true": {
			oldOption:     "--args.dbservers.database.extended-names-databases=true",
			newOption:     "--args.dbservers.database.extended-names-databases=true",
			shouldHaveErr: false,
		},
		"should-err-change-true-to-false": {
			oldOption:     "--args.dbservers.database.extended-names-databases=true",
			newOption:     "--args.dbservers.database.extended-names-databases=false",
			shouldHaveErr: true,
		},
		"should-err-all-change-true-to-false": {
			oldOption:     "--args.all.database.extended-names-databases=true",
			newOption:     "--args.dbservers.database.extended-names-databases=false",
			shouldHaveErr: true,
		},
		"no-err-all-change-true-to-true": {
			oldOption:     "--args.all.database.extended-names-databases=true",
			newOption:     "--args.dbservers.database.extended-names-databases=true",
			shouldHaveErr: false,
		},
		"should-err-default-lang": {
			oldOption:     "--args.dbservers.default-language=en",
			newOption:     "--args.dbservers.default-language=es",
			shouldHaveErr: true,
		},
		"no-err-default-lang": {
			oldOption:     "--args.dbservers.default-language=es",
			newOption:     "--args.dbservers.default-language=es",
			shouldHaveErr: false,
		},
		"no-err-not-persistent": {
			oldOption:     "--args.dbservers.wait-for-sync=true",
			newOption:     "--args.dbservers.wait-for-sync=false",
			shouldHaveErr: false,
		},
		"no-err-config-no-change": {
			oldOption:     "--configuration=test/testdata/single-passthrough-persistent-old.conf",
			newOption:     "--configuration=test/testdata/single-passthrough-persistent-old.conf",
			shouldHaveErr: false,
		},
		"should-err-config-true-to-false": {
			oldOption:     "--configuration=test/testdata/single-passthrough-persistent-old.conf",
			newOption:     "--configuration=test/testdata/single-passthrough-persistent-new.conf",
			shouldHaveErr: true,
		},
		"should-err-config-replace-true-to-false": {
			oldOption:     "--configuration=test/testdata/single-passthrough-persistent-old.conf",
			newOption:     "--args.dbservers.database.extended-names-databases=false",
			shouldHaveErr: true,
		},
	}

	absTestDataPath, err := filepath.Abs("test/testdata")
	require.NoError(t, err)

	ep := insecureStarterEndpoint(0)

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			cID := createDockerID("starter-test-cluster-default1-")
			createDockerVolume(t, cID)
			defer removeDockerVolume(t, cID)

			// Cleanup of left over tests
			removeDockerContainersByLabel(t, "starter-test=true")
			removeStarterCreatedDockerContainers(t)

			t.Logf("Starting container with old option: %s", tc.oldOption)
			dockerRunOld := spawnMemberInDocker(t, basePort, cID, "", "--starter.mode=single "+tc.oldOption, fmt.Sprintf("-v %s:/test/testdata", absTestDataPath))
			defer dockerRunOld.Close()
			defer removeDockerContainer(t, cID)

			if ok := WaitUntilStarterReady(t, whatSingle, 1, dockerRunOld); ok {
				testSingle(t, ep, false)
			}

			err = dockerRunOld.EnsureNoMatches(context.Background(), time.Second*3, re, t.Name())
			require.NoError(t, err)
			waitForCallFunction(t, ShutdownStarterCall(ep))

			t.Logf("Restarting container with the new option: %s", tc.newOption)
			dockerRunNew := spawnMemberInDocker(t, basePort, cID, "", "--starter.mode=single "+tc.newOption, fmt.Sprintf("-v %s:/test/testdata", absTestDataPath))
			defer dockerRunNew.Close()
			defer removeDockerContainer(t, cID)

			if ok := WaitUntilStarterReady(t, whatSingle, 1, dockerRunNew); ok {
				testSingle(t, ep, false)
			}
			if tc.shouldHaveErr {
				err = dockerRunNew.ExpectTimeout(context.Background(), time.Second*3, re, t.Name())
			} else {
				err = dockerRunNew.EnsureNoMatches(context.Background(), time.Second*3, re, t.Name())
			}
			require.NoError(t, err)
			waitForCallFunction(t, ShutdownStarterCall(ep))
		})
	}
}
