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
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/arangodb/go-driver"
)

// TestDockerSingle runs the arangodb starter in docker with `--starter.mode=single`
func TestDockerSingle(t *testing.T) {
	testMatch(t, testModeDocker, starterModeSingle, false)
	if os.Getenv("IP") == "" {
		t.Fatal("IP envvar must be set to IP address of this machine")
	}
	/*
		docker volume create arangodb1
		docker run -i --name=adb1 --rm -p 8528:8528 \
			-v arangodb1:/data \
			-v /var/run/docker.sock:/var/run/docker.sock \
			arangodb/arangodb-starter \
			--docker.container=adb1 \
			--starter.address=$IP \
			--starter.mode=single
	*/
	volID := createDockerID("vol-starter-test-single-")
	createDockerVolume(t, volID)
	defer removeDockerVolume(t, volID)

	// Cleanup of left over tests
	removeDockerContainersByLabel(t, "starter-test=true")
	removeStarterCreatedDockerContainers(t)

	start := time.Now()

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
		createEnvironmentStarterOptions(),
	}, " "))
	defer dockerRun.Close()
	defer removeDockerContainer(t, cID)

	if ok := WaitUntilStarterReady(t, whatSingle, 1, dockerRun); ok {
		t.Logf("Single server start took %s", time.Since(start))
		testSingle(t, insecureStarterEndpoint(0*portIncrement), false)
	}

	waitForCallFunction(t,
		ShutdownStarterCall(insecureStarterEndpoint(0*portIncrement)))
}

// TestDockerSingleAutoKeyFile runs the arangodb starter in docker with `--starter.mode=single` && `--ssl.auto-key`
func TestDockerSingleAutoKeyFile(t *testing.T) {
	testMatch(t, testModeDocker, starterModeSingle, false)
	if os.Getenv("IP") == "" {
		t.Fatal("IP envvar must be set to IP address of this machine")
	}
	/*
		docker volume create arangodb1
		docker run -i --name=adb1 --rm -p 8528:8528 \
			-v arangodb1:/data \
			-v /var/run/docker.sock:/var/run/docker.sock \
			arangodb/arangodb-starter \
			--docker.container=adb1 \
			--starter.ddress=$IP \
			--starter.mode=single \
			--ssl.auto-key
	*/
	volID := createDockerID("vol-starter-test-single-")
	createDockerVolume(t, volID)
	defer removeDockerVolume(t, volID)

	// Cleanup of left over tests
	removeDockerContainersByLabel(t, "starter-test=true")
	removeStarterCreatedDockerContainers(t)

	start := time.Now()

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
		"--ssl.auto-key",
		createEnvironmentStarterOptions(),
	}, " "))
	defer dockerRun.Close()
	defer removeDockerContainer(t, cID)

	if ok := WaitUntilStarterReady(t, whatSingle, 1, dockerRun); ok {
		t.Logf("Single server start took %s", time.Since(start))
		testSingle(t, secureStarterEndpoint(0*portIncrement), true)
	}

	waitForCallFunction(t,
		ShutdownStarterCall(secureStarterEndpoint(0*portIncrement)))
}

// TestDockerSingleAutoContainerName runs the arangodb starter in docker with `--starter.mode=single` automatic detection of its container name.
func TestDockerSingleAutoContainerName(t *testing.T) {
	testMatch(t, testModeDocker, starterModeSingle, false)
	if os.Getenv("IP") == "" {
		t.Fatal("IP envvar must be set to IP address of this machine")
	}
	/*
		docker volume create arangodb1
		docker run -i --name=adb1 --rm -p 8528:8528 \
			-v arangodb1:/data \
			-v /var/run/docker.sock:/var/run/docker.sock \
			arangodb/arangodb-starter \
			--starter.address=$IP \
			--starter.mode=single
	*/
	volID := createDockerID("vol-starter-test-single-")
	createDockerVolume(t, volID)
	defer removeDockerVolume(t, volID)

	// Cleanup of left over tests
	removeDockerContainersByLabel(t, "starter-test=true")
	removeStarterCreatedDockerContainers(t)

	start := time.Now()

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
		"--starter.address=$IP",
		"--starter.mode=single",
		createEnvironmentStarterOptions(),
	}, " "))
	defer dockerRun.Close()
	defer removeDockerContainer(t, cID)

	if ok := WaitUntilStarterReady(t, whatSingle, 1, dockerRun); ok {
		t.Logf("Single server start took %s", time.Since(start))
		testSingle(t, insecureStarterEndpoint(0*portIncrement), false)
	}

	waitForCallFunction(t,
		ShutdownStarterCall(insecureStarterEndpoint(0*portIncrement)))
}

