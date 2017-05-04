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

// TestDockerClusterDefault runs 3 arangodb starters in docker with default settings.
func TestDockerClusterDefault(t *testing.T) {
	if os.Getenv("IP") == "" {
		t.Fatal("IP envvar must be set to IP address of this machine")
	}
	/*
		docker volume create arangodb1
		docker run -it --name=adb1 --rm -p 4000:4000 \
			-v arangodb1:/data \
			-v /var/run/docker.sock:/var/run/docker.sock \
			arangodb/arangodb-starter \
			--dockerContainer=adb1 --ownAddress=$IP \
			--local
	*/
	volID1 := createDockerID("vol-starter-test-cluster-default1-")
	createDockerVolume(t, volID1)
	defer removeDockerVolume(t, volID1)

	volID2 := createDockerID("vol-starter-test-cluster-default2-")
	createDockerVolume(t, volID2)
	defer removeDockerVolume(t, volID2)

	volID3 := createDockerID("vol-starter-test-cluster-default3-")
	createDockerVolume(t, volID3)
	defer removeDockerVolume(t, volID3)

	// Cleanup of left over tests
	removeDockerContainersByLabel(t, "starter-test=true")

	cID1 := createDockerID("starter-test-cluster-default1-")
	dockerRun1, err := Spawn(strings.Join([]string{
		"docker run -it",
		"--label starter-test=true",
		"--name=" + cID1,
		"--rm -p 4000:4000",
		fmt.Sprintf("-v %s:/data", volID1),
		"-v /var/run/docker.sock:/var/run/docker.sock",
		"arangodb/arangodb-starter",
		"--dockerContainer=" + cID1,
		"--ownAddress=$IP",
	}, " "))
	if err != nil {
		t.Fatalf("Failed to run docker container: %s", describe(err))
	}
	defer dockerRun1.Close()
	defer removeDockerContainer(t, cID1)

	cID2 := createDockerID("starter-test-cluster-default2-")
	dockerRun2, err := Spawn(strings.Join([]string{
		"docker run -it",
		"--label starter-test=true",
		"--name=" + cID2,
		"--rm -p 4005:4000",
		fmt.Sprintf("-v %s:/data", volID2),
		"-v /var/run/docker.sock:/var/run/docker.sock",
		"arangodb/arangodb-starter",
		"--dockerContainer=" + cID2,
		"--ownAddress=$IP",
		"--join=$IP:4000",
	}, " "))
	if err != nil {
		t.Fatalf("Failed to run docker container: %s", describe(err))
	}
	defer dockerRun2.Close()
	defer removeDockerContainer(t, cID2)

	cID3 := createDockerID("starter-test-cluster-default3-")
	dockerRun3, err := Spawn(strings.Join([]string{
		"docker run -it",
		"--label starter-test=true",
		"--name=" + cID3,
		"--rm -p 4010:4000",
		fmt.Sprintf("-v %s:/data", volID3),
		"-v /var/run/docker.sock:/var/run/docker.sock",
		"arangodb/arangodb-starter",
		"--dockerContainer=" + cID3,
		"--ownAddress=$IP",
		"--join=$IP:4000",
	}, " "))
	if err != nil {
		t.Fatalf("Failed to run docker container: %s", describe(err))
	}
	defer dockerRun3.Close()
	defer removeDockerContainer(t, cID3)

	fmt.Println("Waiting for cluster ready")
	if ok := WaitUntilStarterReady(t, dockerRun1, dockerRun2, dockerRun3); ok {
		testCluster(t, "http://localhost:4000")
		testCluster(t, "http://localhost:4005")
		testCluster(t, "http://localhost:4010")
	}

	fmt.Println("Waiting for termination")
	stopDockerContainer(t, cID1)
	stopDockerContainer(t, cID2)
	stopDockerContainer(t, cID3)
}
