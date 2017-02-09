package service

import (
	"fmt"
	"strconv"

	docker "github.com/fsouza/go-dockerclient"
)

// findDockerExposedAddress looks up the external port number to which the given
// port is mapped onto for the given container.
func findDockerExposedAddress(dockerEndpoint, containerName string, port int) (hostPort int, isNetHost bool, err error) {
	client, err := docker.NewClient(dockerEndpoint)
	if err != nil {
		return 0, false, maskAny(err)
	}
	container, err := client.InspectContainer(containerName)
	if err != nil {
		return 0, false, maskAny(err)
	}
	isNetHost = container.HostConfig.NetworkMode == "host"
	if isNetHost {
		// There is no port mapping for `--net=host`
		return port, isNetHost, nil
	}
	dockerPort := docker.Port(fmt.Sprintf("%d/tcp", port))
	bindings, ok := container.NetworkSettings.Ports[dockerPort]
	if !ok || len(bindings) == 0 {
		return 0, isNetHost, maskAny(fmt.Errorf("Cannot find port binding for TCP port %d", port))
	}
	hostPort, err = strconv.Atoi(bindings[0].HostPort)
	if err != nil {
		return 0, isNetHost, maskAny(err)
	}
	return hostPort, isNetHost, nil
}
