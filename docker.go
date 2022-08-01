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

package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	docker "github.com/fsouza/go-dockerclient"
	"github.com/pkg/errors"
)

const (
	cgroupDockerMarker   = ":/docker/"
	cgroupDockerCEMarker = ":/docker-ce/docker/"
)

type containerInfo struct {
	Name      string
	ImageName string
}

// isRunningInDocker checks if the process is running in a docker container.
func isRunningInDocker() bool {
	if os.Getenv("RUNNING_IN_DOCKER") != "true" {
		return false
	}
	if _, err := os.Stat("/.dockerenv"); os.IsNotExist(err) {
		return false
	}
	return true
}

// findDockerContainerInfo find information (name or if not possible the ID, image-name) of the container that is used to run this process.
func findDockerContainerInfo(dockerEndpoint string) (containerInfo, error) {
	findID := func() (string, error) {
		raw, err := ioutil.ReadFile("/proc/self/cgroup")
		if err != nil {
			return "", errors.WithStack(err)
		}
		lines := strings.Split(string(raw), "\n")
		for _, line := range lines {

			if i := strings.Index(line, cgroupDockerCEMarker); i > 0 {
				id := strings.TrimSpace(line[i+len(cgroupDockerCEMarker):])
				if id != "" {
					return id, nil
				}
			} else if i := strings.Index(line, cgroupDockerMarker); i > 0 {
				id := strings.TrimSpace(line[i+len(cgroupDockerMarker):])
				if id != "" {
					return id, nil
				}
			}
		}
		return "", errors.WithStack(fmt.Errorf("Cannot find docker marker"))
	}

	id, err := findID()
	if err != nil {
		return containerInfo{}, errors.WithStack(err)
	}
	info := containerInfo{Name: id}

	// Find name for container with ID.
	client, err := docker.NewClient(dockerEndpoint)
	if err != nil {
		return info, nil // fallback to ID
	}
	container, err := client.InspectContainer(id)
	if err != nil {
		return info, nil // fallback to ID
	}

	if name := container.Name; name != "" {
		info.Name = name
	}
	info.ImageName = container.Image

	// Try to inspect image for more naming info
	image, err := client.InspectImage(container.Image)
	if err == nil && len(image.RepoTags) > 0 {
		info.ImageName = image.RepoTags[0]
	}

	return info, nil
}
