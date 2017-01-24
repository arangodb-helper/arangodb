package service

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	logging "github.com/op/go-logging"
)

type ServiceConfig struct {
	AgencySize        int
	ArangodExecutable string
	ArangodJSstartup  string
	MasterPort        int
	RrPath            string
	StartCoordinator  bool
	StartDBserver     bool
	DataDir           string
	OwnAddress        string // IP address of used to reach this process
	MasterAddress     string
	Verbose           bool

	DockerContainer string // Name of the container running this process
	DockerEndpoint  string // Where to reach the docker daemon
	DockerImage     string // Name of Arangodb docker image
	DockerUser      string
}

type Service struct {
	ServiceConfig
	log     *logging.Logger
	state   State
	starter chan bool
	myPeers peers
	stop    bool
}

// NewService creates a new Service instance from the given config.
func NewService(log *logging.Logger, config ServiceConfig) (*Service, error) {
	return &Service{
		ServiceConfig: config,
		log:           log,
		state:         stateStart,
		starter:       make(chan bool),
	}, nil
}

// Configuration data with defaults:

// Overall state:

type State int

const (
	stateStart   State = iota // initial state after start
	stateMaster               // finding phase, first instance
	stateSlave                // finding phase, further instances
	stateRunning              // running phase
)

// State of peers:

type peers struct {
	Hosts        []string
	PortOffsets  []int
	Directories  []string
	MyIndex      int
	AgencySize   int
	MasterHostIP string
	MasterPort   int
}

// A helper function:

func findHost(a string) string {
	pos := strings.LastIndex(a, ":")
	var host string
	if pos > 0 {
		host = a[:pos]
	} else {
		host = a
	}
	if host == "127.0.0.1" || host == "[::1]" {
		host = "localhost"
	}
	return host
}

// For Windows we need to change backslashes to slashes, strangely enough:
func slasher(s string) string {
	return strings.Replace(s, "\\", "/", -1)
}

func testInstance(address string, port int) bool {
	for i := 0; i < 300; i++ {
		url := fmt.Sprintf("http://%s:%d/_api/version", address, port)
		r, e := http.Get(url)
		if e == nil && r != nil && r.StatusCode == 200 {
			return true
		}
		time.Sleep(time.Millisecond * 500)
	}
	return false
}

var confFileTemplate = `# ArangoDB configuration file
#
# Documentation:
# https://docs.arangodb.com/Manual/Administration/Configuration/
#

[server]
endpoint = tcp://0.0.0.0:%s
threads = %d

[log]
level = %s

[javascript]
v8-contexts = %d
`

func (s *Service) makeBaseArgs(myHostDir, myContainerDir string, myAddress string, myPort string, mode string) (args []string, configVolumes []Volume) {
	hostConfFileName := filepath.Join(myHostDir, "arangod.conf")
	containerConfFileName := filepath.Join(myContainerDir, "arangod.conf")

	if runtime.GOOS != "linux" {
		configVolumes = append(configVolumes, Volume{
			HostPath:      hostConfFileName,
			ContainerPath: containerConfFileName,
			ReadOnly:      true,
		})
	}

	if _, err := os.Stat(hostConfFileName); os.IsNotExist(err) {
		out, e := os.Create(hostConfFileName)
		if e != nil {
			s.log.Fatalf("Could not create configuration file %s, error: %#v", hostConfFileName, e)
		}
		switch mode {
		// Parameters are: port, server threads, log level, v8-contexts
		case "agent":
			fmt.Fprintf(out, confFileTemplate, myPort, 8, "INFO", 1)
		case "dbserver":
			fmt.Fprintf(out, confFileTemplate, myPort, 4, "INFO", 4)
		case "coordinator":
			fmt.Fprintf(out, confFileTemplate, myPort, 16, "INFO", 4)
		}
		out.Close()
	}
	args = make([]string, 0, 40)
	executable := s.ArangodExecutable
	jsStartup := s.ArangodJSstartup
	if s.RrPath != "" {
		args = append(args, s.RrPath)
	} else if s.DockerEndpoint != "" {
		executable = "/usr/sbin/arangod"
		jsStartup = "/usr/share/arangodb3/js"
		/*		args = append(args,
					s.DockerPath,
					"run", "-d",
					"--net=host",
					"-v", myDir+":/data",
					"--name", mode+myPort,
				)
				if s.DockerUser != "" {
					args = append(args, "--user", s.DockerUser)
				}
				args = append(args, s.Docker)*/
	}
	args = append(args,
		executable,
		"-c", slasher(containerConfFileName),
		"--database.directory", slasher(filepath.Join(myContainerDir, "data")),
		"--javascript.startup-directory", slasher(jsStartup),
		"--javascript.app-path", slasher(filepath.Join(myContainerDir, "apps")),
		"--log.file", slasher(filepath.Join(myContainerDir, "arangod.log")),
		"--log.force-direct", "false",
		"--server.authentication", "false",
	)
	switch mode {
	case "agent":
		args = append(args,
			"--agency.activate", "true",
			"--agency.my-address", fmt.Sprintf("tcp://%s:%s", myAddress, myPort),
			"--agency.size", strconv.Itoa(s.AgencySize),
			"--agency.supervision", "true",
			"--foxx.queues", "false",
			"--server.statistics", "false",
		)
		for i := 0; i < s.AgencySize; i++ {
			if i != s.myPeers.MyIndex {
				args = append(args,
					"--agency.endpoint",
					fmt.Sprintf("tcp://%s:%d", s.myPeers.Hosts[i], 4001+s.myPeers.PortOffsets[i]),
				)
			}
		}
	case "dbserver":
		args = append(args,
			"--cluster.my-address", fmt.Sprintf("tcp://%s:%s", myAddress, myPort),
			"--cluster.my-role", "PRIMARY",
			"--cluster.my-local-info", fmt.Sprintf("tcp://%s:%s", myAddress, myPort),
			"--foxx.queues", "false",
			"--server.statistics", "true",
		)
	case "coordinator":
		args = append(args,
			"--cluster.my-address", fmt.Sprintf("tcp://%s:%s", myAddress, myPort),
			"--cluster.my-role", "COORDINATOR",
			"--cluster.my-local-info", fmt.Sprintf("tcp://%s:%s", myAddress, myPort),
			"--foxx.queues", "true",
			"--server.statistics", "true",
		)
	}
	if mode != "agent" {
		for i := 0; i < s.AgencySize; i++ {
			args = append(args,
				"--cluster.agency-endpoint",
				fmt.Sprintf("tcp://%s:%d", s.myPeers.Hosts[i], 4001+s.myPeers.PortOffsets[i]),
			)
		}
	}
	return
}

