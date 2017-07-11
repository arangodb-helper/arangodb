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
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	logging "github.com/op/go-logging"
)

const (
	DefaultMasterPort = 8528
)

// Config holds all configuration for a single service.
type Config struct {
	//AgencySize           int
	ArangodPath          string
	ArangodJSPath        string
	MasterPort           int
	RrPath               string
	DataDir              string
	OwnAddress           string // IP address of used to reach this process
	MasterAddress        string
	Verbose              bool
	ServerThreads        int  // If set to something other than 0, this will be added to the commandline of each server with `--server.threads`...
	AllPortOffsetsUnique bool // If set, all peers will get a unique port offset. If false (default) only portOffset+peerAddress pairs will be unique.
	PassthroughOptions   []PassthroughOption
	DebugCluster         bool

	DockerContainerName string // Name of the container running this process
	DockerEndpoint      string // Where to reach the docker daemon
	DockerImage         string // Name of Arangodb docker image
	DockerStarterImage  string
	DockerUser          string
	DockerGCDelay       time.Duration
	DockerNetworkMode   string
	DockerPrivileged    bool
	DockerTTY           bool
	RunningInDocker     bool

	ProjectVersion string
	ProjectBuild   string
}

// Service implements the actual starter behavior of the ArangoDB starter.
type Service struct {
	Config
	id                  string      // Unique identifier of this peer
	mode                ServiceMode // Service mode cluster|single
	startedLocalSlaves  bool
	jwtSecret           string // JWT secret used for arangod communication
	sslKeyFile          string // Path containing an x509 certificate + private key to be used by the servers.
	log                 *logging.Logger
	ctx                 context.Context
	cancel              context.CancelFunc
	state               State
	myPeers             ClusterConfig
	startRunningWaiter  context.Context
	startRunningTrigger context.CancelFunc
	announcePort        int         // Port I can be reached on from the outside
	tlsConfig           *tls.Config // Server side TLS config (if any)
	isNetHost           bool        // Is this process running in a container with `--net=host` or running outside a container?
	mutex               sync.Mutex  // Mutex used to protect access to this datastructure
	logMutex            sync.Mutex  // Mutex used to synchronize server log output
	allowSameDataDir    bool        // If set, multiple arangdb instances are allowed to have the same dataDir (docker case)
	isLocalSlave        bool
	learnOwnAddress     bool // If set, the HTTP server will update my peer with address information gathered from a /hello request.
	servers             struct {
		agentProc       Process
		dbserverProc    Process
		coordinatorProc Process
		singleProc      Process
	}
	stop bool
}

// NewService creates a new Service instance from the given config.
func NewService(log *logging.Logger, config Config, isLocalSlave bool) (*Service, error) {
	ctx, trigger := context.WithCancel(context.Background())
	return &Service{
		Config:              config,
		log:                 log,
		state:               stateStart,
		startRunningWaiter:  ctx,
		startRunningTrigger: trigger,
		isLocalSlave:        isLocalSlave,
	}, nil
}

// State of the service.
type State int

const (
	stateStart   State = iota // initial state after start
	stateMaster               // finding phase, first instance
	stateSlave                // finding phase, further instances
	stateRunning              // running phase
)

const (
	_portOffsetCoordinator = 1 // Coordinator/single server
	_portOffsetDBServer    = 2
	_portOffsetAgent       = 3
	portOffsetIncrement    = 5 // {our http server, agent, coordinator, dbserver, reserved}
)

const (
	minRecentFailuresForLog = 2   // Number of recent failures needed before a log file is shown.
	maxRecentFailures       = 100 // Maximum number of recent failures before the starter gives up.
)

const (
	confFileName = "arangod.conf"
	logFileName  = "arangod.log"
)

// IsSecure returns true when the cluster is using SSL for connections, false otherwise.
func (s *Service) IsSecure() bool {
	if s.sslKeyFile != "" {
		return true
	}
	myPeer, found := s.myPeers.PeerByID(s.id)
	return found && myPeer.IsSecure
}

// serverPort returns the port number on which my server of given type will listen.
func (s *Service) serverPort(serverType ServerType) (int, error) {
	myPeer, found := s.myPeers.PeerByID(s.id)
	if !found {
		// Cannot find my own peer.
		return 0, maskAny(fmt.Errorf("Cannot find peer %s", s.id))
	}
	// Find log path
	portOffset := myPeer.PortOffset
	return s.MasterPort + portOffset + serverType.PortOffset(), nil
}

