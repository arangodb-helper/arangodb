package service

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	logging "github.com/op/go-logging"
)

type ServiceConfig struct {
	ID                   string // Unique identifier of this peer
	AgencySize           int
	ArangodExecutable    string
	ArangodJSstartup     string
	MasterPort           int
	RrPath               string
	StartCoordinator     bool
	StartDBserver        bool
	DataDir              string
	OwnAddress           string // IP address of used to reach this process
	MasterAddress        string
	Verbose              bool
	ServerThreads        int  // If set to something other than 0, this will be added to the commandline of each server with `--server.threads`...
	AllPortOffsetsUnique bool // If set, all peers will get a unique port offset. If false (default) only portOffset+peerAddress pairs will be unique.
	JwtSecret            string
	SslKeyFile           string // Path containing an x509 certificate + private key to be used by the servers.
	SslCAFile            string // Path containing an x509 CA certificate used to authenticate clients.

	DockerContainer  string // Name of the container running this process
	DockerEndpoint   string // Where to reach the docker daemon
	DockerImage      string // Name of Arangodb docker image
	DockerUser       string
	DockerGCDelay    time.Duration
	DockerNetHost    bool
	DockerPrivileged bool
	RunningInDocker  bool

	ProjectVersion string
	ProjectBuild   string
}

type Service struct {
	ServiceConfig
	log                 *logging.Logger
	ctx                 context.Context
	cancel              context.CancelFunc
	state               State
	myPeers             peers
	startRunningWaiter  context.Context
	startRunningTrigger context.CancelFunc
	announcePort        int        // Port I can be reached on from the outside
	isNetHost           bool       // Is this process running in a container with `--net=host` or running outside a container?
	mutex               sync.Mutex // Mutex used to protect access to this datastructure
	allowSameDataDir    bool       // If set, multiple arangdb instances are allowed to have the same dataDir (docker case)
	servers             struct {
		agentProc       Process
		dbserverProc    Process
		coordinatorProc Process
	}
	stop bool
}

// NewService creates a new Service instance from the given config.
func NewService(log *logging.Logger, config ServiceConfig) (*Service, error) {
	// Create unique ID
	if config.ID == "" {
		b := make([]byte, 4)
		if _, err := rand.Read(b); err != nil {
			return nil, maskAny(err)
		}
		config.ID = hex.EncodeToString(b)
	}

	ctx, trigger := context.WithCancel(context.Background())
	return &Service{
		ServiceConfig:       config,
		log:                 log,
		state:               stateStart,
		startRunningWaiter:  ctx,
		startRunningTrigger: trigger,
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

const (
	portOffsetAgent       = 1
	portOffsetCoordinator = 2
	portOffsetDBServer    = 3
	portOffsetIncrement   = 5 // {our http server, agent, coordinator, dbserver, reserved}
)

const (
	maxRecentFailures = 100
)

// A helper function:

func normalizeHostName(host string) string {
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() {
			return "localhost"
		}
	}
	return host
}

// For Windows we need to change backslashes to slashes, strangely enough:
func slasher(s string) string {
	return strings.Replace(s, "\\", "/", -1)
}

// IsSecure returns true when the cluster is using SSL for connections, false otherwise.
func (s *Service) IsSecure() bool {
	return s.SslKeyFile != ""
}

func (s *Service) testInstance(ctx context.Context, address string, port int) (up, cancelled bool) {
	instanceUp := make(chan bool)
	go func() {
		client := &http.Client{Timeout: time.Second * 10}
		scheme := "http"
		if s.IsSecure() {
			scheme = "https"
			client.Transport = &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
				},
			}
		}
		makeRequest := func() error {
			addr := net.JoinHostPort(address, strconv.Itoa(port))
			url := fmt.Sprintf("%s://%s/_api/version", scheme, addr)
			req, err := http.NewRequest("GET", url, nil)
			if err != nil {
				return maskAny(err)
			}
			if err := addJwtHeader(req, s.JwtSecret); err != nil {
				return maskAny(err)
			}
			resp, err := client.Do(req)
			if err != nil {
				return maskAny(err)
			}
			if resp.StatusCode != 200 {
				return maskAny(fmt.Errorf("Invalid status %d", resp.StatusCode))
			}
			return nil
		}

		for i := 0; i < 300; i++ {
			if err := makeRequest(); err == nil {
				instanceUp <- true
				break
			}
			time.Sleep(time.Millisecond * 500)
		}
		instanceUp <- false
	}()
	select {
	case up := <-instanceUp:
		return up, false
	case <-ctx.Done():
		return false, true
	}
}

