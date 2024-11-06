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
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/arangodb-helper/arangodb/service"
)

// createDockerVolumes creates docker volume for each provided prefix and returns the list of volume ids and cleanup function
func createDockerVolumes(t *testing.T, prefixes ...string) ([]string, func()) {
	ids := make([]string, 0, len(prefixes))
	for _, prefix := range prefixes {
		id := createDockerID(prefix)
		ids = append(ids, id)
		createDockerVolume(t, id)
	}
	require.Len(t, ids, len(prefixes))
	return ids, func() {
		for _, id := range ids {
			removeDockerVolume(t, id)
		}
	}
}

func createDockerVolume(t *testing.T, id string) {
	c := Spawn(t, fmt.Sprintf("docker volume create %s --label starter-test=true", id))
	defer c.Close()
	c.Wait()
}

func removeDockerVolumesByLabel(t *testing.T, labelKeyValue string) {
	ps := exec.Command("docker", "volume", "ls", "-q", "--filter", "label="+labelKeyValue)
	list, err := ps.Output()
	if err != nil {
		t.Fatalf("docker ps failed: %s", describe(err))
	}
	ids := strings.Split(strings.TrimSpace(string(list)), "\n")
	for _, id := range ids {
		if id := strings.TrimSpace(id); id != "" {
			removeDockerVolume(t, id)
		}
	}
}

func removeDockerVolume(t *testing.T, id string) {
	c := Spawn(t, fmt.Sprintf("docker volume rm -f %s", id))
	defer c.Close()
	c.Wait()
}

func removeDockerContainer(t *testing.T, id string) {
	if t.Failed() {
		logDockerPS(t)
		logDockerLogs(t, id)
	}

	c := Spawn(t, fmt.Sprintf("docker rm -f -v %s", id))
	defer c.Close()
	c.Wait()
}

func logDockerPS(t *testing.T) {
	log := GetLogger(t)

	// Dump of logs if failed
	c := Spawn(t, fmt.Sprintf("docker ps -a"))
	defer c.Close()

	time.Sleep(500 * time.Millisecond)

	c.Wait()

	logProcessOutput(log, c, "List of containers: ")
}

func logDockerLogs(t *testing.T, id string) {
	log := GetLogger(t)

	// Dump of logs if failed
	c := Spawn(t, fmt.Sprintf("docker logs --timestamps %s", id))
	defer c.Close()
	c.Wait()

	logProcessOutput(log, c, "Log of container %s: ", id)
}

func removeDockerContainersByLabel(t *testing.T, labelKeyValue string) {
	ps := exec.Command("docker", "ps", "-q", "--filter", "label="+labelKeyValue)
	list, err := ps.Output()
	if err != nil {
		t.Fatalf("docker ps failed: %s", describe(err))
	}
	ids := strings.Split(strings.TrimSpace(string(list)), "\n")
	for _, id := range ids {
		if id := strings.TrimSpace(id); id != "" {
			removeDockerContainer(t, id)
		}
	}
}

func removeStarterCreatedDockerContainers(t *testing.T) {
	removeDockerContainersByLabel(t, "created-by=arangodb-starter")
}

func createDockerID(prefix string) string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return prefix + hex.EncodeToString(b)
}

func spawnMemberInDocker(t *testing.T, port int, cID, joinArg, extraArgs, dockerArgs string) *SubProcess {
	if joinArg != "" {
		joinArg = fmt.Sprintf("--starter.join=%s", joinArg)
	}

	return Spawn(t, strings.Join([]string{
		"docker run -i --net=host",
		"--label starter-test=true",
		"--name=" + cID,
		"--rm",
		createLicenseKeyOption(),
		fmt.Sprintf("-v %s:/data", cID),
		"-v /var/run/docker.sock:/var/run/docker.sock",
		dockerArgs,
		"arangodb/arangodb-starter",
		"--docker.container=" + cID,
		"--starter.address=localhost",
		fmt.Sprintf("--starter.port=%d", port),
		createEnvironmentStarterOptions(),
		joinArg,
		extraArgs,
	}, " "))
}

func verifyDockerSetupJson(t *testing.T, members map[int]MembersConfig, expectedAgents int) {
	for _, m := range members {
		c := Spawn(t, fmt.Sprintf("docker exec %s cat /data/setup.json", m.ID))
		require.NoError(t, c.Wait())
		require.NoError(t, c.Close())

		cfg, isRelaunch, err := service.VerifySetupConfig(zerolog.New(zerolog.NewConsoleWriter()), c.Output())
		require.NoError(t, err, "Failed to read setup.json, member: %d", m.Port)
		require.True(t, isRelaunch, "Expected relaunch, member: %d", m.Port)

		t.Logf("Verify setup.json for member: %d, %s", m.Port, m.DataDir)
		verifySetupJsonForMember(t, members, expectedAgents, cfg, m)
	}
}
