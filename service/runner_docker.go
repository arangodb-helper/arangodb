package service

import (
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	docker "github.com/fsouza/go-dockerclient"
	logging "github.com/op/go-logging"
	"github.com/pkg/errors"
)

const (
	stopContainerTimeout = 60 // Seconds before a container is killed (after graceful stop)
	containerFileName    = "CONTAINER"
)

// NewDockerRunner creates a runner that starts processes on the local OS.
func NewDockerRunner(log *logging.Logger, endpoint, image, user, volumesFrom string, gcDelay time.Duration, networkMode string, privileged bool) (Runner, error) {
	client, err := docker.NewClient(endpoint)
	if err != nil {
		return nil, maskAny(err)
	}
	return &dockerRunner{
		log:          log,
		client:       client,
		image:        image,
		user:         user,
		volumesFrom:  volumesFrom,
		containerIDs: make(map[string]time.Time),
		gcDelay:      gcDelay,
		networkMode:  networkMode,
		privileged:   privileged,
	}, nil
}

// dockerRunner implements a Runner that starts processes in a docker container.
type dockerRunner struct {
	log          *logging.Logger
	client       *docker.Client
	image        string
	user         string
	volumesFrom  string
	mutex        sync.Mutex
	containerIDs map[string]time.Time
	gcOnce       sync.Once
	gcDelay      time.Duration
	networkMode  string
	privileged   bool
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

// GetRunningServer checks if there is already a server process running in the given server directory.
// If that is the case, its process is returned.
// Otherwise nil is returned.
func (r *dockerRunner) GetRunningServer(serverDir string) (Process, error) {
	containerContent, err := ioutil.ReadFile(filepath.Join(serverDir, containerFileName))
	if os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, maskAny(err)
	}
	id := string(containerContent)
	// We found a CONTAINER file, see if this container is still running
	c, err := r.client.InspectContainer(id)
	if err != nil {
		// Container cannot be inspected, assume it no longer exists
		return nil, nil
	}
	// Container can be inspected, check its state
	if !c.State.Running {
		// Container is not running
		return nil, nil
	}
	r.recordContainerID(c.ID)
	// Start gc (once)
	r.startGC()

	// Return container
	return &dockerContainer{
		client:    r.client,
		container: c,
	}, nil
}

func (r *dockerRunner) Start(command string, args []string, volumes []Volume, ports []int, containerName, serverDir string) (Process, error) {
	// Start gc (once)
	r.startGC()

	// Pull docker image
	if err := r.pullImage(r.image); err != nil {
		return nil, maskAny(err)
	}

	// Ensure container name is valid
	containerName = strings.Replace(containerName, ":", "", -1)

	var result Process
	op := func() error {
		// Make sure the container is really gone
		r.log.Debugf("Removing container '%s' (if it exists)", containerName)
		if err := r.client.RemoveContainer(docker.RemoveContainerOptions{
			ID:    containerName,
			Force: true,
		}); err != nil && !isNoSuchContainer(err) {
			r.log.Errorf("Failed to remove container '%s': %v", containerName, err)
		}
		// Try starting it now
		p, err := r.start(command, args, volumes, ports, containerName, serverDir)
		if err != nil {
			return maskAny(err)
		}
		result = p
		return nil
	}

	if err := retry(op, time.Minute*2); err != nil {
		return nil, maskAny(err)
	}
	return result, nil
}

// startGC ensures GC is started (only once)
func (r *dockerRunner) startGC() {
	// Start gc (once)
	r.gcOnce.Do(func() { go r.gc() })
}