func (s *Service) makeBaseArgs(myHostDir, myContainerDir string, myAddress string, myPort string, mode string) (args []string, configVolumes []Volume) {
	hostConfFileName := filepath.Join(myHostDir, "arangod.conf")
	containerConfFileName := filepath.Join(myContainerDir, "arangod.conf")
	scheme := "tcp"
	if s.IsSecure() {
		scheme = "ssl"
	}

	if runtime.GOOS != "linux" {
		configVolumes = append(configVolumes, Volume{
			HostPath:      hostConfFileName,
			ContainerPath: containerConfFileName,
			ReadOnly:      true,
		})
	}

	if _, err := os.Stat(hostConfFileName); os.IsNotExist(err) {
		var threads, v8Contexts string
		logLevel := "INFO"
		switch mode {
		// Parameters are: port, server threads, log level, v8-contexts
		case "agent":
			threads = "8"
			v8Contexts = "1"
		case "dbserver":
			threads = "4"
			v8Contexts = "4"
		case "coordinator":
			threads = "16"
			v8Contexts = "4"
		}
		serverSection := &configSection{
			Name: "server",
			Settings: map[string]string{
				"endpoint":       fmt.Sprintf("%s://[::]:%s", scheme, myPort),
				"threads":        threads,
				"authentication": "false",
			},
		}
		if s.JwtSecret != "" {
			serverSection.Settings["authentication"] = "true"
			serverSection.Settings["jwt-secret"] = s.JwtSecret
		}
		config := configFile{
			serverSection,
			&configSection{
				Name: "log",
				Settings: map[string]string{
					"level": logLevel,
				},
			},
			&configSection{
				Name: "javascript",
				Settings: map[string]string{
					"v8-contexts": v8Contexts,
				},
			},
		}
		if s.IsSecure() {
			sslSection := &configSection{
				Name: "ssl",
				Settings: map[string]string{
					"keyfile": s.SslKeyFile,
				},
			}
			if s.SslCAFile != "" {
				sslSection.Settings["cafile"] = s.SslCAFile
			}
			config = append(config, sslSection)
		}

		out, e := os.Create(hostConfFileName)
		if e != nil {
			s.log.Fatalf("Could not create configuration file %s, error: %#v", hostConfFileName, e)
		}
		_, err := config.WriteTo(out)
		out.Close()
		if err != nil {
			s.log.Fatalf("Cannot create config file: %v", err)
		}
	}
	args = make([]string, 0, 40)
	executable := s.ArangodExecutable
	jsStartup := s.ArangodJSstartup
	if s.RrPath != "" {
		args = append(args, s.RrPath)
	}
	args = append(args,
		executable,
		"-c", slasher(containerConfFileName),
		"--database.directory", slasher(filepath.Join(myContainerDir, "data")),
		"--javascript.startup-directory", slasher(jsStartup),
		"--javascript.app-path", slasher(filepath.Join(myContainerDir, "apps")),
		"--log.file", slasher(filepath.Join(myContainerDir, "arangod.log")),
		"--log.force-direct", "false",
	)
	if s.ServerThreads != 0 {
		args = append(args, "--server.threads", strconv.Itoa(s.ServerThreads))
	}
	myTCPURL := scheme + "://" + net.JoinHostPort(myAddress, myPort)
	switch mode {
	case "agent":
		args = append(args,
			"--agency.activate", "true",
			"--agency.my-address", myTCPURL,
			"--agency.size", strconv.Itoa(s.AgencySize),
			"--agency.supervision", "true",
			"--foxx.queues", "false",
			"--server.statistics", "false",
		)
		for _, p := range s.myPeers.Peers {
			if p.HasAgent && p.ID != s.ID {
				args = append(args,
					"--agency.endpoint",
					fmt.Sprintf("%s://%s", scheme, net.JoinHostPort(p.Address, strconv.Itoa(s.MasterPort+p.PortOffset+portOffsetAgent))),
				)
			}
		}
	case "dbserver":
		args = append(args,
			"--cluster.my-address", myTCPURL,
			"--cluster.my-role", "PRIMARY",
			"--cluster.my-local-info", myTCPURL,
			"--foxx.queues", "false",
			"--server.statistics", "true",
		)
	case "coordinator":
		args = append(args,
			"--cluster.my-address", myTCPURL,
			"--cluster.my-role", "COORDINATOR",
			"--cluster.my-local-info", myTCPURL,
			"--foxx.queues", "true",
			"--server.statistics", "true",
		)
	}
	if mode != "agent" {
		for i := 0; i < s.AgencySize; i++ {
			p := s.myPeers.Peers[i]
			args = append(args,
				"--cluster.agency-endpoint",
				fmt.Sprintf("%s://%s", scheme, net.JoinHostPort(p.Address, strconv.Itoa(s.MasterPort+p.PortOffset+portOffsetAgent))),
			)
		}
	}
	return
}

