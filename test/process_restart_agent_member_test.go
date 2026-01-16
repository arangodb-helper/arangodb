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
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestDockerMultipleRestartAgentMember tests the default case of starting a cluster with 5 members and restarting all of them
// In that case only 3 agents are started and 2 non-agents
func TestProcessAgentsMultipleRestart(t *testing.T) {
	removeArangodProcesses(t)
	testMatch(t, testModeProcess, starterModeCluster, false)

	members := map[int]MembersConfig{
		6000:  {"node1", 6000, SetUniqueDataDir(t), nil, nil},
		7000:  {"node2", 7000, SetUniqueDataDir(t), nil, nil},
		8000:  {"node3", 8000, SetUniqueDataDir(t), nil, nil},
		9000:  {"node4", 9000, SetUniqueDataDir(t), nil, nil},
		10000: {"node5", 10000, SetUniqueDataDir(t), nil, nil},
	}

	joins := "127.0.0.1:6000,127.0.0.1:7000,127.0.0.1:8000"
	for port, m := range members {
		m.Process = spawnMemberProcess(t, m.Port, m.DataDir, joins, "")
		members[port] = m
	}

	waitForCluster(t, members, time.Now())

	t.Logf("Verify setup.json after fresh start")
	verifyProcessSetupJson(t, members, 3)
	verifyEndpointSetup(t, members)

	for i := 0; i < 1; i++ {
		t.Logf("Restart all members, iteration: %d", i)
		t.Run("Restart all members", func(t *testing.T) {
			for port := range members {
				require.NoError(t, members[port].Process.Kill())
			}
			// Give processes time to fully terminate and ports to be released
			time.Sleep(5 * time.Second)

			for port, m := range members {
				m.Process = spawnMemberProcess(t, m.Port, m.DataDir, joins, "")
				members[port] = m
			}

			waitForCluster(t, members, time.Now())

			// Give cluster time to stabilize after restart (agents need to start, master election needs to complete)
			time.Sleep(2 * time.Second)

			t.Logf("Verify setup after member restart, iteration: %d", i)
			verifyProcessSetupJson(t, members, 3)
			verifyEndpointSetup(t, members)
		})
	}

	waitForCallFunction(t, getShutdownCalls(members)...)
}