// Try to start a command with given arguments
func (r *dockerRunner) start(command string, args []string, volumes []Volume, ports []int, containerName, serverDir string) (Process, error) {
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
			PublishAllPorts: false,
			AutoRemove:      false,
			Privileged:      r.privileged,
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
	if r.networkMode != "" && r.networkMode != "default" {
		opts.HostConfig.NetworkMode = r.networkMode
	} else {
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
	}
	r.log.Debugf("Creating container %s", containerName)
	c, err := r.client.CreateContainer(opts)
	if err != nil {
		return nil, maskAny(err)
	}
	r.recordContainerID(c.ID) // Record ID so we can clean it up later
	r.log.Debugf("Starting container %s", containerName)
	if err := r.client.StartContainer(c.ID, opts.HostConfig); err != nil {
		return nil, maskAny(err)
	}
	r.log.Debugf("Started container %s", containerName)
	// Write container ID to disk
	containerFilePath := filepath.Join(serverDir, containerFileName)
	if err := ioutil.WriteFile(containerFilePath, []byte(c.ID), 0755); err != nil {
		r.log.Errorf("Failed to store container ID in '%s': %v", containerFilePath, err)
	}
	// Inspect container to make sure we have the latest info
	c, err = r.client.InspectContainer(c.ID)
	if err != nil {
		return nil, maskAny(err)
	}
	return &dockerContainer{
		client:    r.client,
		container: c,
	}, nil
}

// pullImage tries to pull the given image.
// It retries several times upon failure.
func (r *dockerRunner) pullImage(image string) error {
	// Pull docker image
	repo, tag := docker.ParseRepositoryTag(r.image)

	op := func() error {
		r.log.Debugf("Pulling image %s:%s", repo, tag)
		if err := r.client.PullImage(docker.PullImageOptions{
			Repository: repo,
			Tag:        tag,
		}, docker.AuthConfiguration{}); err != nil {
			if isNotFound(err) {
				return maskAny(&PermanentError{err})
			}
			return maskAny(err)
		}
		return nil
	}

	if err := retry(op, time.Minute*2); err != nil {
		return maskAny(err)
	}
	return nil
}

func (r *dockerRunner) CreateStartArangodbCommand(index int, masterIP string, masterPort string) string {
	addr := masterIP
	hostPort := 4000 + (portOffsetIncrement * (index - 1))
	if masterPort != "" {
		addr = net.JoinHostPort(addr, masterPort)
		masterPortI, _ := strconv.Atoi(masterPort)
		hostPort = masterPortI + (portOffsetIncrement * (index - 1))
	}
	var netArgs string
	if r.networkMode == "" || r.networkMode == "default" {
		netArgs = fmt.Sprintf("-p %d:4000", hostPort)
	} else {
		netArgs = fmt.Sprintf("--net=%s", r.networkMode)
	}
	lines := []string{
		fmt.Sprintf("docker volume create arangodb%d &&", index),
		fmt.Sprintf("docker run -it --name=adb%d --rm %s -v arangodb%d:/data", index, netArgs, index),
		fmt.Sprintf("-v /var/run/docker.sock:/var/run/docker.sock arangodb/arangodb-starter"),
		fmt.Sprintf("--dockerContainer=adb%d --ownAddress=%s --join=%s", index, masterIP, addr),
	}
	return strings.Join(lines, " \\\n    ")
}

// Cleanup after all processes are dead and have been cleaned themselves
func (r *dockerRunner) Cleanup() error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	for id := range r.containerIDs {
		r.log.Infof("Removing container %s", id)
		if err := r.client.RemoveContainer(docker.RemoveContainerOptions{
			ID:            id,
			Force:         true,
			RemoveVolumes: true,
		}); err != nil && !isNoSuchContainer(err) {
			r.log.Warningf("Failed to remove container %s: %#v", id, err)
		}
	}
	r.containerIDs = nil

	return nil
}

// recordContainerID records an ID of a created container
func (r *dockerRunner) recordContainerID(id string) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.containerIDs[id] = time.Now()
}

// unrecordContainerID removes an ID from the list of created containers
func (r *dockerRunner) unrecordContainerID(id string) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	delete(r.containerIDs, id)
}