func (s *Service) writeCommand(filename string, executable string, args []string) {
	content := strings.Join(args, " \\\n") + "\n"
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		if err := ioutil.WriteFile(filename, []byte(content), 0755); err != nil {
			s.log.Errorf("Failed to write command to %s: %#v", filename, err)
		}
	}
}

func (s *Service) startRunning(runner Runner) {
	s.state = stateRunning
	myPeer, ok := s.myPeers.PeerByID(s.ID)
	if !ok {
		s.log.Fatalf("Cannot find peer information for my ID ('%s')", s.ID)
	}
	portOffset := myPeer.PortOffset
	myHost := myPeer.Address

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

	startArangod := func(serverPortOffset int, mode string, restart int) (Process, error) {
		myPort := s.MasterPort + portOffset + serverPortOffset
		myHostDir := filepath.Join(s.DataDir, fmt.Sprintf("%s%d", mode, myPort))
		os.MkdirAll(filepath.Join(myHostDir, "data"), 0755)
		os.MkdirAll(filepath.Join(myHostDir, "apps"), 0755)

		// Check if the server is already running
		s.log.Infof("Looking for a running instance of %s on port %d", mode, myPort)
		p, err := runner.GetRunningServer(myHostDir)
		if err != nil {
			return nil, maskAny(err)
		}
		if p != nil {
			s.log.Infof("%s seems to be running already, checking port %d...", mode, myPort)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
			up, _ := s.testInstance(ctx, myHost, myPort)
			cancel()
			if up {
				s.log.Infof("%s is already running on %d. No need to start anything.", mode, myPort)
				return p, nil
			}
			s.log.Infof("%s is not up on port %d. Terminating existing process and restarting it...", mode, myPort)
			p.Terminate()
		}

		s.log.Infof("Starting %s on port %d", mode, myPort)
		myContainerDir := runner.GetContainerDir(myHostDir)
		args, vols := s.makeBaseArgs(myHostDir, myContainerDir, myHost, strconv.Itoa(myPort), mode)
		vols = addDataVolumes(vols, myHostDir, myContainerDir)
		s.writeCommand(filepath.Join(myHostDir, "arangod_command.txt"), executable, args)
		containerNamePrefix := ""
		if s.DockerContainer != "" {
			containerNamePrefix = fmt.Sprintf("%s-", s.DockerContainer)
		}
		containerName := fmt.Sprintf("%s%s-%s-%d-%s-%d", containerNamePrefix, mode, s.ID, restart, myHost, myPort)
		ports := []int{myPort}
		if p, err := runner.Start(args[0], args[1:], vols, ports, containerName, myHostDir); err != nil {
			return nil, maskAny(err)
		} else {
			return p, nil
		}
	}

	runArangod := func(serverPortOffset int, mode string, processVar *Process, runProcess *bool) {
		restart := 0
		recentFailures := 0
		for {
			startTime := time.Now()
			p, err := startArangod(serverPortOffset, mode, restart)
			if err != nil {
				s.log.Errorf("Error while starting %s: %#v", mode, err)
				break
			}
			*processVar = p
			ctx, cancel := context.WithCancel(s.ctx)
			go func() {
				port := s.MasterPort + portOffset + serverPortOffset
				if up, cancelled := s.testInstance(ctx, myHost, port); !cancelled {
					if up {
						s.log.Infof("%s up and running.", mode)
						if mode == "coordinator" {
							hostPort, err := p.HostPort(port)
							if err != nil {
								if id := p.ContainerID(); id != "" {
									s.log.Infof("%s can only be accessed from inside a container.", mode)
								}
							} else {
								scheme := "http"
								arangoshScheme := "tcp"
								if myPeer.IsSecure {
									scheme = "https"
									arangoshScheme = "ssl"
								}
								ip := myPeer.Address
								s.log.Infof("Your cluster can now be accessed with a browser at `%s://%s:%d` or", scheme, ip, hostPort)
								s.log.Infof("using `arangosh --server.endpoint %s://%s:%d`.", arangoshScheme, ip, hostPort)
							}
						}
					} else {
						s.log.Warningf("%s not ready after 5min!", mode)
					}
				}
			}()
			p.Wait()
			cancel()
			uptime := time.Since(startTime)
			if uptime < time.Second*30 {
				recentFailures++
			} else {
				recentFailures = 0
			}

			s.log.Infof("%s has terminated in %s (recent failures: %d)", mode, uptime, recentFailures)
			if s.stop {
				break
			}

			if recentFailures >= maxRecentFailures {
				s.log.Errorf("%s has failed %d times, giving up", mode, recentFailures)
				s.stop = true
				break
			}

			s.log.Infof("restarting %s", mode)
			restart++
		}
	}

	// Start agent:
	if s.needsAgent() {
		runAlways := true
		go runArangod(portOffsetAgent, "agent", &s.servers.agentProc, &runAlways)
	}
	time.Sleep(time.Second)

	// Start DBserver:
	if s.StartDBserver {
		go runArangod(portOffsetDBServer, "dbserver", &s.servers.dbserverProc, &s.StartDBserver)
	}

	time.Sleep(time.Second)

	// Start Coordinator:
	if s.StartCoordinator {
		go runArangod(portOffsetCoordinator, "coordinator", &s.servers.coordinatorProc, &s.StartCoordinator)
	}

	for {
		time.Sleep(time.Second)
		if s.stop {
			break
		}
	}

	s.log.Info("Shutting down services...")
	if p := s.servers.coordinatorProc; p != nil {
		if err := p.Terminate(); err != nil {
			s.log.Warningf("Failed to terminate coordinator: %v", err)
		}
	}
	if p := s.servers.dbserverProc; p != nil {
		if err := p.Terminate(); err != nil {
			s.log.Warningf("Failed to terminate dbserver: %v", err)
		}
	}
	time.Sleep(3 * time.Second)
	if p := s.servers.agentProc; p != nil {
		if err := p.Terminate(); err != nil {
			s.log.Warningf("Failed to terminate agent: %v", err)
		}
	}

	// Cleanup containers
	if p := s.servers.coordinatorProc; p != nil {
		if err := p.Cleanup(); err != nil {
			s.log.Warningf("Failed to cleanup coordinator: %v", err)
		}
	}
	if p := s.servers.dbserverProc; p != nil {
		if err := p.Cleanup(); err != nil {
			s.log.Warningf("Failed to cleanup dbserver: %v", err)
		}
	}
	time.Sleep(3 * time.Second)
	if p := s.servers.agentProc; p != nil {
		if err := p.Cleanup(); err != nil {
			s.log.Warningf("Failed to cleanup agent: %v", err)
		}
	}

	// Cleanup runner
	if err := runner.Cleanup(); err != nil {
		s.log.Warningf("Failed to cleanup runner: %v", err)
	}
}

