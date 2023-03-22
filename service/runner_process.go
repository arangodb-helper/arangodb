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
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/rs/zerolog"

	"github.com/arangodb-helper/arangodb/pkg/definitions"
)

// NewProcessRunner creates a runner that starts processes on the local OS.
func NewProcessRunner(log zerolog.Logger) Runner {
	return &processRunner{
		log: log,
	}
}

// processRunner implements a ProcessRunner that starts processes on the local OS.
type processRunner struct {
	log zerolog.Logger
}

type process struct {
	log     zerolog.Logger
	p       *os.Process
	isChild bool
}

// getLockFilePath returns path to the file with the lock for the given server directory.
func getLockFilePath(serverDir string) string {
	return filepath.Join(serverDir, "data", "LOCK")
}

func (p *process) WaitCh() <-chan int {
	c := make(chan int)

	go func() {
		defer close(c)

		c <- p.Wait()
	}()

	return c
}

func (r *processRunner) GetContainerDir(hostDir, _ string) string {
	return hostDir
}

// GetRunningServer checks if there is already a server process running in the given server directory.
// If that is the case, its process is returned.
// Otherwise nil is returned.
func (r *processRunner) GetRunningServer(serverDir string) (Process, error) {
	lockFile := getLockFilePath(serverDir)
	lockContent, err := ioutil.ReadFile(lockFile)
	if os.IsNotExist(err) {
		r.log.Debug().Msgf("Cannot find %s", lockFile)
		return nil, nil
	} else if err != nil {
		return nil, maskAny(err)
	}
	pid, err := strconv.Atoi(string(lockContent))
	if err != nil {
		// No valid contents in LOCK file
		return nil, nil
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		// Cannot find pid
		r.log.Debug().Msgf("Cannot find process %d", pid)
		return nil, nil
	}
	if err := p.Signal(syscall.Signal(0)); err != nil {
		// Process does not seem to exist anymore
		r.log.Debug().Msgf("Cannot signal process %d", pid)
		return nil, nil
	}
	// Apparently we still have a server.
	return &process{log: r.log, p: p, isChild: false}, nil
}

func (r *processRunner) Start(ctx context.Context, processType definitions.ProcessType, command string, args []string, envs map[string]string, volumes []Volume, ports []int, containerName, serverDir string, output io.Writer) (Process, error) {
	c := exec.Command(command, args...)
	if output != nil {
		c.Stdout = output
		c.Stderr = output
	}

	c.SysProcAttr = getSysProcAttr()

	// Copy current envs
	c.Env = os.Environ()
	for k, v := range envs {
		c.Env = append(c.Env, fmt.Sprintf("%s=%s", k, v))
	}
	c.Env = append(c.Env, fmt.Sprintf("ARANGODB_SERVER_DIR=%s", serverDir))

	if err := c.Start(); err != nil {
		return nil, maskAny(err)
	}
	return &process{log: r.log, p: c.Process, isChild: true}, nil
}

func (r *processRunner) CreateStartArangodbCommand(myDataDir string, index int, masterIP, masterPort, starterImageName string, clusterConfig ClusterConfig) string {
	if masterIP == "" {
		masterIP = "127.0.0.1"
	}
	addr := masterIP
	if masterPort != "" {
		addr = net.JoinHostPort(addr, masterPort)
	}
	var dataDir string
	if strings.HasSuffix(myDataDir, "1") {
		dataDir = fmt.Sprintf("%s%d", myDataDir[:len(myDataDir)-1], index)
	} else {
		dataDir = fmt.Sprintf("./db%d", index)
	}
	return fmt.Sprintf("arangodb --starter.data-dir=%s --starter.join %s", dataDir, addr)
}

// Cleanup after all processes are dead and have been cleaned themselves
func (r *processRunner) Cleanup() error {
	// Nothing here
	return nil
}

// ProcessID returns the pid of the process (if not running in docker)
func (p *process) ProcessID() int {
	proc := p.p
	if proc != nil {
		return proc.Pid
	}
	return 0
}

// ContainerID returns the ID of the docker container that runs the process.
func (p *process) ContainerID() string {
	return ""
}

// ContainerIP returns the IP address of the docker container that runs the process.
func (p *process) ContainerIP() string {
	return ""
}

// HostPort returns the port on the host that is used to access the given port of the process.
func (p *process) HostPort(containerPort int) (int, error) {
	return containerPort, nil
}

func (p *process) Wait() int {
	if proc := p.p; proc != nil {
		p.log.Debug().Msgf("Waiting on %d", proc.Pid)
		if p.isChild {
			ps, err := proc.Wait()
			if err != nil {
				if err.Error() != "wait: no child processes" && err.Error() != "waitid: no child processes" {
					// on terminate Wait might be called twice
					p.log.Error().Err(err).Msgf("Wait on %d failed", proc.Pid)
				}
			} else {
				if ps.ExitCode() != 0 {
					if ws, ok := ps.Sys().(syscall.WaitStatus); ok {
						l := p.log.Info()
						if ws.Exited() {
							l = l.Int("exit-status", ws.ExitStatus())
						}

						if ws.Stopped() {
							l = l.Str("stop-signal", ws.StopSignal().String())
						}

						if ws.Signaled() {
							l = l.Str("signal", ws.Signal().String())
						}

						if ws.Continued() {
							l = l.Bool("continued", true)
						}

						if ws.CoreDump() {
							l = l.Bool("core-dump", true)
						}

						l.Int("trap-cause", ws.TrapCause()).Msgf("Wait on %d returned", proc.Pid)
					} else {
						p.log.Info().Int("exitcode", ps.ExitCode()).Msgf("Wait on %d returned", proc.Pid)
					}
				}

				return ps.ExitCode()
			}
		} else {
			// Cannot wait on non-child process, so let's do it the hard way
			for {
				if err := proc.Signal(syscall.Signal(0)); err != nil {
					// Process does not seem to exist anymore
					p.log.Debug().Msgf("Wait on %d ended at process seems to be gone", proc.Pid)
					break
				}
				time.Sleep(time.Second)
			}
		}
	}
	return -1
}

func (p *process) Kill() error {
	if proc := p.p; proc != nil {
		if err := proc.Kill(); err != nil {
			return maskAny(err)
		}
	}
	return nil
}

func (p *process) Hup() error {
	if proc := p.p; proc != nil {
		if err := proc.Signal(syscall.SIGHUP); err != nil {
			return maskAny(err)
		}
	}
	return nil
}

// Remove all traces of this process
func (p *process) Cleanup() error {
	// Nothing todo here
	return nil
}

// GetLogger creates a new logger for the process.
func (p *process) GetLogger(logger zerolog.Logger) zerolog.Logger {
	return logger.With().Int("pid", p.ProcessID()).Logger()
}
