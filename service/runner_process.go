package service

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
)

// NewProcessRunner creates a runner that starts processes on the local OS.
func NewProcessRunner() Runner {
	return &processRunner{}
}

// processRunner implements a ProcessRunner that starts processes on the local OS.
type processRunner struct {
}

type process struct {
	p *os.Process
}

func (r *processRunner) GetContainerDir(hostDir string) string {
	return hostDir
}

// GetRunningServer checks if there is already a server process running in the given server directory.
// If that is the case, its process is returned.
// Otherwise nil is returned.
func (r *processRunner) GetRunningServer(serverDir string) (Process, error) {
	lockContent, err := ioutil.ReadFile(filepath.Join(serverDir, "data", "LOCK"))
	if os.IsNotExist(err) {
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
		return nil, nil
	}
	if err := p.Signal(syscall.Signal(0)); err != nil {
		// Process does not seem to exist anymore
		return nil, nil
	}
	// Apparently we still have a server.
	return &process{p}, nil
}

func (r *processRunner) Start(command string, args []string, volumes []Volume, ports []int, containerName, serverDir string) (Process, error) {
	c := exec.Command(command, args...)
	if err := c.Start(); err != nil {
		return nil, maskAny(err)
	}
	return &process{c.Process}, nil
}

func (r *processRunner) CreateStartArangodbCommand(index int, masterIP string, masterPort string) string {
	if masterIP == "" {
		masterIP = "127.0.0.1"
	}
	addr := masterIP
	if masterPort != "" {
		addr = addr + ":" + masterPort
	}
	return fmt.Sprintf("arangodb --dataDir=./db%d --join %s", index, addr)
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

func (p *process) Wait() {
	proc := p.p
	if proc != nil {
		proc.Wait()
	}
}

func (p *process) Terminate() error {
	proc := p.p
	if proc != nil {
		if err := proc.Signal(syscall.SIGTERM); err != nil {
			return maskAny(err)
		}
	}
	return nil
}

func (p *process) Kill() error {
	proc := p.p
	if proc != nil {
		if err := proc.Kill(); err != nil {
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