// Run runs the service in either master or slave mode.
func (s *Service) Run(rootCtx context.Context) {
	s.ctx, s.cancel = context.WithCancel(rootCtx)
	go func() {
		select {
		case <-s.ctx.Done():
			s.stop = true
		}
	}()

	// Find the port mapping if running in a docker container
	if s.DockerContainer != "" {
		if s.OwnAddress == "" {
			s.log.Fatal("OwnAddress must be specified")
		}
		hostPort, isNetHost, err := findDockerExposedAddress(s.DockerEndpoint, s.DockerContainer, s.MasterPort)
		if err != nil {
			s.log.Fatalf("Failed to detect port mapping: %#v", err)
			return
		}
		s.announcePort = hostPort
		s.isNetHost = isNetHost
	} else {
		s.announcePort = s.MasterPort
		s.isNetHost = true // Not running in container so always true
	}

	// Create a runner
	var runner Runner
	if s.DockerEndpoint != "" && s.DockerImage != "" {
		var err error
		runner, err = NewDockerRunner(s.log, s.DockerEndpoint, s.DockerImage, s.DockerUser, s.DockerContainer, s.DockerGCDelay, s.DockerNetHost, s.DockerPrivileged)
		if err != nil {
			s.log.Fatalf("Failed to create docker runner: %#v", err)
		}
		s.log.Debug("Using docker runner")
		// Set executables to their image path's
		s.ArangodExecutable = "/usr/sbin/arangod"
		s.ArangodJSstartup = "/usr/share/arangodb3/js"
		// Docker setup uses different volumes with same dataDir, allow that
		s.allowSameDataDir = true
	} else {
		if s.RunningInDocker {
			s.log.Fatalf("When running in docker, you must provide a --dockerEndpoint=<endpoint> and --docker=<image>")
		}
		runner = NewProcessRunner(s.log)
		s.log.Debug("Using process runner")
	}

	// Is this a new start or a restart?
	if s.relaunch(runner) {
		return
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

// needsAgent returns true if the agent should run in this instance
func (s *Service) needsAgent() bool {
	myPeer, ok := s.myPeers.PeerByID(s.ID)
	return ok && myPeer.HasAgent
}
