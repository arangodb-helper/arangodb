package service

import (
	"fmt"
	"os/exec"
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
	cmd *exec.Cmd
}

func (r *processRunner) GetContainerDir(hostDir string) string {
	return hostDir
}

func (r *processRunner) Start(command string, args []string, volumes []Volume, ports []int, containerName string) (Process, error) {
	c := exec.Command(command, args...)
	if err := c.Start(); err != nil {
		return nil, maskAny(err)
	}
	return &process{c}, nil
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

// ProcessID returns the pid of the process (if not running in docker)
func (p *process) ProcessID() int {
	proc := p.cmd.Process
	if proc != nil {
		return proc.Pid
	}
	return 0
}

// ContainerID returns the ID of the docker container that runs the process.
func (p *process) ContainerID() string {
	return ""
}

func (p *process) Wait() {
	proc := p.cmd.Process
	if proc != nil {
		proc.Wait()
	}
}

func (p *process) Terminate() error {
	proc := p.cmd.Process
	if proc != nil {
		if err := proc.Signal(syscall.SIGTERM); err != nil {
			return maskAny(err)
		}
	}
	return nil
}

func (p *process) Kill() error {
	proc := p.cmd.Process
	if proc != nil {
		if err := proc.Kill(); err != nil {
			return maskAny(err)
		}
	}
	return nil
}
