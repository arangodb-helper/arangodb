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
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestProcessErrExitCodeHandler runs in 'single' mode with invalid ArangoD configuration and
// checks that exit code is recognized by starter
func TestProcessErrExitCodeHandler(t *testing.T) {
	removeArangodProcesses(t)
	testMatch(t, testModeProcess, starterModeSingle, false)
	dataDir := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDir)

	child := Spawn(t, "${STARTER} --starter.mode=single --args.all.config=invalidvalue "+createEnvironmentStarterOptions())
	defer child.Close()

	re := regexp.MustCompile("has failed 1 times, giving up")
	require.NoError(t, child.ExpectTimeout(context.Background(), time.Second*20, re, ""))

	if isVerbose {
		t.Log("Waiting for termination")
	}
	require.NoError(t, child.WaitTimeout(time.Second*10), "Starter is not stopped in time")
}