func writeCommand(filename string, executable string, args []string) {
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		out, err := os.Create(filename)
		if err == nil {
			defer out.Close()
			for _, s := range args {
				fmt.Fprintf(out, " %s", s)
			}
			fmt.Fprintf(out, "\n")
		}
	}
}

func (s *Service) startRunning(runner Runner) {
	s.state = stateRunning
	myAddress := s.myPeers.Hosts[s.myPeers.MyIndex] + ":"
	portOffset := s.myPeers.PortOffsets[s.myPeers.MyIndex]

	var executable string
	if s.RrPath != "" {
		executable = s.RrPath
	} else {
		executable = s.ArangodExecutable
	}

	addDataVolumes := func(configVolumes []Volume, hostPath, containerPath string) []Volume {
		if runtime.GOOS == "linux" {
			return []Volume{
				Volume{
					HostPath:      hostPath,
					ContainerPath: containerPath,
					ReadOnly:      false,
				},
			}
		}
		return configVolumes
	}

	// Start agent:
	var agentProc Process
	if s.myPeers.MyIndex < s.AgencySize {
		myPort := strconv.Itoa(4001 + portOffset)
		s.log.Infof("Starting agent on port %s", myPort)
		myHostDir := filepath.Join(s.DataDir, "agent"+myPort)
		os.MkdirAll(filepath.Join(myHostDir, "data"), 0755)
		os.MkdirAll(filepath.Join(myHostDir, "apps"), 0755)
		myContainerDir := runner.GetContainerDir(myHostDir)
		args, vols := s.makeBaseArgs(myHostDir, myContainerDir, myAddress, myPort, "agent")
		vols = addDataVolumes(vols, myHostDir, myContainerDir)
		writeCommand(filepath.Join(myHostDir, "arangod_command.txt"), executable, args)
		containerName := fmt.Sprintf("agent-%s-%s", myAddress, myPort)
		if p, err := runner.Start(executable, args, vols, containerName); err != nil {
			s.log.Errorf("Error whilst starting agent: %#v", err)
		} else {
			agentProc = p
		}
	}

	time.Sleep(time.Second)

	// Start DBserver:
	var dbserverProc Process
	if s.StartDBserver {
		myPort := strconv.Itoa(8629 + portOffset)
		s.log.Infof("Starting DBserver on port %s", myPort)
		myHostDir := filepath.Join(s.DataDir, "dbserver"+myPort)
		os.MkdirAll(filepath.Join(myHostDir, "data"), 0755)
		os.MkdirAll(filepath.Join(myHostDir, "apps"), 0755)
		myContainerDir := runner.GetContainerDir(myHostDir)
		args, vols := s.makeBaseArgs(myHostDir, myContainerDir, myAddress, myPort, "dbserver")
		vols = addDataVolumes(vols, myHostDir, myContainerDir)
		writeCommand(filepath.Join(myHostDir, "arangod_command.txt"), executable, args)
		containerName := fmt.Sprintf("dbserver-%s-%s", myAddress, myPort)
		if p, err := runner.Start(executable, args, vols, containerName); err != nil {
			s.log.Errorf("Error whilst starting dbserver: %#v", err)
		} else {
			dbserverProc = p
		}
	}

	time.Sleep(time.Second)

	// Start Coordinator:
	var coordinatorProc Process
	if s.StartCoordinator {
		myPort := strconv.Itoa(8530 + portOffset)
		s.log.Infof("Starting coordinator on port %s", myPort)
		myHostDir := filepath.Join(s.DataDir, "coordinator"+myPort)
		os.MkdirAll(filepath.Join(myHostDir, "data"), 0755)
		os.MkdirAll(filepath.Join(myHostDir, "apps"), 0755)
		myContainerDir := runner.GetContainerDir(myHostDir)
		args, vols := s.makeBaseArgs(myHostDir, myContainerDir, myAddress, myPort, "coordinator")
		vols = addDataVolumes(vols, myHostDir, myContainerDir)
		writeCommand(filepath.Join(myHostDir, "arangod_command.txt"), executable, args)
		containerName := fmt.Sprintf("coordinator-%s-%s", myAddress, myPort)
		if p, err := runner.Start(executable, args, vols, containerName); err != nil {
			s.log.Errorf("Error whilst starting coordinator: %#v", err)
		} else {
			coordinatorProc = p
		}
	}

	// Check servers:
	me := s.myPeers.MyIndex
	if me < s.AgencySize {
		if testInstance(s.myPeers.Hosts[me], 4001+s.myPeers.PortOffsets[me]) {
			s.log.Info("Agent up and running.")
		} else {
			s.log.Warning("Agent not ready after 5min!")
		}
	}
	if testInstance(s.myPeers.Hosts[me], 8629+s.myPeers.PortOffsets[me]) {
		s.log.Info("DBserver up and running.")
	} else {
		s.log.Warning("DBserver not ready after 5min!")
	}
	if testInstance(s.myPeers.Hosts[me], 8530+s.myPeers.PortOffsets[me]) {
		s.log.Info("Coordinator up and running.")
	} else {
		s.log.Warning("Coordinator not ready after 5min!")
	}

	if coordinatorProc != nil {
		coordinatorProc.Wait()
	}
	if dbserverProc != nil {
		dbserverProc.Wait()
	}
	time.Sleep(3 * time.Second)
	if agentProc != nil {
		agentProc.Wait()
	}

	for {
		time.Sleep(time.Second)
		if s.stop {
			break
		}
	}

	fmt.Println("Shutting down services...")
	if coordinatorProc != nil {
		coordinatorProc.Kill()
	}
	if dbserverProc != nil {
		dbserverProc.Kill()
	}
	time.Sleep(3 * time.Second)
	if agentProc != nil {
		agentProc.Kill()
	}
}