// serverHostDir returns the path of the folder (in host namespace) containing data for the given server.
func (s *Service) serverHostDir(serverType ServerType) (string, error) {
	myPort, err := s.serverPort(serverType)
	if err != nil {
		return "", maskAny(err)
	}
	return filepath.Join(s.DataDir, fmt.Sprintf("%s%d", serverType, myPort)), nil
}

// serverExecutable returns the path of the server's executable.
func (s *Service) serverExecutable() string {
	if s.RrPath != "" {
		return s.RrPath
	}
	return s.ArangodPath
}

// StatusItem contain a single point in time for a status feedback channel.
type StatusItem struct {
	PrevStatusCode int
	StatusCode     int
	Duration       time.Duration
}

// TestInstance checks the `up` status of an arangod server instance.
func (s *Service) TestInstance(ctx context.Context, address string, port int, statusChanged chan StatusItem) (up bool, version string, statusTrail []int, cancelled bool) {
	instanceUp := make(chan string)
	statusCodes := make(chan int)
	if statusChanged != nil {
		defer close(statusChanged)
	}
	go func() {
		defer close(instanceUp)
		defer close(statusCodes)
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
		makeRequest := func() (string, int, error) {
			addr := net.JoinHostPort(address, strconv.Itoa(port))
			url := fmt.Sprintf("%s://%s/_api/version", scheme, addr)
			req, err := http.NewRequest("GET", url, nil)
			if err != nil {
				return "", -1, maskAny(err)
			}
			if err := addJwtHeader(req, s.jwtSecret); err != nil {
				return "", -2, maskAny(err)
			}
			resp, err := client.Do(req)
			if err != nil {
				return "", -3, maskAny(err)
			}
			if resp.StatusCode != 200 {
				return "", resp.StatusCode, maskAny(fmt.Errorf("Invalid status %d", resp.StatusCode))
			}
			versionResponse := struct {
				Version string `json:"version"`
			}{}
			defer resp.Body.Close()
			decoder := json.NewDecoder(resp.Body)
			if err := decoder.Decode(&versionResponse); err != nil {
				return "", -4, maskAny(fmt.Errorf("Unexpected version response: %#v", err))
			}
			return versionResponse.Version, resp.StatusCode, nil
		}

		for i := 0; i < 300; i++ {
			if version, statusCode, err := makeRequest(); err == nil {
				instanceUp <- version
				break
			} else {
				statusCodes <- statusCode
			}
			time.Sleep(time.Millisecond * 500)
		}
		instanceUp <- ""
	}()
	statusTrail = make([]int, 0, 16)
	startTime := time.Now()
	for {
		select {
		case version := <-instanceUp:
			return version != "", version, statusTrail, false
		case statusCode := <-statusCodes:
			lastStatusCode := math.MinInt32
			if len(statusTrail) > 0 {
				lastStatusCode = statusTrail[len(statusTrail)-1]
			}
			if len(statusTrail) == 0 || lastStatusCode != statusCode {
				statusTrail = append(statusTrail, statusCode)
			}
			if statusChanged != nil {
				statusChanged <- StatusItem{
					PrevStatusCode: lastStatusCode,
					StatusCode:     statusCode,
					Duration:       time.Since(startTime),
				}
			}
		case <-ctx.Done():
			return false, "", statusTrail, true
		}
	}
}

