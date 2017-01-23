package service

import "os/exec"

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

func (r *processRunner) Start(command string, args []string, volumes []Volume, containerName string) (Process, error) {
	c := exec.Command(command, args...)
	if err := c.Start(); err != nil {
		return nil, maskAny(err)
	}
	return &process{c}, nil
}

func (p *process) Wait() {
	proc := p.cmd.Process
	if proc != nil {
		proc.Wait()
	}
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
