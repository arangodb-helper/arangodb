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
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func createDockerVolume(t *testing.T, id string) {
	c := Spawn(t, fmt.Sprintf("docker volume create %s", id))
	defer c.Close()
	c.Wait()
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
