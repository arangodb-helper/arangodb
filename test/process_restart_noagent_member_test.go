//
// DISCLAIMER
//
// Copyright 2024 ArangoDB GmbH, Cologne, Germany
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
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/arangodb-helper/arangodb/service"
)

type MembersConfig struct {
	ID       string
	Port     int
	DataDir  string
	HasAgent bool
	Process  *SubProcess
}

func TestProcessRestartNoAgentMember(t *testing.T) {
	removeArangodProcesses(t)
	testMatch(t, testModeProcess, starterModeCluster, false)

	members := map[int]MembersConfig{
		6000:  {"master", 6000, SetUniqueDataDir(t), true, nil},
		7000:  {"slave1", 7000, SetUniqueDataDir(t), true, nil},
		8000:  {"slave2", 8000, SetUniqueDataDir(t), true, nil},
		9000:  {"slave3", 9000, SetUniqueDataDir(t), false, nil},
		10000: {"slave4", 10000, SetUniqueDataDir(t), false, nil},
	}

	joins := "localhost:6000,localhost:7000,localhost:8000"
	for k, m := range members {
		m.DataDir = SetUniqueDataDir(t)
		m.Process = spawnMemberProcess(t, m.Port, m.DataDir, joins, fmt.Sprintf("--cluster.start-agent=%v", m.HasAgent))
		members[k] = m
	}

	waitForCluster(t, members, time.Now())

	t.Logf("Verify setup.json after fresh start")
	verifySetupJson(t, members)

	t.Run("Restart slave4 (10000)", func(t *testing.T) {
		require.NoError(t, members[10000].Process.Kill())
		time.Sleep(3 * time.Second)

		m := members[10000]
		m.Process = spawnMemberProcess(t, m.Port, m.DataDir, joins, fmt.Sprintf("--cluster.start-agent=%v", m.HasAgent))
		members[10000] = m
		waitForCluster(t, members, time.Now())

		t.Logf("Verify setup.json after member restart")
		verifySetupJson(t, members)
	})

	// TODO fix-me: GT-608
	//SendIntrAndWait(t, members[10000].Process, members[6000].Process, members[7000].Process, members[8000].Process, members[9000].Process)
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

func waitForCluster(t *testing.T, members map[int]MembersConfig, start time.Time) {
	var processes []*SubProcess
	for _, m := range members {
		processes = append(processes, m.Process)
	}

	require.True(t, WaitUntilStarterReady(t, whatCluster, len(processes), processes...))
	t.Logf("Cluster start took %s", time.Since(start))

	for _, m := range members {
		testCluster(t, fmt.Sprintf("http://localhost:%d", m.Port), false)
	}
}

// verifySetupJson validates setup.json file on all members
func verifySetupJson(t *testing.T, members map[int]MembersConfig) {
	for _, m := range members {
		cfg, isRelaunch, err := service.ReadSetupConfig(zerolog.New(zerolog.NewConsoleWriter()), m.DataDir)
		require.NoError(t, err, "Failed to read setup.json, member: %d", m.Port)
		require.True(t, isRelaunch, "Expected relaunch, member: %d", m.Port)

		for _, p := range cfg.Peers.AllPeers {
			mLocal, ok := members[p.Port]
			require.True(t, ok, "Member %d not found in members list", p.Port)

			require.Equal(t, mLocal.DataDir, p.DataDir, "DataDir mismatch, member: %d", m.Port)
			assert.Equal(t, mLocal.HasAgent, p.HasAgent(), "HasAgent mismatch, memberConfig: %d, peerPort: %d", m.Port, p.Port)
		}
	}
}
