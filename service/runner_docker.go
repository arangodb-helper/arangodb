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
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	docker "github.com/fsouza/go-dockerclient"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
)

const (
	stopContainerTimeout = 60 // Seconds before a container is killed (after graceful stop)
	containerFileName    = "CONTAINER"
	createdByKey         = "created-by"
	createdByValue       = "arangodb-starter"
	dockerDataDir        = "/data"
)

// NewDockerRunner creates a runner that starts processes in a docker container.
func NewDockerRunner(log zerolog.Logger, endpoint, arangodImage, arangoSyncImage string, imagePullPolicy ImagePullPolicy, user, volumesFrom string, gcDelay time.Duration,
	networkMode string, privileged, tty bool) (Runner, error) {

	os.Setenv("DOCKER_HOST", endpoint)
	client, err := docker.NewClientFromEnv()
	if err != nil {
		return nil, maskAny(err)
	}
	return &dockerRunner{
		log:             log,
		client:          client,
		arangodImage:    arangodImage,
		arangoSyncImage: arangoSyncImage,
		imagePullPolicy: imagePullPolicy,
		user:            user,
		volumesFrom:     volumesFrom,
		containerIDs:    make(map[string]time.Time),
		gcDelay:         gcDelay,
		networkMode:     networkMode,
		privileged:      privileged,
		tty:             tty,
	}, nil
}

// dockerRunner implements a Runner that starts processes in a docker container.
type dockerRunner struct {
	log             zerolog.Logger
	client          *docker.Client
	arangodImage    string
	arangoSyncImage string
	imagePullPolicy ImagePullPolicy
	user            string
	volumesFrom     string
	mutex           sync.Mutex
	containerIDs    map[string]time.Time
	gcOnce          sync.Once
	gcDelay         time.Duration
	networkMode     string
	privileged      bool
	tty             bool
}

type dockerContainer struct {
	log       zerolog.Logger
	client    *docker.Client
	container *docker.Container
	waiter    docker.CloseWaiter
}

