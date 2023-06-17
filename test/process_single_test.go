//
// DISCLAIMER
//
// Copyright 2017-2023 ArangoDB GmbH, Cologne, Germany
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
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/arangodb/go-driver"
)

// TestProcessSingle runs `arangodb --starter.mode=single`
func TestProcessSingle(t *testing.T) {
	removeArangodProcesses(t)
	needTestMode(t, testModeProcess)
	needStarterMode(t, starterModeSingle)
	dataDir := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDir)

	start := time.Now()

	child := Spawn(t, "${STARTER} --starter.mode=single "+createEnvironmentStarterOptions())
	defer child.Close()

	if ok := WaitUntilStarterReady(t, whatSingle, 1, child); ok {
		t.Logf("Single server start took %s", time.Since(start))
		testSingle(t, insecureStarterEndpoint(0*portIncrement), false)
	}

	if isVerbose {
		t.Log("Waiting for termination")
	}
	SendIntrAndWait(t, child)
}

// TestProcessSingleShutdownViaAPI runs `arangodb --starter.mode=single`, stopping it through the `/shutdown` API.
func TestProcessSingleShutdownViaAPI(t *testing.T) {
	removeArangodProcesses(t)
	needTestMode(t, testModeProcess)
	needStarterMode(t, starterModeSingle)
	dataDir := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDir)

	start := time.Now()

	child := Spawn(t, "${STARTER} --starter.mode=single "+createEnvironmentStarterOptions())
	defer child.Close()

	if ok := WaitUntilStarterReady(t, whatSingle, 1, child); ok {
		t.Logf("Single server start took %s", time.Since(start))
		testSingle(t, insecureStarterEndpoint(0*portIncrement), false)
	}

	waitForCallFunction(t,
		ShutdownStarterCall(insecureStarterEndpoint(0*portIncrement)))
}

// TestProcessSingleAutoKeyFile runs `arangodb --starter.mode=single --ssl.auto-key`
func TestProcessSingleAutoKeyFile(t *testing.T) {
	removeArangodProcesses(t)
	needTestMode(t, testModeProcess)
	needStarterMode(t, starterModeSingle)
	dataDir := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDir)

	start := time.Now()

	child := Spawn(t, "${STARTER} --starter.mode=single --ssl.auto-key "+createEnvironmentStarterOptions())
	defer child.Close()

	if ok := WaitUntilStarterReady(t, whatSingle, 1, child); ok {
		t.Logf("Single server start took %s", time.Since(start))
		testSingle(t, secureStarterEndpoint(0*portIncrement), true)
	}

	if isVerbose {
		t.Log("Waiting for termination")
	}
	SendIntrAndWait(t, child)
}

// TestOldProcessSingle runs `arangodb --mode=single`
func TestOldProcessSingle(t *testing.T) {
	removeArangodProcesses(t)
	needTestMode(t, testModeProcess)
	needStarterMode(t, starterModeSingle)
	dataDir := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDir)

	start := time.Now()

	child := Spawn(t, "${STARTER} --mode=single "+createEnvironmentStarterOptions())
	defer child.Close()

	if ok := WaitUntilStarterReady(t, whatSingle, 1, child); ok {
		t.Logf("Single server start took %s", time.Since(start))
		testSingle(t, insecureStarterEndpoint(0*portIncrement), false)
	}

	if isVerbose {
		t.Log("Waiting for termination")
	}
	SendIntrAndWait(t, child)
}

// TestOldProcessSingleAutoKeyFile runs `arangodb --mode=single --sslAutoKeyFile`
func TestOldProcessSingleAutoKeyFile(t *testing.T) {
	removeArangodProcesses(t)
	needTestMode(t, testModeProcess)
	needStarterMode(t, starterModeSingle)
	dataDir := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDir)

	start := time.Now()

	child := Spawn(t, "${STARTER} --mode=single --sslAutoKeyFile "+createEnvironmentStarterOptions())
	defer child.Close()

	if ok := WaitUntilStarterReady(t, whatSingle, 1, child); ok {
		t.Logf("Single server start took %s", time.Since(start))
		testSingle(t, secureStarterEndpoint(0*portIncrement), true)
	}

	if isVerbose {
		t.Log("Waiting for termination")
	}
	SendIntrAndWait(t, child)
}

// TestProcessSingleCheckPersistentOptions runs arangodb in single mode and
// checks that overriding an persistent option results in an error message
func TestProcessSingleCheckPersistentOptions(t *testing.T) {
	removeArangodProcesses(t)
	needTestMode(t, testModeProcess)
	needStarterMode(t, starterModeSingle)

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
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			dataDir := SetUniqueDataDir(t)
			defer os.RemoveAll(dataDir)
			// first start
			start := time.Now()
			child := Spawn(t, "${STARTER} --starter.mode=single "+createEnvironmentStarterOptions()+" "+tc.oldOption)
			defer child.Close()
			if ok := WaitUntilStarterReady(t, whatSingle, 1, child); ok {
				t.Logf("Single server start took %s", time.Since(start))
				testSingle(t, insecureStarterEndpoint(0*portIncrement), false)
			}
			err = child.EnsureNoMatches(context.Background(), time.Second*3, re, t.Name())
			require.NoError(t, err)
			SendIntrAndWait(t, child)

			// second start
			start = time.Now()
			child = Spawn(t, "${STARTER} --starter.mode=single "+createEnvironmentStarterOptions()+" "+tc.newOption)
			defer child.Close()
			if ok := WaitUntilStarterReady(t, whatSingle, 1, child); ok {
				t.Logf("Single server start took %s", time.Since(start))
				testSingle(t, insecureStarterEndpoint(0*portIncrement), false)
			}

			if tc.shouldHaveErr {
				err = child.ExpectTimeout(context.Background(), time.Second*3, re, t.Name())
			} else {
				err = child.EnsureNoMatches(context.Background(), time.Second*3, re, t.Name())
			}
			require.NoError(t, err)

			SendIntrAndWait(t, child)
		})
	}
}
