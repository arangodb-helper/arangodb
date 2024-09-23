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
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/arangodb-helper/arangodb/service"
)

// we have a reproducible test case where we start a cluster with 3 agents and 2 non-agents and race conditions
func TestDockerMultipleRestartNoAgentMember(t *testing.T) {
	testMatch(t, testModeDocker, starterModeCluster, false)
	hostIP := os.Getenv("IP")

	if hostIP == "" {
		t.Fatal("IP envvar must be set to IP address of this machine")
	}

	// NOTE: Raise the iteration count to 10 to reproduce the issue
	for i := 0; i < 1; i++ {
		// Cleanup previous tests
		removeDockerContainersByLabel(t, "starter-test=true")
		removeStarterCreatedDockerContainers(t)
		removeDockerVolumesByLabel(t, "starter-test=true")

		members := map[int]MembersConfig{
			6000: {createDockerID("s6000-"), 6000, SetUniqueDataDir(t), true, nil},
			6100: {createDockerID("s6100-"), 6100, SetUniqueDataDir(t), true, nil},
			6200: {createDockerID("s6200-"), 6200, SetUniqueDataDir(t), true, nil},
			6300: {createDockerID("s6300-"), 6300, SetUniqueDataDir(t), false, nil},
			6400: {createDockerID("s6400-"), 6400, SetUniqueDataDir(t), false, nil},
		}

		joins := "$IP:6000,$IP:6100,$IP:6200"

		for k, m := range members {
			createDockerVolume(t, m.ID)

			m.Process = spawnMemberInDocker(t, m.Port, m.ID, joins, fmt.Sprintf("--cluster.start-agent=%v", m.HasAgent))
			members[k] = m
		}

		waitForCluster(t, members, time.Now())

		t.Logf("Verify setup.json after fresh start, iteration: %d", i)
		verifySetupJsonInDocker(t, members)
		verifyEndpointSetup(t, members, hostIP)

		for j := 0; j < 0; j++ {
			t.Run("Restart all members", func(t *testing.T) {
				for k := range members {
					require.NoError(t, members[k].Process.Kill())
				}

				time.Sleep(3 * time.Second)

				for k := range members {
					logDockerLogs(t, members[k].ID)
					removeDockerContainer(t, members[k].ID)
				}

				for k, m := range members {
					m.Process = spawnMemberInDocker(t, m.Port, m.ID, joins, fmt.Sprintf("--cluster.start-agent=%v", m.HasAgent))
					members[k] = m
				}

				waitForCluster(t, members, time.Now())

				t.Logf("Verify setup member restart")
				verifySetupJsonInDocker(t, members)
				verifyEndpointSetup(t, members, hostIP)
			})
		}

		waitForCallFunction(t, getShutdownCalls(members)...)
		removeDockerVolumesByLabel(t, "starter-test=true")
	}
}

func TestDockerRestartNoAgentMember(t *testing.T) {
	testMatch(t, testModeDocker, starterModeCluster, false)
	if os.Getenv("IP") == "" {
		t.Fatal("IP envvar must be set to IP address of this machine")
	}

	// Cleanup previous tests
	removeDockerContainersByLabel(t, "starter-test=true")
	removeStarterCreatedDockerContainers(t)
	removeDockerVolumesByLabel(t, "starter-test=true")

	members := map[int]MembersConfig{
		6000: {createDockerID("s6000-"), 6000, SetUniqueDataDir(t), true, nil},
		6100: {createDockerID("s6100-"), 6100, SetUniqueDataDir(t), true, nil},
		6200: {createDockerID("s6200-"), 6200, SetUniqueDataDir(t), true, nil},
		6300: {createDockerID("s6300-"), 6300, SetUniqueDataDir(t), false, nil},
		6400: {createDockerID("s6400-"), 6400, SetUniqueDataDir(t), false, nil},
	}

	joins := "$IP:6000,$IP:6100,$IP:6200"

	for k, m := range members {
		createDockerVolume(t, m.ID)

		m.Process = spawnMemberInDocker(t, m.Port, m.ID, joins, fmt.Sprintf("--cluster.start-agent=%v", m.HasAgent))
		members[k] = m
	}

	waitForCluster(t, members, time.Now())

	t.Logf("Verify setup.json after fresh start")
	verifySetupJsonInDocker(t, members)

	t.Run("Restart slave4 (6400)", func(t *testing.T) {
		require.NoError(t, members[6400].Process.Kill())
		time.Sleep(3 * time.Second)

		removeDockerContainer(t, members[6400].ID)

		m := members[6400]
		m.Process = spawnMemberInDocker(t, m.Port, m.ID, joins, fmt.Sprintf("--cluster.start-agent=%v", m.HasAgent))
		members[6400] = m
		waitForCluster(t, members, time.Now())

		t.Logf("Verify setup.json after member restart")
		verifySetupJsonInDocker(t, members)
	})

	waitForCallFunction(t, getShutdownCalls(members)...)
	removeDockerVolumesByLabel(t, "starter-test=true")
}

func spawnMemberInDocker(t *testing.T, port int, cID, joins, extraArgs string) *SubProcess {
	return Spawn(t, strings.Join([]string{
		"docker run -i",
		"--label starter-test=true",
		"--name=" + cID,
		"--rm",
		createLicenseKeyOption(),
		fmt.Sprintf("-p %d:%d", port, port),
		fmt.Sprintf("-v %s:/data", cID),
		"-v /var/run/docker.sock:/var/run/docker.sock",
		"arangodb/arangodb-starter",
		"--docker.container=" + cID,
		"--starter.address=$IP",
		fmt.Sprintf("--starter.port=%d", port),
		createEnvironmentStarterOptions(),
		fmt.Sprintf("--starter.join=%s", joins),
		extraArgs,
	}, " "))
}

// verifySetupJsonInDocker validates setup.json file on all members
func verifySetupJsonInDocker(t *testing.T, members map[int]MembersConfig) {
	for _, m := range members {
		c := Spawn(t, fmt.Sprintf("docker exec %s cat /data/setup.json", m.ID))
		require.NoError(t, c.Wait())
		require.NoError(t, c.Close())

		cfg, isRelaunch, err := service.VerifySetupConfig(zerolog.New(zerolog.NewConsoleWriter()), c.Output())
		require.NoError(t, err, "Failed to read setup.json, member: %d", m.Port)
		require.True(t, isRelaunch, "Expected relaunch, member: %d", m.Port)

		for _, p := range cfg.Peers.AllPeers {
			mLocal, ok := members[p.Port]
			require.True(t, ok, "Member %d not found in members list", p.Port)

			assert.Equal(t, mLocal.HasAgent, p.HasAgent(), "HasAgent mismatch, memberConfig: %s, peerPort: %d", m.Port, p.Port)
		}
	}
}

func getShutdownCalls(members map[int]MembersConfig) []callFunction {
	var calls []callFunction
	for _, m := range members {
		calls = append(calls, ShutdownStarterCall(fmt.Sprintf("http://localhost:%d", m.Port)))
	}
	return calls
}
