package service

import (
	"fmt"
	"strconv"

	docker "github.com/fsouza/go-dockerclient"
)

// findDockerExposedAddress looks up the external address and port number to which the given
// port is mapped onto for the given container.
func findDockerExposedAddress(dockerSocket, containerName string, port int) (string, int, error) {
	endpoint := "unix:///var/run/docker.sock"
	client, err := docker.NewClient(endpoint)
	if err != nil {
		return "", 0, maskAny(err)
	}
	container, err := client.InspectContainer(containerName)
	if err != nil {
		return "", 0, maskAny(err)
	}
	dockerPort := docker.Port(fmt.Sprintf("%d/tcp", port))
	bindings, ok := container.NetworkSettings.Ports[dockerPort]
	if !ok || len(bindings) == 0 {
		return "", 0, maskAny(fmt.Errorf("Cannot find port binding for TCP port %d", port))
	}
	hostPort, err := strconv.Atoi(bindings[0].HostPort)
	if err != nil {
		return "", 0, maskAny(err)
	}
	return bindings[0].HostIP, hostPort, nil
}
