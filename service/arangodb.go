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
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
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

const (
	DefaultMasterPort = 8528
)

// Config holds all configuration for a single service.
type Config struct {
	ID                   string // Unique identifier of this peer
	Mode                 string // Service mode cluster|single
	AgencySize           int
	ArangodPath          string
	ArangodJSPath        string
	MasterPort           int
	RrPath               string
	StartCoordinator     bool
	StartDBserver        bool
	StartLocalSlaves     bool // If set, start sufficient slave (Service's) locally.
	DataDir              string
	OwnAddress           string // IP address of used to reach this process
	MasterAddress        string
	Verbose              bool
	ServerThreads        int    // If set to something other than 0, this will be added to the commandline of each server with `--server.threads`...
	ServerStorageEngine  string // mmfiles | rocksdb
	AllPortOffsetsUnique bool   // If set, all peers will get a unique port offset. If false (default) only portOffset+peerAddress pairs will be unique.
	JwtSecret            string
	SslKeyFile           string // Path containing an x509 certificate + private key to be used by the servers.
	SslCAFile            string // Path containing an x509 CA certificate used to authenticate clients.

	DockerContainerName string // Name of the container running this process
	DockerEndpoint      string // Where to reach the docker daemon
	DockerImage         string // Name of Arangodb docker image
	DockerStarterImage  string
	DockerUser          string
	DockerGCDelay       time.Duration
	DockerNetworkMode   string
	DockerPrivileged    bool
	RunningInDocker     bool

	ProjectVersion string
	ProjectBuild   string
}

// Service implements the actual starter behavior of the ArangoDB starter.
type Service struct {
	Config
	log                 *logging.Logger
	ctx                 context.Context
	cancel              context.CancelFunc
	state               State
	myPeers             peers
	startRunningWaiter  context.Context
	startRunningTrigger context.CancelFunc
	announcePort        int         // Port I can be reached on from the outside
	tlsConfig           *tls.Config // Server side TLS config (if any)
	isNetHost           bool        // Is this process running in a container with `--net=host` or running outside a container?
	mutex               sync.Mutex  // Mutex used to protect access to this datastructure
	logMutex            sync.Mutex  // Mutex used to synchronize server log output
	allowSameDataDir    bool        // If set, multiple arangdb instances are allowed to have the same dataDir (docker case)
	isLocalSlave        bool
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
	// Create unique ID
	if config.ID == "" {
		var err error
		config.ID, err = createUniqueID()
		if err != nil {
			return nil, maskAny(err)
		}
	}

	// Check mode & flags
	switch config.Mode {
	case "cluster":
		if config.AgencySize < 3 {
			return nil, maskAny(fmt.Errorf("AgentSize must be >= 3"))
		}
	case "single":
		config.AgencySize = 1
	default:
		return nil, maskAny(fmt.Errorf("Unknown mode '%s'", config.Mode))
	}

	// Load certificates (if needed)
	var tlsConfig *tls.Config
	if config.SslKeyFile != "" {
		cert, err := LoadKeyFile(config.SslKeyFile)
		if err != nil {
			return nil, maskAny(err)
		}
		tlsConfig = &tls.Config{
			Certificates: []tls.Certificate{cert},
		}
	}

	ctx, trigger := context.WithCancel(context.Background())
	return &Service{
		Config:              config,
		log:                 log,
		state:               stateStart,
		startRunningWaiter:  ctx,
		startRunningTrigger: trigger,
		isLocalSlave:        isLocalSlave,
		tlsConfig:           tlsConfig,
	}, nil
}

// createUniqueID creates a new random ID.
func createUniqueID() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", maskAny(err)
	}
	return hex.EncodeToString(b), nil
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

// normalizeHostName normalizes all loopback addresses to "localhost"
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

