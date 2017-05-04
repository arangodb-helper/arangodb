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

// +build docker

package test

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestDockerLocalCluster runs `arangodb --docker=... --local`
func TestDockerLocalCluster(t *testing.T) {
	if os.Getenv("IP") == "" {
		t.Fatal("IP envvar must be set to IP address of this machine")
	}
	/*
		docker volume create arangodb1
		docker run -it --name=adb1 --rm -p 4000:4000 \
			-v arangodb1:/data \
			-v /var/run/docker.sock:/var/run/docker.sock \
			arangodb/arangodb-starter \
			--dockerContainer=adb1 --ownAddress=$IP
	*/
	volID := createDockerID("vol-starter-test-local-cluster-")
	createDockerVolume(t, volID)
	defer removeDockerVolume(t, volID)

	cID := createDockerID("starter-test-local-cluster-")
	dockerRun, err := Spawn(strings.Join([]string{
		"docker run -it",
		"--name=" + cID,
		"--rm -p 4000:4000",
		fmt.Sprintf("-v %s:/data", volID),
		"-v /var/run/docker.sock:/var/run/docker.sock",
		"arangodb/arangodb-starter",
		"--dockerContainer=" + cID,
		"--ownAddress=$IP",
		"--local",
	}, " "))
	if err != nil {
		t.Fatalf("Failed to run docker container: %s", describe(err))
	}
	defer dockerRun.Close()
	defer removeDockerContainer(t, cID)

	fmt.Println("Waiting for cluster ready")
	if ok := WaitUntilStarterReady(t, dockerRun); ok {
		testCluster(t, "http://localhost:4000")
	}

	fmt.Println("Waiting for termination")
	stopDockerContainer(t, cID)
}
