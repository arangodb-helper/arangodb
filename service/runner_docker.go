package service

import (
	"fmt"
	"strings"

	docker "github.com/fsouza/go-dockerclient"
)

// NewDockerRunner creates a runner that starts processes on the local OS.
func NewDockerRunner(endpoint, image, user string) (Runner, error) {
	client, err := docker.NewClient(endpoint)
	if err != nil {
		return nil, maskAny(err)
	}
	return &dockerRunner{
		client: client,
		image:  image,
		user:   user,
	}, nil
}

// dockerRunner implements a Runner that starts processes in a docker container.
type dockerRunner struct {
	client *docker.Client
	image  string
	user   string
}

type dockerContainer struct {
	client    *docker.Client
	container *docker.Container
}

func (r *dockerRunner) GetContainerDir(hostDir string) string {
	return "/data"
}

func (r *dockerRunner) Start(command string, args []string, hostVolume, containerVolume, containerName string) (Process, error) {
	containerName = strings.Replace(containerName, ":", "", -1)
	opts := docker.CreateContainerOptions{
		Name: containerName,
		Config: &docker.Config{
			Image:      r.image,
			Entrypoint: []string{command},
			Cmd:        args,
			Tty:        true,
			User:       r.user,
		},
		HostConfig: &docker.HostConfig{
			NetworkMode: "host",
			Binds: []string{
				fmt.Sprintf("%s:%s", hostVolume, containerVolume),
			},
		},
	}
	c, err := r.client.CreateContainer(opts)
	if err != nil {
		return nil, maskAny(err)
	}
	if err := r.client.StartContainer(c.ID, opts.HostConfig); err != nil {
		return nil, maskAny(err)
	}
	return &dockerContainer{
		client:    r.client,
		container: c,
	}, nil
}

func (p *dockerContainer) Wait() {
	p.client.WaitContainer(p.container.ID)
}

func (p *dockerContainer) Kill() error {
	opts := docker.RemoveContainerOptions{
		ID:    p.container.ID,
		Force: true,
	}
	if err := p.client.RemoveContainer(opts); err != nil {
		return maskAny(err)
	}
	return nil
}