// serverPort returns the port number on which my server of given type will listen.
func (s *Service) serverPort(serverType ServerType) (int, error) {
	myPeer, found := s.myPeers.PeerByID(s.ID)
	if !found {
		// Cannot find my own peer.
		return 0, maskAny(fmt.Errorf("Cannot find peer %s", s.ID))
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

// testInstance checks the `up` status of an arangod server instance.
func (s *Service) testInstance(ctx context.Context, address string, port int) (up bool, version string, cancelled bool) {
	instanceUp := make(chan string)
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
		makeRequest := func() (string, error) {
			addr := net.JoinHostPort(address, strconv.Itoa(port))
			url := fmt.Sprintf("%s://%s/_api/version", scheme, addr)
			req, err := http.NewRequest("GET", url, nil)
			if err != nil {
				return "", maskAny(err)
			}
			if err := addJwtHeader(req, s.JwtSecret); err != nil {
				return "", maskAny(err)
			}
			resp, err := client.Do(req)
			if err != nil {
				return "", maskAny(err)
			}
			if resp.StatusCode != 200 {
				return "", maskAny(fmt.Errorf("Invalid status %d", resp.StatusCode))
			}
			versionResponse := struct {
				Version string `json:"version"`
			}{}
			defer resp.Body.Close()
			decoder := json.NewDecoder(resp.Body)
			if err := decoder.Decode(&versionResponse); err != nil {
				return "", maskAny(fmt.Errorf("Unexpected version response: %#v", err))
			}
			return versionResponse.Version, nil
		}

		for i := 0; i < 300; i++ {
			if version, err := makeRequest(); err == nil {
				instanceUp <- version
				break
			}
			time.Sleep(time.Millisecond * 500)
		}
		instanceUp <- ""
	}()
	select {
	case version := <-instanceUp:
		return version != "", version, false
	case <-ctx.Done():
		return false, "", true
	}
}

// makeBaseArgs returns the command line arguments needed to run an arangod server of given type.
func (s *Service) makeBaseArgs(myHostDir, myContainerDir string, myAddress string, myPort string, serverType ServerType) (args []string, configVolumes []Volume) {
	hostConfFileName := filepath.Join(myHostDir, confFileName)
	containerConfFileName := filepath.Join(myContainerDir, confFileName)
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
		switch serverType {
		// Parameters are: port, server threads, log level, v8-contexts
		case ServerTypeAgent:
			threads = "8"
			v8Contexts = "1"
		case ServerTypeDBServer:
			threads = "4"
			v8Contexts = "4"
		case ServerTypeCoordinator, ServerTypeSingle:
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
		if s.ServerStorageEngine == "rocksdb" {
			serverSection.Settings["storage-engine"] = "rocksdb"
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
	executable := s.ArangodPath
	jsStartup := s.ArangodJSPath
	if s.RrPath != "" {
		args = append(args, s.RrPath)
	}
	args = append(args,
		executable,
		"-c", slasher(containerConfFileName),
		"--database.directory", slasher(filepath.Join(myContainerDir, "data")),
		"--javascript.startup-directory", slasher(jsStartup),
		"--javascript.app-path", slasher(filepath.Join(myContainerDir, "apps")),
		"--log.file", slasher(filepath.Join(myContainerDir, logFileName)),
		"--log.force-direct", "false",
	)
	if s.ServerThreads != 0 {
		args = append(args, "--server.threads", strconv.Itoa(s.ServerThreads))
	}
	myTCPURL := scheme + "://" + net.JoinHostPort(myAddress, myPort)
	switch serverType {
	case ServerTypeAgent:
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
					fmt.Sprintf("%s://%s", scheme, net.JoinHostPort(p.Address, strconv.Itoa(s.MasterPort+p.PortOffset+_portOffsetAgent))),
				)
			}
		}
	case ServerTypeDBServer:
		args = append(args,
			"--cluster.my-address", myTCPURL,
			"--cluster.my-role", "PRIMARY",
			"--cluster.my-local-info", myTCPURL,
			"--foxx.queues", "false",
			"--server.statistics", "true",
		)
	case ServerTypeCoordinator:
		args = append(args,
			"--cluster.my-address", myTCPURL,
			"--cluster.my-role", "COORDINATOR",
			"--cluster.my-local-info", myTCPURL,
			"--foxx.queues", "true",
			"--server.statistics", "true",
		)
	case ServerTypeSingle:
		args = append(args,
			"--foxx.queues", "true",
			"--server.statistics", "true",
		)
	}
	if serverType != ServerTypeAgent && serverType != ServerTypeSingle {
		for i := 0; i < s.AgencySize; i++ {
			p := s.myPeers.Peers[i]
			args = append(args,
				"--cluster.agency-endpoint",
				fmt.Sprintf("%s://%s", scheme, net.JoinHostPort(p.Address, strconv.Itoa(s.MasterPort+p.PortOffset+_portOffsetAgent))),
			)
		}
	}
	return
}

// writeCommand writes the command used to start a server in a file with given path.
func (s *Service) writeCommand(filename string, executable string, args []string) {
	content := strings.Join(args, " \\\n") + "\n"
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		if err := ioutil.WriteFile(filename, []byte(content), 0755); err != nil {
			s.log.Errorf("Failed to write command to %s: %#v", filename, err)
		}
	}
}