func (r *dockerRunner) GetContainerDir(hostDir, defaultContainerDir string) string {
	if r.volumesFrom != "" {
		return hostDir
	}
	return defaultContainerDir
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

func (r *dockerRunner) Start(ctx context.Context, processType ProcessType, command string, args []string, volumes []Volume, ports []int, containerName, serverDir string, output io.Writer) (Process, error) {
	// Start gc (once)
	r.startGC()

	// Select image
	var image string
	switch processType {
	case ProcessTypeArangod:
		image = r.arangodImage
	case ProcessTypeArangoSync:
		image = r.arangoSyncImage
	default:
		return nil, maskAny(fmt.Errorf("Unknown process type: %s", processType))
	}

	// Pull docker image
	switch r.imagePullPolicy {
	case ImagePullPolicyAlways:
		if err := r.pullImage(ctx, image); err != nil {
			return nil, maskAny(err)
		}
	case ImagePullPolicyIfNotPresent:
		if found, err := r.imageExists(ctx, image); err != nil {
			return nil, maskAny(err)
		} else if !found {
			if err := r.pullImage(ctx, image); err != nil {
				return nil, maskAny(err)
			}
		}
	case ImagePullPolicyNever:
		if found, err := r.imageExists(ctx, image); err != nil {
			return nil, maskAny(err)
		} else if !found {
			return nil, maskAny(fmt.Errorf("Image '%s' not found", image))
		}
	}

	// Ensure container name is valid
	containerName = strings.Replace(containerName, ":", "", -1)

	var result Process
	op := func() error {
		// Make sure the container is really gone
		r.log.Debug().Msgf("Removing container '%s' (if it exists)", containerName)
		if err := r.client.RemoveContainer(docker.RemoveContainerOptions{
			ID:    containerName,
			Force: true,
		}); err != nil && !isNoSuchContainer(err) {
			r.log.Error().Err(err).Msgf("Failed to remove container '%s'", containerName)
		}
		// Try starting it now
		p, err := r.start(image, command, args, volumes, ports, containerName, serverDir, output)
		if err != nil {
			return maskAny(err)
		}
		result = p
		return nil
	}

	if err := retry(ctx, op, time.Minute*2); err != nil {
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
func (r *dockerRunner) start(image string, command string, args []string, volumes []Volume, ports []int, containerName, serverDir string, output io.Writer) (Process, error) {
	env := make([]string, 0, 1)
	licenseKey := os.Getenv("ARANGO_LICENSE_KEY")
	if licenseKey != "" {
		env = append(env, "ARANGO_LICENSE_KEY="+licenseKey)
	}
	opts := docker.CreateContainerOptions{
		Name: containerName,
		Config: &docker.Config{
			Image:        image,
			Entrypoint:   []string{command},
			Cmd:          args,
			Env:          env,
			Tty:          r.tty,
			AttachStdout: output != nil,
			AttachStderr: output != nil,
			User:         r.user,
			ExposedPorts: make(map[docker.Port]struct{}),
			Labels: map[string]string{
				createdByKey: createdByValue,
			},
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
	r.log.Debug().Msgf("Creating container %s", containerName)
	c, err := r.client.CreateContainer(opts)
	if err != nil {
		r.log.Error().Err(err).Interface("options", opts).Msg("Creating container failed")
		return nil, maskAny(err)
	}
	r.recordContainerID(c.ID) // Record ID so we can clean it up later

	var waiter docker.CloseWaiter
	if output != nil {
		// Attach output to container
		r.log.Debug().Msgf("Attaching to output of container %s", containerName)
		success := make(chan struct{})
		defer close(success)
		waiter, err = r.client.AttachToContainerNonBlocking(docker.AttachToContainerOptions{
			Container:    c.ID,
			OutputStream: output,
			Logs:         true,
			Stdout:       true,
			Stderr:       true,
			Success:      success,
			Stream:       true,
			RawTerminal:  true,
		})
		if err != nil {
			r.log.Error().Err(err).Msgf("Failed to attach to output of container %s", c.ID)
			return nil, maskAny(err)
		}
		<-success
	}
	r.log.Debug().Msgf("Starting container %s", containerName)
	if err := r.client.StartContainer(c.ID, opts.HostConfig); err != nil {
		return nil, maskAny(err)
	}
	r.log.Debug().Msgf("Started container %s", containerName)
	// Write container ID to disk
	containerFilePath := filepath.Join(serverDir, containerFileName)
	if err := ioutil.WriteFile(containerFilePath, []byte(c.ID), 0755); err != nil {
		r.log.Error().Err(err).Msgf("Failed to store container ID in '%s'", containerFilePath)
	}
	// Inspect container to make sure we have the latest info
	c, err = r.client.InspectContainer(c.ID)
	if err != nil {
		return nil, maskAny(err)
	}
	return &dockerContainer{
		log:       r.log.With().Str("container", c.ID).Logger(),
		client:    r.client,
		container: c,
		waiter:    waiter,
	}, nil
}

// imageExists looks for a local image and returns true it it exists, false otherwise.
func (r *dockerRunner) imageExists(ctx context.Context, image string) (bool, error) {
	found := false
	op := func() error {
		if _, err := r.client.InspectImage(image); isNoSuchImage(err) {
			found = false
			return nil
		} else if err != nil {
			return maskAny(err)
		} else {
			found = true
			return nil
		}
	}

	if err := retry(ctx, op, time.Minute*2); err != nil {
		return false, maskAny(err)
	}
	return found, nil
}

// pullImage tries to pull the given image.
// It retries several times upon failure.
func (r *dockerRunner) pullImage(ctx context.Context, image string) error {
	// Pull docker image
	repo, tag := docker.ParseRepositoryTag(image)

	op := func() error {
		r.log.Debug().Msgf("Pulling image %s:%s", repo, tag)
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

	if err := retry(ctx, op, time.Minute*2); err != nil {
		return maskAny(err)
	}
	return nil
}

func (r *dockerRunner) CreateStartArangodbCommand(myDataDir string, index int, masterIP, masterPort, starterImageName string, clusterConfig ClusterConfig) string {
	addr := masterIP
	portOffsetIncrement := clusterConfig.NextPortOffset(0)
	hostPort := DefaultMasterPort + (portOffsetIncrement * (index - 1))
	if masterPort != "" {
		addr = net.JoinHostPort(addr, masterPort)
		masterPortI, _ := strconv.Atoi(masterPort)
		hostPort = masterPortI + (portOffsetIncrement * (index - 1))
	}
	var netArgs string
	if r.networkMode == "" || r.networkMode == "default" {
		netArgs = fmt.Sprintf("-p %d:%d", hostPort, DefaultMasterPort)
	} else {
		netArgs = fmt.Sprintf("--net=%s", r.networkMode)
	}
	lines := []string{
		fmt.Sprintf("docker volume create arangodb%d &&", index),
		fmt.Sprintf("docker run -it --name=adb%d --rm %s -v arangodb%d:%s", index, netArgs, index, dockerDataDir),
		fmt.Sprintf("-v /var/run/docker.sock:/var/run/docker.sock %s", starterImageName),
		fmt.Sprintf("--starter.address=%s --starter.join=%s", masterIP, addr),
	}
	return strings.Join(lines, " \\\n    ")
}

// Cleanup after all processes are dead and have been cleaned themselves
func (r *dockerRunner) Cleanup() error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	for id := range r.containerIDs {
		r.log.Info().Msgf("Removing container %s", id)
		if err := r.client.RemoveContainer(docker.RemoveContainerOptions{
			ID:            id,
			Force:         true,
			RemoveVolumes: true,
		}); err != nil && !isNoSuchContainer(err) {
			r.log.Warn().Err(err).Msgf("Failed to remove container %s: %#v", id)
		}
	}
	r.containerIDs = make(map[string]time.Time)

	return nil
}

// recordContainerID records an ID of a created container
func (r *dockerRunner) recordContainerID(id string) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if r.containerIDs != nil {
		r.containerIDs[id] = time.Now()
	}
}

// unrecordContainerID removes an ID from the list of created containers
func (r *dockerRunner) unrecordContainerID(id string) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if r.containerIDs != nil {
		delete(r.containerIDs, id)
	}
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
					r.log.Warn().Err(err).Msgf("Failed to inspect container %s", id)
				}
			} else if canGC(c) {
				// Container is dead for more than 10 minutes, gc it.
				r.log.Info().Msgf("Removing old container %s", id)
				if err := r.client.RemoveContainer(docker.RemoveContainerOptions{
					ID:            id,
					RemoveVolumes: true,
				}); err != nil {
					r.log.Warn().Err(err).Msgf("Failed to remove container %s", id)
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
	if p.waiter != nil {
		p.waiter.Wait()
	}
	exitCode, err := p.client.WaitContainer(p.container.ID)
	if err != nil {
		p.log.Error().Err(err).Msg("WaitContainer failed")
	} else if exitCode != 0 {
		p.log.Info().Int("exitcode", exitCode).Msg("Container terminated with non-zero exit code")
	}
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

// Hup sends a SIGHUP to the process
func (p *dockerContainer) Hup() error {
	if err := p.client.KillContainer(docker.KillContainerOptions{
		ID:     p.container.ID,
		Signal: docker.SIGHUP,
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

// isNoSuchImage returns true if the given error is (or is caused by) a NoSuchImage error.
func isNoSuchImage(err error) bool {
	return err == docker.ErrNoSuchImage || errors.Cause(err) == docker.ErrNoSuchImage
}

// isNotFound returns true if the given error is (or is caused by) a 404 response error.
func isNotFound(err error) bool {
	if err, ok := errors.Cause(err).(*docker.Error); ok {
		return err.Status == 404
	}
	return false
}
