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
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
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
	id                   string      // Unique identifier of this peer
	mode                 ServiceMode // Service mode cluster|single
	startedLocalSlaves   bool
	jwtSecret            string // JWT secret used for arangod communication
	sslKeyFile           string // Path containing an x509 certificate + private key to be used by the servers.
	log                  *logging.Logger
	ctx                  context.Context
	cancel               context.CancelFunc
	state                State // Current service state (bootstrapMaster, bootstrapSlave, running)
	myPeers              ClusterConfig
	startRunningWaiter   context.Context
	startRunningTrigger  context.CancelFunc
	announcePort         int         // Port I can be reached on from the outside
	tlsConfig            *tls.Config // Server side TLS config (if any)
	isNetHost            bool        // Is this process running in a container with `--net=host` or running outside a container?
	mutex                sync.Mutex  // Mutex used to protect access to this datastructure
	allowSameDataDir     bool        // If set, multiple arangdb instances are allowed to have the same dataDir (docker case)
	isLocalSlave         bool
	learnOwnAddress      bool // If set, the HTTP server will update my peer with address information gathered from a /hello request.
	runtimeServerManager runtimeServerManager
	stop_                bool
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
	return s.myPeers.IsSecure()
}

// ClusterConfig returns the current cluster configuration and the current peer
func (s *Service) ClusterConfig() (ClusterConfig, Peer, ServiceMode) {
	myPeer, _ := s.myPeers.PeerByID(s.id)
	return s.myPeers, myPeer, s.mode
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
func (s *Config) serverExecutable() string {
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

// IsLocalSlave returns true if this peer is running as a local slave
func (s *Service) IsLocalSlave() bool {
	return s.isLocalSlave
}

// Stop the peer
func (s *Service) Stop() {
	s.cancel()
}

// startRunning starts all relevant servers and keeps the running.
func (s *Service) startRunning(runner Runner, bsCfg BootstrapConfig) {
	// Always start running as slave. Runtime process will elect master
	s.state = stateRunningSlave

	// Ensure we have a valid peer
	if _, ok := s.myPeers.PeerByID(s.id); !ok {
		s.log.Fatalf("Cannot find peer information for my ID ('%s')", s.id)
	}

	// Start the runtime server manager
	s.runtimeServerManager.Run(s.ctx, s.log, s, runner, s.Config, bsCfg)
}

// Run runs the service in either master or slave mode.
func (s *Service) Run(rootCtx context.Context, bsCfg BootstrapConfig, myPeers ClusterConfig, shouldRelaunch bool) error {
	// Prepare a context that is cancelled when we need to stop
	s.ctx, s.cancel = context.WithCancel(rootCtx)

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
			s.state = stateBootstrapSlave
			s.bootstrapSlave(s.MasterAddress, runner, bsCfg)
		} else {
			s.state = stateBootstrapMaster
			s.bootstrapMaster(s.ctx, runner, bsCfg)
		}
	}

	return nil
}