// startArangod starts a single Arango server of the given type.
func (s *Service) startArangod(runner Runner, bsCfg BootstrapConfig, myHostAddress string, serverType ServerType, restart int) (Process, bool, error) {
	myPort, err := s.serverPort(serverType)
	if err != nil {
		return nil, false, maskAny(err)
	}
	myHostDir, err := s.serverHostDir(serverType)
	if err != nil {
		return nil, false, maskAny(err)
	}
	os.MkdirAll(filepath.Join(myHostDir, "data"), 0755)
	os.MkdirAll(filepath.Join(myHostDir, "apps"), 0755)

	// Check if the server is already running
	s.log.Infof("Looking for a running instance of %s on port %d", serverType, myPort)
	p, err := runner.GetRunningServer(myHostDir)
	if err != nil {
		return nil, false, maskAny(err)
	}
	if p != nil {
		s.log.Infof("%s seems to be running already, checking port %d...", serverType, myPort)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
		up, _, _, _ := s.TestInstance(ctx, myHostAddress, myPort, nil)
		cancel()
		if up {
			s.log.Infof("%s is already running on %d. No need to start anything.", serverType, myPort)
			return p, false, nil
		}
		s.log.Infof("%s is not up on port %d. Terminating existing process and restarting it...", serverType, myPort)
		p.Terminate()
	}

	// Check availability of port
	if !IsPortOpen(myPort) {
		return nil, true, maskAny(fmt.Errorf("Cannot start %s, because port %d is already in use", serverType, myPort))
	}

	s.log.Infof("Starting %s on port %d", serverType, myPort)
	myContainerDir := runner.GetContainerDir(myHostDir, dockerDataDir)
	// Create/read arangod.conf
	confVolumes, config, err := createArangodConf(s.log, bsCfg, myHostDir, myContainerDir, strconv.Itoa(myPort), serverType)
	if err != nil {
		return nil, false, maskAny(err)
	}
	// Create arangod command line arguments
	args := s.createArangodArgs(myContainerDir, myHostAddress, strconv.Itoa(myPort), serverType, config)
	s.writeCommand(filepath.Join(myHostDir, "arangod_command.txt"), s.serverExecutable(), args)
	// Collect volumes
	configVolumes := collectConfigVolumes(config)
	vols := addVolume(append(confVolumes, configVolumes...), myHostDir, myContainerDir, false)
	// Start process/container
	containerNamePrefix := ""
	if s.DockerContainerName != "" {
		containerNamePrefix = fmt.Sprintf("%s-", s.DockerContainerName)
	}
	containerName := fmt.Sprintf("%s%s-%s-%d-%s-%d", containerNamePrefix, serverType, s.id, restart, myHostAddress, myPort)
	ports := []int{myPort}
	if p, err := runner.Start(args[0], args[1:], vols, ports, containerName, myHostDir); err != nil {
		return nil, false, maskAny(err)
	} else {
		return p, false, nil
	}
}

// showRecentLogs dumps the most recent log lines of the server of given type to the console.
func (s *Service) showRecentLogs(serverType ServerType) {
	myHostDir, err := s.serverHostDir(serverType)
	if err != nil {
		s.log.Errorf("Cannot find server host dir: %#v", err)
		return
	}
	logPath := filepath.Join(myHostDir, logFileName)
	logFile, err := os.Open(logPath)
	if os.IsNotExist(err) {
		s.log.Infof("Log file for %s is empty", serverType)
	} else if err != nil {
		s.log.Errorf("Cannot open log file for %s: %#v", serverType, err)
	} else {
		defer logFile.Close()
		rd := bufio.NewReader(logFile)
		lines := [20]string{}
		maxLines := 0
		for {
			line, err := rd.ReadString('\n')
			if line != "" || err == nil {
				copy(lines[1:], lines[0:])
				lines[0] = line
				if maxLines < len(lines) {
					maxLines++
				}
			}
			if err != nil {
				break
			}
		}
		s.logMutex.Lock()
		defer s.logMutex.Unlock()
		s.log.Infof("## Start of %s log", serverType)
		for i := maxLines - 1; i >= 0; i-- {
			fmt.Println("\t" + strings.TrimSuffix(lines[i], "\n"))
		}
		s.log.Infof("## End of %s log", serverType)
	}
}

