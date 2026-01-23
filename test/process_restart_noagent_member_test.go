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
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestProcessRestartNoAgentMember(t *testing.T) {
	removeArangodProcesses(t)
	testMatch(t, testModeProcess, starterModeCluster, false)

	members := map[int]MembersConfig{
		6000:  {"node1", 6000, SetUniqueDataDir(t), BoolPtr(true), nil},
		7000:  {"node2", 7000, SetUniqueDataDir(t), BoolPtr(true), nil},
		8000:  {"node3", 8000, SetUniqueDataDir(t), BoolPtr(true), nil},
		9000:  {"node4", 9000, SetUniqueDataDir(t), BoolPtr(false), nil},
		10000: {"node5", 10000, SetUniqueDataDir(t), BoolPtr(false), nil},
	}

	joins := "localhost:6000,localhost:7000,localhost:8000"
	for k, m := range members {
		m.Process = spawnMemberProcess(t, m.Port, m.DataDir, joins, fmt.Sprintf("--cluster.start-agent=%v", *m.HasAgent))
		m.Process.label = fmt.Sprintf("node-%d", m.Port)
		members[k] = m
	}

	waitForCluster(t, members, time.Now())

	t.Logf("Verify setup.json after fresh start")
	verifyProcessSetupJson(t, members, 3)

	t.Run("Restart slave4 (10000)", func(t *testing.T) {
		require.NoError(t, members[10000].Process.Kill())
		// Give process time to fully terminate and port to be released
		time.Sleep(5 * time.Second)

		m := members[10000]
		m.Process = spawnMemberProcess(t, m.Port, m.DataDir, joins, fmt.Sprintf("--cluster.start-agent=%v", *m.HasAgent))
		m.Process.label = fmt.Sprintf("node-%d", m.Port)
		members[10000] = m
		waitForCluster(t, members, time.Now())

		// Give cluster time to stabilize after restart
		time.Sleep(2 * time.Second)

		t.Logf("Verify setup.json after member restart")
		verifyProcessSetupJson(t, members, 3)
	})

	waitForCallFunction(t, getShutdownCalls(members)...)
}

func TestProcessMultipleRestartNoAgentMember(t *testing.T) {
	removeArangodProcesses(t)
	testMatch(t, testModeProcess, starterModeCluster, false)

	members := map[int]MembersConfig{
		6000:  {"node1", 6000, SetUniqueDataDir(t), BoolPtr(true), nil},
		7000:  {"node2", 7000, SetUniqueDataDir(t), BoolPtr(true), nil},
		8000:  {"node3", 8000, SetUniqueDataDir(t), BoolPtr(true), nil},
		9000:  {"node4", 9000, SetUniqueDataDir(t), BoolPtr(false), nil},
		10000: {"node5", 10000, SetUniqueDataDir(t), BoolPtr(false), nil},
	}

	joins := "localhost:6000,localhost:7000,localhost:8000"
	for port, m := range members {
		m.Process = spawnMemberProcess(t, m.Port, m.DataDir, joins, fmt.Sprintf("--cluster.start-agent=%v", *m.HasAgent))
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
				m.Process = spawnMemberProcess(t, m.Port, m.DataDir, joins, fmt.Sprintf("--cluster.start-agent=%v", *m.HasAgent))
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
