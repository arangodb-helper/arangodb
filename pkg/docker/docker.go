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

package docker

import (
	"fmt"
	"os"
	"strings"

	docker "github.com/fsouza/go-dockerclient"
	"github.com/pkg/errors"
)

const (
	cgroupDockerMarker   = ":/docker/"
	cgroupDockerCEMarker = ":/docker-ce/docker/"
)

type ContainerInfo struct {
	Name      string
	ImageName string
}

// IsRunningInDocker checks if the process is running in a docker container.
func IsRunningInDocker() bool {
	if os.Getenv("RUNNING_IN_DOCKER") != "true" {
		return false
	}
	if _, err := os.Stat("/.dockerenv"); os.IsNotExist(err) {
		return false
	}
	return true
}

// FindDockerContainerInfo find information (name or if not possible the ID, image-name) of the container that is used to run this process.
func FindDockerContainerInfo(dockerEndpoint string) (ContainerInfo, error) {
	id, err := findRunningContainerID()
	if err != nil {
		// Determining the container ID in unified-hierarchy mode is impossible.
		// Falling back to hostname if available
		id = os.Getenv("HOSTNAME")
		if !IsCGroup2UnifiedMode() || id == "" {
			return ContainerInfo{}, errors.WithMessagef(err, "hostname env val %s", id)
		}
	}
	info := ContainerInfo{Name: id}

	// Find name for container with ID.
	client, err := docker.NewClient(dockerEndpoint)
	if err != nil {
		return info, nil
	}
	container, err := client.InspectContainer(id)
	if err != nil {
		return info, nil
	}

	info.Name = container.ID
	if container.Name != "" {
		info.Name = container.Name
	}
	info.ImageName = container.Image

	// Try to inspect image for more naming info
	image, err := client.InspectImage(container.Image)
	if err == nil && len(image.RepoTags) > 0 {
		info.ImageName = image.RepoTags[0]
	}

	return info, nil
}

func findRunningContainerID() (string, error) {
	raw, err := os.ReadFile("/proc/self/cgroup")
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
	return "", errors.WithStack(fmt.Errorf("cannot find docker marker"))
}