// addDataVolumes extends the list of volumes with given host+container pair if running on linux.
func addDataVolumes(configVolumes []Volume, hostPath, containerPath string) []Volume {
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

// startArangod starts a single Arango server of the given type.
func (s *Service) startArangod(runner Runner, myHostAddress string, serverType ServerType, restart int) (Process, bool, error) {
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
		up, _, _ := s.testInstance(ctx, myHostAddress, myPort)
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
	myContainerDir := runner.GetContainerDir(myHostDir)
	args, vols := s.makeBaseArgs(myHostDir, myContainerDir, myHostAddress, strconv.Itoa(myPort), serverType)
	vols = addDataVolumes(vols, myHostDir, myContainerDir)
	s.writeCommand(filepath.Join(myHostDir, "arangod_command.txt"), s.serverExecutable(), args)
	containerNamePrefix := ""
	if s.DockerContainerName != "" {
		containerNamePrefix = fmt.Sprintf("%s-", s.DockerContainerName)
	}
	containerName := fmt.Sprintf("%s%s-%s-%d-%s-%d", containerNamePrefix, serverType, s.ID, restart, myHostAddress, myPort)
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
func (s *Service) runArangod(runner Runner, myPeer Peer, serverType ServerType, processVar *Process, runProcess_ *bool) {
	restart := 0
	recentFailures := 0
	for {
		myHostAddress := myPeer.Address
		startTime := time.Now()
		p, portInUse, err := s.startArangod(runner, myHostAddress, serverType, restart)
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
				if up, version, cancelled := s.testInstance(ctx, myHostAddress, port); !cancelled {
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
						s.log.Warningf("%s not ready after 5min!", serverType)
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
				if recentFailures >= minRecentFailuresForLog {
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
func (s *Service) startRunning(runner Runner) {
	s.state = stateRunning
	myPeer, ok := s.myPeers.PeerByID(s.ID)
	if !ok {
		s.log.Fatalf("Cannot find peer information for my ID ('%s')", s.ID)
	}

	if s.isClusterMode() {
		// Start agent:
		if s.needsAgent() {
			runAlways := true
			go s.runArangod(runner, myPeer, ServerTypeAgent, &s.servers.agentProc, &runAlways)
			time.Sleep(time.Second)
		}

		// Start DBserver:
		if s.StartDBserver {
			go s.runArangod(runner, myPeer, ServerTypeDBServer, &s.servers.dbserverProc, &s.StartDBserver)
			time.Sleep(time.Second)
		}

		// Start Coordinator:
		if s.StartCoordinator {
			go s.runArangod(runner, myPeer, ServerTypeCoordinator, &s.servers.coordinatorProc, &s.StartCoordinator)
		}
	} else if s.isSingleMode() {
		// Start Single server:
		go s.runArangod(runner, myPeer, ServerTypeSingle, &s.servers.singleProc, nil)
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
func (s *Service) Run(rootCtx context.Context) {
	s.ctx, s.cancel = context.WithCancel(rootCtx)
	go func() {
		select {
		case <-s.ctx.Done():
			s.stop = true
		}
	}()

	// Decide what type of process runner to use.
	useDockerRunner := s.DockerEndpoint != "" && s.DockerImage != ""

	// Guess own IP address if not specified
	if s.OwnAddress == "" && s.isSingleMode() && !useDockerRunner {
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
		hostPort, isNetHost, networkMode, err := findDockerExposedAddress(s.DockerEndpoint, s.DockerContainerName, s.MasterPort)
		if err != nil {
			s.log.Fatalf("Failed to detect port mapping: %#v", err)
			return
		}
		if s.DockerNetworkMode == "" && networkMode != "" && networkMode != "default" {
			s.log.Infof("Auto detected network mode: %s", networkMode)
			s.DockerNetworkMode = networkMode
		}
		s.announcePort = hostPort
		s.isNetHost = isNetHost
	} else {
		s.announcePort = s.MasterPort
		s.isNetHost = true // Not running in container so always true
	}

	// Create a runner
	var runner Runner
	if useDockerRunner {
		var err error
		runner, err = NewDockerRunner(s.log, s.DockerEndpoint, s.DockerImage, s.DockerUser, s.DockerContainerName, s.DockerGCDelay, s.DockerNetworkMode, s.DockerPrivileged)
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

// isClusterMode returns true when the service is running in cluster mode.
func (s *Service) isClusterMode() bool {
	return s.Mode == "cluster"
}

// isSingleMode returns true when the service is running in single server mode.
func (s *Service) isSingleMode() bool {
	return s.Mode == "single"
}

// needsAgent returns true if the agent should run in this instance
func (s *Service) needsAgent() bool {
	myPeer, ok := s.myPeers.PeerByID(s.ID)
	return ok && myPeer.HasAgent
}