// runArangod starts a single Arango server of the given type and keeps restarting it when needed.
func (s *Service) runArangod(runner Runner, bsCfg BootstrapConfig, myPeer Peer, serverType ServerType, processVar *Process) {
	restart := 0
	recentFailures := 0
	for {
		myHostAddress := myPeer.Address
		startTime := time.Now()
		p, portInUse, err := s.startArangod(runner, bsCfg, myHostAddress, serverType, restart)
		if err != nil {
			s.log.Errorf("Error while starting %s: %#v", serverType, err)
			if !portInUse {
				break
			}
		} else {
			*processVar = p
			ctx, cancel := context.WithCancel(s.ctx)
			go func() {
				port, err := s.serverPort(serverType)
				if err != nil {
					s.log.Fatalf("Cannot collect serverPort: %#v", err)
				}
				statusChanged := make(chan StatusItem)
				go func() {
					showLogDuration := time.Minute
					for {
						statusItem, ok := <-statusChanged
						if ok {
							if statusItem.PrevStatusCode != statusItem.StatusCode {
								if s.DebugCluster {
									s.log.Infof("%s status changed to %d", serverType, statusItem.StatusCode)
								} else {
									s.log.Debugf("%s status changed to %d", serverType, statusItem.StatusCode)
								}
							}
							if statusItem.Duration > showLogDuration {
								showLogDuration = statusItem.Duration + time.Second*30
								s.showRecentLogs(serverType)
							}
						}
					}
				}()
				if up, version, statusTrail, cancelled := s.TestInstance(ctx, myHostAddress, port, statusChanged); !cancelled {
					if up {
						s.log.Infof("%s up and running (version %s).", serverType, version)
						if (serverType == ServerTypeCoordinator && !s.isLocalSlave) || serverType == ServerTypeSingle {
							hostPort, err := p.HostPort(port)
							if err != nil {
								if id := p.ContainerID(); id != "" {
									s.log.Infof("%s can only be accessed from inside a container.", serverType)
								}
							} else {
								ip := myPeer.Address
								urlSchemes := NewURLSchemes(myPeer.IsSecure)
								what := "cluster"
								if serverType == ServerTypeSingle {
									what = "single server"
								}
								s.logMutex.Lock()
								s.log.Infof("Your %s can now be accessed with a browser at `%s://%s:%d` or", what, urlSchemes.Browser, ip, hostPort)
								s.log.Infof("using `arangosh --server.endpoint %s://%s:%d`.", urlSchemes.ArangoSH, ip, hostPort)
								s.logMutex.Unlock()
							}
						}
					} else {
						s.log.Warningf("%s not ready after 5min!: Status trail: %#v", serverType, statusTrail)
					}
				}
			}()
			p.Wait()
			cancel()
		}
		uptime := time.Since(startTime)
		var isRecentFailure bool
		if uptime < time.Second*30 {
			recentFailures++
			isRecentFailure = true
		} else {
			recentFailures = 0
			isRecentFailure = false
		}

		if isRecentFailure {
			if !portInUse {
				s.log.Infof("%s has terminated, quickly, in %s (recent failures: %d)", serverType, uptime, recentFailures)
				if recentFailures >= minRecentFailuresForLog && s.DebugCluster {
					// Show logs of the server
					s.showRecentLogs(serverType)
				}
			}
			if recentFailures >= maxRecentFailures {
				s.log.Errorf("%s has failed %d times, giving up", serverType, recentFailures)
				s.stop = true
				break
			}
		} else {
			s.log.Infof("%s has terminated", serverType)
		}
		if portInUse {
			time.Sleep(time.Second)
		}

		if s.stop {
			break
		}

		s.log.Infof("restarting %s", serverType)
		restart++
	}
}