// gc performs continues garbage collection of stopped old containers
func (r *dockerRunner) gc() {
	canGC := func(c *docker.Container) bool {
		gcBoundary := time.Now().UTC().Add(-r.gcDelay)
		switch c.State.StateString() {
		case "dead", "exited":
			if c.State.FinishedAt.Before(gcBoundary) {
				// Dead or exited long enough
				return true
			}
		case "created":
			if c.Created.Before(gcBoundary) {
				// Created but not running long enough
				return true
			}
		}
		return false
	}
	for {
		ids := r.gatherCollectableContainerIDs()
		for _, id := range ids {
			c, err := r.client.InspectContainer(id)
			if err != nil {
				if isNoSuchContainer(err) {
					// container no longer exists
					r.unrecordContainerID(id)
				} else {
					r.log.Warningf("Failed to inspect container %s: %#v", id, err)
				}
			} else if canGC(c) {
				// Container is dead for more than 10 minutes, gc it.
				r.log.Infof("Removing old container %s", id)
				if err := r.client.RemoveContainer(docker.RemoveContainerOptions{
					ID:            id,
					RemoveVolumes: true,
				}); err != nil {
					r.log.Warningf("Failed to remove container %s: %#v", id, err)
				} else {
					// Remove succeeded
					r.unrecordContainerID(id)
				}
			}
		}
		time.Sleep(time.Minute)
	}
}

// gatherCollectableContainerIDs returns all container ID's that are old enough to be consider for garbage collection.
func (r *dockerRunner) gatherCollectableContainerIDs() []string {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	var result []string
	gcBoundary := time.Now().Add(-r.gcDelay)
	for id, ts := range r.containerIDs {
		if ts.Before(gcBoundary) {
			result = append(result, id)
		}
	}
	return result
}

// ProcessID returns the pid of the process (if not running in docker)
func (p *dockerContainer) ProcessID() int {
	return 0
}

// ContainerID returns the ID of the docker container that runs the process.
func (p *dockerContainer) ContainerID() string {
	return p.container.ID
}

// ContainerIP returns the IP address of the docker container that runs the process.
func (p *dockerContainer) ContainerIP() string {
	if ns := p.container.NetworkSettings; ns != nil {
		return ns.IPAddress
	}
	return ""
}

// HostPort returns the port on the host that is used to access the given port of the process.
func (p *dockerContainer) HostPort(containerPort int) (int, error) {
	if hostConfig := p.container.HostConfig; hostConfig != nil {
		if hostConfig.NetworkMode == "host" {
			return containerPort, nil
		}
		dockerPort := docker.Port(fmt.Sprintf("%d/tcp", containerPort))
		if binding, ok := hostConfig.PortBindings[dockerPort]; ok && len(binding) > 0 {
			return strconv.Atoi(binding[0].HostPort)
		}
	}
	return 0, fmt.Errorf("Cannot find port mapping.")
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
	if err := p.client.KillContainer(docker.KillContainerOptions{
		ID: p.container.ID,
	}); err != nil {
		return maskAny(err)
	}
	return nil
}

func (p *dockerContainer) Cleanup() error {
	opts := docker.RemoveContainerOptions{
		ID:            p.container.ID,
		Force:         true,
		RemoveVolumes: true,
	}
	if err := p.client.RemoveContainer(opts); err != nil {
		return maskAny(err)
	}
	return nil
}

// isNoSuchContainer returns true if the given error is (or is caused by) a NoSuchContainer error.
func isNoSuchContainer(err error) bool {
	if _, ok := err.(*docker.NoSuchContainer); ok {
		return true
	}
	if _, ok := errors.Cause(err).(*docker.NoSuchContainer); ok {
		return true
	}
	return false
}

// isNotFound returns true if the given error is (or is caused by) a 404 response error.
func isNotFound(err error) bool {
	if err, ok := errors.Cause(err).(*docker.Error); ok {
		return err.Status == 404
	}
	return false
}
