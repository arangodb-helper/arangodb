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
	"testing"
)

func createDockerVolume(t *testing.T, id string) {
	c, err := Spawn(fmt.Sprintf("docker volume create %s", id))
	if err != nil {
		t.Fatalf("Failed to create docker volume: %s", describe(err))
	} else {
		defer c.Close()
		c.Wait()
	}
}

func removeDockerVolume(t *testing.T, id string) {
	c, err := Spawn(fmt.Sprintf("docker volume rm -f %s", id))
	if err != nil {
		t.Errorf("Failed to remove docker volume: %s", describe(err))
	} else {
		defer c.Close()
		c.Wait()
	}
}

func removeDockerContainer(t *testing.T, id string) {
	c, err := Spawn(fmt.Sprintf("docker rm -f -v %s", id))
	if err != nil {
		t.Errorf("Failed to remove docker container: %s", describe(err))
	} else {
		defer c.Close()
		c.Wait()
	}
}

func stopDockerContainer(t *testing.T, id string) {
	c, err := Spawn(fmt.Sprintf("docker stop --time=120 %s", id))
	if err != nil {
		t.Errorf("Failed to stop docker container: %s", describe(err))
	} else {
		defer c.Close()
		c.Wait()
	}
}

func createDockerID(prefix string) string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return prefix + hex.EncodeToString(b)
}
