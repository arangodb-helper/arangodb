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
	"io"
	"time"

	"github.com/rs/zerolog"
)

type Volume struct {
	HostPath      string
	ContainerPath string
	ReadOnly      bool
}

type Runner interface {
	// Map the given host directory to a container directory
	GetContainerDir(hostDir, defaultContainerDir string) string

	// GetRunningServer checks if there is already a server process running in the given server directory.
	// If that is the case, its process is returned.
	// Otherwise nil is returned.
	GetRunningServer(serverDir string) (Process, error)

	// Start a server with given arguments
	Start(ctx context.Context, processType ProcessType, command string, args []string, volumes []Volume, ports []int, containerName, serverDir string, output io.Writer) (Process, error)

	// Create a command that a user should use to start a slave arangodb instance.
	CreateStartArangodbCommand(myDataDir string, index int, masterIP, masterPort, starterImageName string, clusterConfig ClusterConfig) string

	// Cleanup after all processes are dead and have been cleaned themselves
	Cleanup() error
}

type Process interface {
	// ProcessID returns the pid of the process (if not running in docker)
	ProcessID() int
	// ContainerID returns the ID of the docker container that runs the process.
	ContainerID() string
	// ContainerIP returns the IP address of the docker container that runs the process.
	ContainerIP() string
	// HostPort returns the port on the host that is used to access the given port of the process.
	HostPort(containerPort int) (int, error)

	// Wait until the process has terminated
	Wait()
	// Terminate performs a graceful termination of the process
	Terminate() error
	// Kill performs a hard termination of the process
	Kill() error
	// Hup sends a SIGHUP to the process
	Hup() error

	// Remove all traces of this process
	Cleanup() error
}

// terminateProcess tries to terminate the given process gracefully.
// When the process has not terminated after given timeout it is killed.
func terminateProcess(log zerolog.Logger, p Process, name string, killTimeout time.Duration) {
	log.Debug().Msgf("Terminating %s...", name)
	terminated := make(chan struct{})
	go func() {
		defer close(terminated)
		if err := p.Terminate(); err != nil {
			log.Warn().Err(err).Msgf("Failed to terminate %s", name)
		}
                p.Wait()
		log.Debug().Msgf("%s terminated", name)
	}()
	select {
	case <-terminated:
		// We're done
	case <-time.After(killTimeout):
		// Kill the process
		log.Warn().Msgf("Killing %s...", name)
		p.Kill()
                p.Wait()
	}
}
