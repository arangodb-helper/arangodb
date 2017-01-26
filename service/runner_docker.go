package service

import (
	"fmt"
	"strconv"
	"strings"

	docker "github.com/fsouza/go-dockerclient"
	logging "github.com/op/go-logging"
)

const (
	stopContainerTimeout = 60 // Seconds before a container is killed (after graceful stop)
)

// NewDockerRunner creates a runner that starts processes on the local OS.
func NewDockerRunner(log *logging.Logger, endpoint, image, user, volumesFrom string) (Runner, error) {
	client, err := docker.NewClient(endpoint)
	if err != nil {
		return nil, maskAny(err)
	}
	return &dockerRunner{
		log:         log,
		client:      client,
		image:       image,
		user:        user,
		volumesFrom: volumesFrom,
	}, nil
}

// dockerRunner implements a Runner that starts processes in a docker container.
type dockerRunner struct {
	log         *logging.Logger
	client      *docker.Client
	image       string
	user        string
	volumesFrom string
}

type dockerContainer struct {
	client    *docker.Client
	container *docker.Container
}

func (r *dockerRunner) GetContainerDir(hostDir string) string {
	if r.volumesFrom != "" {
		return hostDir
	}
	return "/data"
}

func (r *dockerRunner) Start(command string, args []string, volumes []Volume, ports []int, containerName string) (Process, error) {
	// Pull docker image
	repo, tag := docker.ParseRepositoryTag(r.image)
	r.log.Debugf("Pulling image %s:%s", repo, tag)
	if err := r.client.PullImage(docker.PullImageOptions{
		Repository: repo,
		Tag:        tag,
	}, docker.AuthConfiguration{}); err != nil {
		return nil, maskAny(err)
	}

	containerName = strings.Replace(containerName, ":", "", -1)
	opts := docker.CreateContainerOptions{
		Name: containerName,
		Config: &docker.Config{
			Image:        r.image,
			Entrypoint:   []string{command},
			Cmd:          args,
			Tty:          true,
			User:         r.user,
			ExposedPorts: make(map[docker.Port]struct{}),
		},
		HostConfig: &docker.HostConfig{
			PortBindings:    make(map[docker.Port][]docker.PortBinding),
			PublishAllPorts: true,
			AutoRemove:      true,
		},
	}
	if r.volumesFrom != "" {
		opts.HostConfig.VolumesFrom = []string{r.volumesFrom}
	} else {
		for _, v := range volumes {
			bind := fmt.Sprintf("%s:%s", v.HostPath, v.ContainerPath)
			if v.ReadOnly {
				bind = bind + ":ro"
			}
			opts.HostConfig.Binds = append(opts.HostConfig.Binds, bind)
		}
	}
	for _, p := range ports {
		dockerPort := docker.Port(fmt.Sprintf("%d/tcp", p))
		opts.Config.ExposedPorts[dockerPort] = struct{}{}
		opts.HostConfig.PortBindings[dockerPort] = []docker.PortBinding{
			docker.PortBinding{
				HostIP:   "0.0.0.0",
				HostPort: strconv.Itoa(p),
			},
		}
	}
	r.log.Debugf("Creating container %s", containerName)
	c, err := r.client.CreateContainer(opts)
	if err != nil {
		return nil, maskAny(err)
	}
	r.log.Debugf("Starting container %s", containerName)
	if err := r.client.StartContainer(c.ID, opts.HostConfig); err != nil {
		return nil, maskAny(err)
	}
	r.log.Debugf("Started container %s", containerName)
	return &dockerContainer{
		client:    r.client,
		container: c,
	}, nil
}

func (r *dockerRunner) CreateStartArangodbCommand(index int, masterIP string, masterPort string) string {
	addr := masterIP
	hostPort := 4000 + (portOffsetIncrement * (index - 1))
	if masterPort != "" {
		addr = addr + ":" + masterPort
		masterPortI, _ := strconv.Atoi(masterPort)
		hostPort = masterPortI + (portOffsetIncrement * (index - 1))
	}
	lines := []string{
		fmt.Sprintf("docker volume create arangodb%d &&", index),
		fmt.Sprintf("docker run -it --name=adb%d --rm -p %d:4000 -v arangodb%d:/data -v /var/run/docker.sock:/var/run/docker.sock arangodb/arangodb-starter", index, hostPort, index),
		fmt.Sprintf("--dockerContainer=adb%d --ownAddress=%s --join=%s", index, masterIP, addr),
	}
	return strings.Join(lines, " \\\n    ")
}

// ProcessID returns the pid of the process (if not running in docker)
func (p *dockerContainer) ProcessID() int {
	return 0
}

// ContainerID returns the ID of the docker container that runs the process.
func (p *dockerContainer) ContainerID() string {
	return p.container.ID
}

func (p *dockerContainer) Wait() {
	p.client.WaitContainer(p.container.ID)
}

func (p *dockerContainer) Terminate() error {
	if err := p.client.StopContainer(p.container.ID, stopContainerTimeout); err != nil {
		return maskAny(err)
	}
	return nil
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
