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
	"context"
	"os"
	"testing"
	"time"

	driver "github.com/arangodb/go-driver/v2/arangodb"
	driver_http "github.com/arangodb/go-driver/v2/connection"
	"github.com/stretchr/testify/require"

	"github.com/arangodb-helper/arangodb/client"
)

func TestProcessConfigFileLoading(t *testing.T) {
	testMatch(t, testModeProcess, starterModeCluster, false)

	t.Run("non-existing", func(t *testing.T) {
		dataDir := SetUniqueDataDir(t)
		defer os.RemoveAll(dataDir)

		child := Spawn(t, "${STARTER} --configuration=test/testdata/thisfiledoesnoexist.conf")
		defer child.Close()

		WaitUntilStarterExit(t, 15*time.Second, 1, child)
	})

	t.Run("invalid keys", func(t *testing.T) {
		dataDir := SetUniqueDataDir(t)
		defer os.RemoveAll(dataDir)

		child := Spawn(t, "${STARTER} --configuration=test/testdata/invalidkeys.conf")
		defer child.Close()

		WaitUntilStarterExit(t, 15*time.Second, 1, child)
	})

	t.Run("invalid values", func(t *testing.T) {
		dataDir := SetUniqueDataDir(t)
		defer os.RemoveAll(dataDir)

		child := Spawn(t, "${STARTER} --configuration=test/testdata/invalidvalues.conf")
		defer child.Close()

		WaitUntilStarterExit(t, 15*time.Second, 1, child)
	})

	t.Run("none", func(t *testing.T) {
		dataDir := SetUniqueDataDir(t)
		defer os.RemoveAll(dataDir)

		child := Spawn(t, "${STARTER} --configuration=noNe --starter.mode=single")
		defer child.Close()

		require.True(t, WaitUntilStarterReady(t, whatSingle, 1, child))
		SendIntrAndWait(t, child)
	})

	t.Run("plain", func(t *testing.T) {
		dataDir := SetUniqueDataDir(t)
		defer os.RemoveAll(dataDir)

		child := Spawn(t, "${STARTER} --configuration=test/testdata/single.conf")
		defer child.Close()

		require.True(t, WaitUntilStarterReady(t, whatSingle, 1, child))
		SendIntrAndWait(t, child)
	})

	t.Run("flag-override", func(t *testing.T) {
		dataDir := SetUniqueDataDir(t)
		defer os.RemoveAll(dataDir)

		child := Spawn(t, "${STARTER} --starter.mode=single --configuration=test/testdata/activefailover.conf")
		defer child.Close()

		require.True(t, WaitUntilStarterReady(t, whatSingle, 1, child))
		SendIntrAndWait(t, child)
	})

	t.Run("child-sections", func(t *testing.T) {
		dataDir := SetUniqueDataDir(t)
		defer os.RemoveAll(dataDir)

		child := Spawn(t, "${STARTER} --configuration=test/testdata/single-sections.conf")
		defer child.Close()

		require.True(t, WaitUntilStarterReady(t, whatSingle, 1, child))
		SendIntrAndWait(t, child)
	})

	t.Run("passthrough-options", func(t *testing.T) {
		// Skip this test if V8 JavaScript is not available (required for fetching config via JavaScript transaction)
		features := getSupportedDatabaseFeatures(t, testModeProcess)
		if !features.HasV8JavaScriptSupport() {
			t.Skip("Skipping passthrough-options test: V8 JavaScript is not available in this build")
		}

		dataDir := SetUniqueDataDir(t)
		defer os.RemoveAll(dataDir)

		child := Spawn(t, "${STARTER} --args.all.default-language=es_419 -c test/testdata/single-passthrough.conf")
		defer child.Close()

		require.True(t, WaitUntilStarterReady(t, whatSingle, 1, child))

		endpoint := insecureStarterEndpoint(0 * portIncrement)
		arangoDConf := fetchArangoDConfig(t, endpoint)

		defaultLanguage, ok := arangoDConf["default-language"]
		require.True(t, ok, "expected to find 'default-language' field")
		require.Equalf(t, "es_419", defaultLanguage, "passthrough option should be passed to instance, but flags has priority")

		rocksDBStats, ok := arangoDConf["rocksdb.enable-statistics"]
		require.True(t, ok, "expected to find 'rocksdb.enable-statistics' field")
		require.Equalf(t, true, rocksDBStats, "passthrough option should be passed to instance")

		logLevels, ok := arangoDConf["log.level"]
		require.True(t, ok, "expected to find 'log.level' field")
		expectedLogLevels := []string{"startup=trace", "queries=debug", "warning", "info"} // info level is added by starter automatically via arangod.conf
		require.ElementsMatchf(t, expectedLogLevels, logLevels, "passthrough options should be passed to instance")

		SendIntrAndWait(t, child)
	})
}

func fetchArangoDConfig(t *testing.T, endpoint string) map[string]interface{} {
	auth := driver_http.NewBasicAuth("root", "")
	coordinatorClient, err := CreateClient(t, endpoint, client.ServerTypeSingle, auth)
	require.NoError(t, err)

	WaitUntilServiceReadyAPI(t, coordinatorClient, ServiceReadyCheckVersion()).ExecuteT(t, 30*time.Second, 500*time.Millisecond)

	ctx := context.Background()
	db, err := coordinatorClient.GetDatabase(ctx, "_system", nil)
	require.NoError(t, err)

	jsAction := `
function() {
    return require("internal").options();
}`
	actionResult, err := db.TransactionJS(ctx, driver.TransactionJSOptions{
		Action:      jsAction,
		Collections: driver.TransactionCollections{},
	})
	require.NoError(t, err)

	optionsMap, ok := actionResult.(map[string]interface{})
	require.True(t, ok, "actionResult should be castable to map[string]interface{}")

	return optionsMap
}
