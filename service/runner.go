package service

type Volume struct {
	HostPath      string
	ContainerPath string
	ReadOnly      bool
}

type Runner interface {
	// Map the given host directory to a container directory
	GetContainerDir(hostDir string) string

	// GetRunningServer checks if there is already a server process running in the given server directory.
	// If that is the case, its process is returned.
	// Otherwise nil is returned.
	GetRunningServer(serverDir string) (Process, error)

	// Start a server with given arguments
	Start(command string, args []string, volumes []Volume, ports []int, containerName, serverDir string) (Process, error)

	// Create a command that a user should use to start a slave arangodb instance.
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
