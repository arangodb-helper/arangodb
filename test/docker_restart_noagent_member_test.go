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

// we have a reproducible test case where we start a cluster with 3 agents and 2 non-agents and race conditions
func TestDockerMultipleRestartNoAgentMember(t *testing.T) {
	testMatch(t, testModeDocker, starterModeCluster, false)

	// NOTE: Raise the iteration count to 10 to reproduce the issue
	for i := 0; i < 1; i++ {
		// Cleanup previous tests
		removeDockerContainersByLabel(t, "starter-test=true")
		removeStarterCreatedDockerContainers(t)
		removeDockerVolumesByLabel(t, "starter-test=true")

		members := map[int]MembersConfig{
			6000:  {createDockerID("s6000-"), 6000, SetUniqueDataDir(t), BoolPtr(true), nil},
			7000:  {createDockerID("s7000-"), 7000, SetUniqueDataDir(t), BoolPtr(true), nil},
			8000:  {createDockerID("s8000-"), 8000, SetUniqueDataDir(t), BoolPtr(true), nil},
			9000:  {createDockerID("s9000-"), 9000, SetUniqueDataDir(t), BoolPtr(false), nil},
			10000: {createDockerID("s10000-"), 10000, SetUniqueDataDir(t), BoolPtr(false), nil},
		}

		joins := "localhost:6000,localhost:7000,localhost:8000"

		for k, m := range members {
			createDockerVolume(t, m.ID)

			m.Process = spawnMemberInDocker(t, m.Port, m.ID, joins, fmt.Sprintf("--cluster.start-agent=%v", *m.HasAgent))
			members[k] = m
		}

		waitForCluster(t, members, time.Now())

		t.Logf("Verify setup.json after fresh start, iteration: %d", i)
		verifyDockerSetupJson(t, members, 3)
		verifyEndpointSetup(t, members)

		for j := 0; j < 1; j++ {
			t.Run("Restart all members", func(t *testing.T) {
				for k := range members {
					require.NoError(t, members[k].Process.Kill())
				}

				time.Sleep(3 * time.Second)

				for k := range members {
					removeDockerContainer(t, members[k].ID)
				}

				for k, m := range members {
					m.Process = spawnMemberInDocker(t, m.Port, m.ID, joins, fmt.Sprintf("--cluster.start-agent=%v", *m.HasAgent))
					members[k] = m
				}

				waitForCluster(t, members, time.Now())

				t.Logf("Verify setup member restart")
				verifyDockerSetupJson(t, members, 3)
				verifyEndpointSetup(t, members)
			})
		}

		waitForCallFunction(t, getShutdownCalls(members)...)
		removeDockerVolumesByLabel(t, "starter-test=true")
	}
}

func TestDockerRestartNoAgentMember(t *testing.T) {
	testMatch(t, testModeDocker, starterModeCluster, false)

	// Cleanup previous tests
	removeDockerContainersByLabel(t, "starter-test=true")
	removeStarterCreatedDockerContainers(t)
	removeDockerVolumesByLabel(t, "starter-test=true")

	members := map[int]MembersConfig{
		6000:  {createDockerID("s6000-"), 6000, SetUniqueDataDir(t), BoolPtr(true), nil},
		7000:  {createDockerID("s7000-"), 7000, SetUniqueDataDir(t), BoolPtr(true), nil},
		8000:  {createDockerID("s8000-"), 8000, SetUniqueDataDir(t), BoolPtr(true), nil},
		9000:  {createDockerID("s9000-"), 9000, SetUniqueDataDir(t), BoolPtr(false), nil},
		10000: {createDockerID("s10000-"), 10000, SetUniqueDataDir(t), BoolPtr(false), nil},
	}

	joins := "localhost:6000,localhost:7000,localhost:8000"

	for k, m := range members {
		createDockerVolume(t, m.ID)

		m.Process = spawnMemberInDocker(t, m.Port, m.ID, joins, fmt.Sprintf("--cluster.start-agent=%v", *m.HasAgent))
		members[k] = m
	}

	waitForCluster(t, members, time.Now())

	t.Logf("Verify setup.json after fresh start")
	verifyDockerSetupJson(t, members, 3)

	t.Run("Restart s9000", func(t *testing.T) {
		require.NoError(t, members[9000].Process.Kill())
		time.Sleep(3 * time.Second)

		removeDockerContainer(t, members[9000].ID)

		m := members[9000]
		m.Process = spawnMemberInDocker(t, m.Port, m.ID, joins, fmt.Sprintf("--cluster.start-agent=%v", *m.HasAgent))
		members[9000] = m
		waitForCluster(t, members, time.Now())

		t.Logf("Verify setup.json after member restart")
		verifyDockerSetupJson(t, members, 3)
	})

	waitForCallFunction(t, getShutdownCalls(members)...)
	removeDockerVolumesByLabel(t, "starter-test=true")
}
