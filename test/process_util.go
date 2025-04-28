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
	"fmt"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/arangodb-helper/arangodb/service"
)

func removeArangodProcesses(t *testing.T) {
	t.Log("Removing arangod processes")
	listArangodProcesses(t, GetLogger(t))
	c := SpawnWithExpand(t, "sh -c 'PIDS=$(pidof arangod); if [ ! -z \"${PIDS}\" ]; then kill -9 ${PIDS}; fi'", false)
	defer c.Close()
	err := c.Wait()
	if err != nil {
		t.Logf("Failed to kill arangod processes: %v", err)
	} else {
		t.Log("Successfully killed arangod processes")
	}
}

func closeProcess(t *testing.T, s *SubProcess, name string) {
	s.Close()

	showProcessLogs(t, s, name)
}

func listArangodProcesses(t *testing.T, log Logger) {
	c := SpawnWithExpand(t, "pidof arangod", false)
	defer c.Close()
	c.Wait()

	logProcessOutput(log, c, "Processes: ")
}

func showProcessLogs(t *testing.T, s *SubProcess, name string) {
	if !t.Failed() {
		return
	}

	log := GetLogger(t)

	logProcessOutput(log, s, "Log of process: %s", name)
}

func spawnMemberProcess(t *testing.T, port int, dataDir, joins, extraArgs string) *SubProcess {
	return Spawn(t, strings.Join([]string{
		fmt.Sprintf("${STARTER} --starter.port=%d", port),
		fmt.Sprintf("--starter.data-dir=%s", dataDir),
		fmt.Sprintf("--starter.join=%s", joins),
		createEnvironmentStarterOptions(),
		extraArgs,
	}, " "))

}

func verifyProcessSetupJson(t *testing.T, members map[int]MembersConfig, expectedAgents int) {
	for _, m := range members {
		cfg, isRelaunch, err := service.ReadSetupConfig(zerolog.New(zerolog.NewConsoleWriter()), m.DataDir)
		require.NoError(t, err, "Failed to read setup.json, member: %d", m.Port)
		require.True(t, isRelaunch, "Expected relaunch, member: %d", m.Port)

		t.Logf("Verify setup.json for member: %d, %s", m.Port, m.DataDir)
		verifySetupJsonForMember(t, members, expectedAgents, cfg, m)
	}
}