// saveSetup saves the current peer configuration to disk.
func (s *Service) saveSetup() error {
	b, err := json.Marshal(s.myPeers)
	if err != nil {
		s.log.Errorf("Cannot serialize myPeers: %#v", err)
		return maskAny(err)
	}
	if err := ioutil.WriteFile(filepath.Join(s.DataDir, "setup.json"), b, 0644); err != nil {
		s.log.Errorf("Error writing setup: %#v", err)
		return maskAny(err)
	}
	return nil
}

// Run runs the service in either master or slave mode.
func (s *Service) Run(stopChan chan bool) {
	go func() {
		select {
		case <-stopChan:
			s.stop = true
		}
	}()

	// Find the port mapping if running in a docker container
	if s.DockerContainer != "" {
		if s.OwnAddress == "" {
			s.log.Fatal("OwnAddress must be specified")
		}
		hostPort, err := findDockerExposedAddress(s.DockerEndpoint, s.DockerContainer, s.MasterPort)
		if err != nil {
			s.log.Fatalf("Failed to detect port mapping: %#v", err)
			return
		}
		s.myPeers.MasterHostIP = s.OwnAddress
		s.myPeers.MasterPort = hostPort
	} else {
		s.myPeers.MasterHostIP = s.OwnAddress
		s.myPeers.MasterPort = s.MasterPort
	}

	// Create a runner
	var runner Runner
	if s.DockerEndpoint != "" && s.DockerImage != "" {
		var err error
		runner, err = NewDockerRunner(s.DockerEndpoint, s.DockerImage, s.DockerUser)
		if err != nil {
			s.log.Fatalf("Failed to create docker runner: %#v", err)
		}
		s.log.Debug("Using docker runner")
	} else {
		runner = NewProcessRunner()
		s.log.Debug("Using process runner")
	}

	// Is this a new start or a restart?
	if setupContent, err := ioutil.ReadFile(filepath.Join(s.DataDir, "setup.json")); err == nil {
		// Could read file
		if err := json.Unmarshal(setupContent, &s.myPeers); err == nil {
			s.AgencySize = s.myPeers.AgencySize
			fmt.Println("Relaunching service...")
			s.startRunning(runner)
			return
		} else {
			s.log.Warningf("Failed to unmarshal existing setup.json: %#v", err)
		}
	}

	// Do we have to register?
	if s.MasterAddress != "" {
		s.state = stateSlave
		s.startSlave(s.MasterAddress, runner)
	} else {
		s.state = stateMaster
		s.startMaster(runner)
	}
}
