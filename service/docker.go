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

package service

import (
	"fmt"
	"strconv"
	"strings"

	docker "github.com/fsouza/go-dockerclient"
)

// findDockerExposedAddress looks up the external port number to which the given
// port is mapped onto for the given container.
func findDockerExposedAddress(dockerEndpoint, containerName string, port int) (hostPort int, isNetHost bool, networkMode string, hasTTY bool, err error) {
	client, err := docker.NewClient(dockerEndpoint)
	if err != nil {
		return 0, false, "", false, maskAny(err)
	}
	container, err := client.InspectContainer(containerName)
	if err != nil {
		return 0, false, "", false, maskAny(err)
	}
	hasTTY = container.Config.Tty
	networkMode = container.HostConfig.NetworkMode
	isNetHost = networkMode == "host" || strings.HasPrefix(networkMode, "container:")
	if isNetHost {
		// There is no port mapping for `--net=host`
		return port, isNetHost, networkMode, hasTTY, nil
	}
	dockerPort := docker.Port(fmt.Sprintf("%d/tcp", port))
	bindings, ok := container.NetworkSettings.Ports[dockerPort]
	if !ok || len(bindings) == 0 {
		return 0, isNetHost, networkMode, hasTTY, maskAny(fmt.Errorf("Cannot find port binding for TCP port %d", port))
	}
	hostPort, err = strconv.Atoi(bindings[0].HostPort)
	if err != nil {
		return 0, isNetHost, networkMode, hasTTY, maskAny(err)
	}
	return hostPort, isNetHost, networkMode, hasTTY, nil
}
