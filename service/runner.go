package service

type Volume struct {
	HostPath      string
	ContainerPath string
	ReadOnly      bool
}

type Runner interface {
	GetContainerDir(hostDir string) string
	Start(command string, args []string, volumes []Volume, ports []int, containerName string) (Process, error)
	CreateStartArangodbCommand(index int, masterIP string, masterPort string) string

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

	// Wait until the process has terminated
	Wait()
	// Terminate performs a graceful termination of the process
	Terminate() error
	// Kill performs a hard termination of the process
	Kill() error

	// Remove all traces of this process
	Cleanup() error
}