// startRunning starts all relevant servers and keeps the running.
func (s *Service) startRunning(runner Runner, bsCfg BootstrapConfig) {
	s.state = stateRunning
	myPeer, ok := s.myPeers.PeerByID(s.id)
	if !ok {
		s.log.Fatalf("Cannot find peer information for my ID ('%s')", s.id)
	}

	if s.mode.IsClusterMode() {
		// Start agent:
		if myPeer.HasAgent() {
			go s.runArangod(runner, bsCfg, myPeer, ServerTypeAgent, &s.servers.agentProc)
			time.Sleep(time.Second)
		}

		// Start DBserver:
		if bsCfg.StartDBserver == nil || *bsCfg.StartDBserver {
			go s.runArangod(runner, bsCfg, myPeer, ServerTypeDBServer, &s.servers.dbserverProc)
			time.Sleep(time.Second)
		}

		// Start Coordinator:
		if bsCfg.StartCoordinator == nil || *bsCfg.StartCoordinator {
			go s.runArangod(runner, bsCfg, myPeer, ServerTypeCoordinator, &s.servers.coordinatorProc)
		}
	} else if s.mode.IsSingleMode() {
		// Start Single server:
		go s.runArangod(runner, bsCfg, myPeer, ServerTypeSingle, &s.servers.singleProc)
	}

	for {
		time.Sleep(time.Second)
		if s.stop {
			break
		}
	}

	s.log.Info("Shutting down services...")
	if p := s.servers.singleProc; p != nil {
		if err := p.Terminate(); err != nil {
			s.log.Warningf("Failed to terminate single server: %v", err)
		}
	}
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
	if p := s.servers.agentProc; p != nil {
		time.Sleep(3 * time.Second)
		if err := p.Terminate(); err != nil {
			s.log.Warningf("Failed to terminate agent: %v", err)
		}
	}

	// Cleanup containers
	if p := s.servers.singleProc; p != nil {
		if err := p.Cleanup(); err != nil {
			s.log.Warningf("Failed to cleanup single server: %v", err)
		}
	}
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
	if p := s.servers.agentProc; p != nil {
		time.Sleep(3 * time.Second)
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
func (s *Service) Run(rootCtx context.Context, bsCfg BootstrapConfig, myPeers ClusterConfig, shouldRelaunch bool) error {
	s.ctx, s.cancel = context.WithCancel(rootCtx)
	go func() {
		select {
		case <-s.ctx.Done():
			s.stop = true
		}
	}()

	// Load settings from BootstrapConfig
	s.id = bsCfg.ID
	s.mode = bsCfg.Mode
	s.startedLocalSlaves = bsCfg.StartLocalSlaves
	s.jwtSecret = bsCfg.JwtSecret
	s.sslKeyFile = bsCfg.SslKeyFile

	// Check mode & flags
	if bsCfg.Mode.IsClusterMode() {
		if bsCfg.AgencySize < 1 {
			return maskAny(fmt.Errorf("AgentSize must be >= 1"))
		}
	} else if bsCfg.Mode.IsSingleMode() {
		bsCfg.AgencySize = 1
	} else {
		return maskAny(fmt.Errorf("Unknown mode '%s'", bsCfg.Mode))
	}

	// Load certificates (if needed)
	if bsCfg.SslKeyFile != "" {
		cert, err := LoadKeyFile(bsCfg.SslKeyFile)
		if err != nil {
			return maskAny(err)
		}
		s.tlsConfig = &tls.Config{
			Certificates: []tls.Certificate{cert},
		}
	}

	// Decide what type of process runner to use.
	useDockerRunner := s.DockerEndpoint != "" && s.DockerImage != ""

	// Guess own IP address if not specified
	if s.OwnAddress == "" && bsCfg.Mode.IsSingleMode() && !useDockerRunner {
		addr, err := GuessOwnAddress()
		if err != nil {
			s.log.Fatalf("starter.address must be specified, it cannot be guessed because: %v", err)
		}
		s.log.Infof("Using auto-detected starter.address: %s", addr)
		s.OwnAddress = addr
	}

	// Find the port mapping if running in a docker container
	if s.RunningInDocker {
		if s.OwnAddress == "" {
			s.log.Fatal("starter.address must be specified")
		}
		if s.DockerContainerName == "" {
			s.log.Fatal("docker.container must be specified")
		}
		if s.DockerEndpoint == "" {
			s.log.Fatal("docker.endpoint must be specified")
		}
		hostPort, isNetHost, networkMode, hasTTY, err := findDockerExposedAddress(s.DockerEndpoint, s.DockerContainerName, s.MasterPort)
		if err != nil {
			s.log.Fatalf("Failed to detect port mapping: %#v", err)
			return maskAny(err)
		}
		if s.DockerNetworkMode == "" && networkMode != "" && networkMode != "default" {
			s.log.Infof("Auto detected network mode: %s", networkMode)
			s.DockerNetworkMode = networkMode
		}
		s.announcePort = hostPort
		s.isNetHost = isNetHost
		if !hasTTY {
			s.DockerTTY = false
		}
	} else {
		s.announcePort = s.MasterPort
		s.isNetHost = true // Not running in container so always true
	}

	// Create a runner
	var runner Runner
	if useDockerRunner {
		var err error
		runner, err = NewDockerRunner(s.log, s.DockerEndpoint, s.DockerImage, s.DockerUser, s.DockerContainerName, s.DockerGCDelay, s.DockerNetworkMode, s.DockerPrivileged, s.DockerTTY)
		if err != nil {
			s.log.Fatalf("Failed to create docker runner: %#v", err)
		}
		s.log.Debug("Using docker runner")
		// Set executables to their image path's
		s.ArangodPath = "/usr/sbin/arangod"
		s.ArangodJSPath = "/usr/share/arangodb3/js"
		// Docker setup uses different volumes with same dataDir, allow that
		s.allowSameDataDir = true
	} else {
		if s.RunningInDocker {
			s.log.Fatalf("When running in docker, you must provide a --docker.endpoint=<endpoint> and --docker.image=<image>")
		}
		runner = NewProcessRunner(s.log)
		s.log.Debug("Using process runner")
	}

	// Is this a new start or a restart?
	if shouldRelaunch {
		s.myPeers = myPeers
		s.log.Infof("Relaunching service with id '%s' on %s:%d...", s.id, s.OwnAddress, s.announcePort)
		s.startHTTPServer()
		wg := &sync.WaitGroup{}
		if bsCfg.StartLocalSlaves {
			s.startLocalSlaves(wg, bsCfg, myPeers.Peers)
		}
		s.startRunning(runner, bsCfg)
		wg.Wait()
	} else {
		// Bootstrap new cluster
		// Do we have to register?
		if s.MasterAddress != "" {
			s.state = stateSlave
			s.bootstrapSlave(s.MasterAddress, runner, bsCfg)
		} else {
			s.state = stateMaster
			s.bootstrapMaster(runner, bsCfg)
		}
	}

	return nil
}