// TestDockerSingleAutoRocksdb runs the arangodb starter in docker with `--server.storage-engine=rocksdb` and a 3.2+ image.
func TestDockerSingleAutoRocksdb(t *testing.T) {
	testMatch(t, testModeDocker, starterModeSingle, false)
	if os.Getenv("IP") == "" {
		t.Fatal("IP envvar must be set to IP address of this machine")
	}
	/*
		docker volume create arangodb1
		docker run -i --name=adb1 --rm -p 8528:8528 \
			-v arangodb1:/data \
			-v /var/run/docker.sock:/var/run/docker.sock \
			arangodb/arangodb-starter \
			--starter.address=$IP \
			--starter.mode=single \
			--server.storage-engine=rocksdb \
			--docker.image=arangodb/arangodb:...
	*/
	volID := createDockerID("vol-starter-test-single-")
	createDockerVolume(t, volID)
	defer removeDockerVolume(t, volID)

	// Cleanup of left over tests
	removeDockerContainersByLabel(t, "starter-test=true")
	removeStarterCreatedDockerContainers(t)

	start := time.Now()

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
		"--starter.address=$IP",
		"--starter.mode=single",
		"--server.storage-engine=rocksdb",
		createEnvironmentStarterOptions(),
	}, " "))
	defer dockerRun.Close()
	defer removeDockerContainer(t, cID)

	if ok := WaitUntilStarterReady(t, whatSingle, 1, dockerRun); ok {
		t.Logf("Single server start took %s", time.Since(start))
		testSingle(t, insecureStarterEndpoint(0*portIncrement), false)
	}

	waitForCallFunction(t,
		ShutdownStarterCall(insecureStarterEndpoint(0*portIncrement)))
}

// TestOldDockerSingleAutoKeyFile runs the arangodb starter in docker with `--mode=single` && `--sslAutoKeyFile`
func TestOldDockerSingleAutoKeyFile(t *testing.T) {
	testMatch(t, testModeDocker, starterModeSingle, false)
	if os.Getenv("IP") == "" {
		t.Fatal("IP envvar must be set to IP address of this machine")
	}
	/*
		docker volume create arangodb1
		docker run -i --name=adb1 --rm -p 8528:8528 \
			-v arangodb1:/data \
			-v /var/run/docker.sock:/var/run/docker.sock \
			arangodb/arangodb-starter \
			--dockerContainer=adb1 --ownAddress=$IP \
			--mode=single --sslAutoKeyFile
	*/
	volID := createDockerID("vol-starter-test-single-")
	createDockerVolume(t, volID)
	defer removeDockerVolume(t, volID)

	// Cleanup of left over tests
	removeDockerContainersByLabel(t, "starter-test=true")
	removeStarterCreatedDockerContainers(t)

	start := time.Now()

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
		"--dockerContainer=" + cID,
		"--ownAddress=$IP",
		"--mode=single",
		"--sslAutoKeyFile",
		createEnvironmentStarterOptions(),
	}, " "))
	defer dockerRun.Close()
	defer removeDockerContainer(t, cID)

	if ok := WaitUntilStarterReady(t, whatSingle, 1, dockerRun); ok {
		t.Logf("Single server start took %s", time.Since(start))
		testSingle(t, secureStarterEndpoint(0*portIncrement), true)
	}

	waitForCallFunction(t,
		ShutdownStarterCall(secureStarterEndpoint(0*portIncrement)))
}

// TestDockerSingleCheckPersistentOptions runs arangodb in single mode and
// checks that overriding a persistent option results in an error message
func TestDockerSingleCheckPersistentOptions(t *testing.T) {
	testMatch(t, testModeDocker, starterModeSingle, false)
	if os.Getenv("IP") == "" {
		t.Fatal("IP envvar must be set to IP address of this machine")
	}

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
			volID := createDockerID("vol-starter-test-single-")
			createDockerVolume(t, volID)
			defer removeDockerVolume(t, volID)
			absTestDataPath, err := filepath.Abs("test/testdata")
			require.NoError(t, err)
			mounts := map[string]string{
				"/test/testdata": absTestDataPath,
				"/data":          volID,
			}
			ep := insecureStarterEndpoint(0 * portIncrement)

			// first start
			cID := createDockerID("starter-test-single-")
			args := []string{
				"--starter.mode=single",
				"--starter.address=$IP",
				tc.oldOption,
				createEnvironmentStarterOptions(),
			}
			proc := runStarterInContainer(t, false, cID, basePort, mounts, args)
			defer proc.Close()
			defer removeDockerContainer(t, cID)
			if ok := WaitUntilStarterReady(t, whatSingle, 1, proc); ok {
				testSingle(t, ep, false)
			}
			err = proc.EnsureNoMatches(context.Background(), time.Second*3, re, t.Name())
			require.NoError(t, err)
			waitForCallFunction(t, ShutdownStarterCall(ep))

			// second start
			cID = createDockerID("starter-test-single-")
			args = []string{
				"--starter.mode=single",
				"--starter.address=$IP",
				tc.newOption,
				createEnvironmentStarterOptions(),
			}
			procNew := runStarterInContainer(t, false, cID, basePort, mounts, args)
			defer procNew.Close()
			defer removeDockerContainer(t, cID)
			if ok := WaitUntilStarterReady(t, whatSingle, 1, procNew); ok {
				testSingle(t, ep, false)
			}
			if tc.shouldHaveErr {
				err = procNew.ExpectTimeout(context.Background(), time.Second*3, re, t.Name())
			} else {
				err = procNew.EnsureNoMatches(context.Background(), time.Second*3, re, t.Name())
			}
			require.NoError(t, err)
			waitForCallFunction(t, ShutdownStarterCall(ep))
		})
	}
}
