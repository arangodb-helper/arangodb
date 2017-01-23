package service

type Runner interface {
	GetContainerDir(hostDir string) string
	Start(command string, args []string, hostVolume, containerVolume, containerName string) (Process, error)
}

type Process interface {
	Wait()
	Kill() error
}
