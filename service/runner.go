package service

type Volume struct {
	HostPath      string
	ContainerPath string
	ReadOnly      bool
}

type Runner interface {
	GetContainerDir(hostDir string) string
	Start(command string, args []string, volumes []Volume, containerName string) (Process, error)
}

type Process interface {
	Wait()
	Kill() error
}
