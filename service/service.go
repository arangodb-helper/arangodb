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

// UseDockerRunner returns true if the docker runner should be used.
// (instead of the local process runner).
func (c Config) UseDockerRunner() bool {
	return c.DockerEndpoint != "" && c.DockerImage != ""
}

// GuessOwnAddress fills in the OwnAddress field if needed and returns an update config.
func (c Config) GuessOwnAddress(log *logging.Logger, bsCfg BootstrapConfig) Config {
	// Guess own IP address if not specified
	if c.OwnAddress == "" && bsCfg.Mode.IsSingleMode() && !c.UseDockerRunner() {
		addr, err := GuessOwnAddress()
		if err != nil {
			log.Fatalf("starter.address must be specified, it cannot be guessed because: %v", err)
		}
		log.Infof("Using auto-detected starter.address: %s", addr)
		c.OwnAddress = addr
	}
	return c
}

// GetNetworkEnvironment loads information about the network environment
// based on the given config and returns an updated config, the announce port and isNetHost.
func (c Config) GetNetworkEnvironment(log *logging.Logger) (Config, int, bool) {
	// Find the port mapping if running in a docker container
	if c.RunningInDocker {
		if c.OwnAddress == "" {
			log.Fatal("starter.address must be specified")
		}
		if c.DockerContainerName == "" {
			log.Fatal("docker.container must be specified")
		}
		if c.DockerEndpoint == "" {
			log.Fatal("docker.endpoint must be specified")
		}
		hostPort, isNetHost, networkMode, hasTTY, err := findDockerExposedAddress(c.DockerEndpoint, c.DockerContainerName, c.MasterPort)
		if err != nil {
			log.Fatalf("Failed to detect port mapping: %#v", err)
		}
		if c.DockerNetworkMode == "" && networkMode != "" && networkMode != "default" {
			log.Infof("Auto detected network mode: %s", networkMode)
			c.DockerNetworkMode = networkMode
		}
		if !hasTTY {
			c.DockerTTY = false
		}
		return c, hostPort, isNetHost
	}
	// Not running as container, so net=host
	return c, c.MasterPort, true
}

// CreateRunner creates a process runner based on given configuration.
// Returns: Runner, updated configuration, allowSameDataDir
func (c Config) CreateRunner(log *logging.Logger) (Runner, Config, bool) {
	var runner Runner
	if c.UseDockerRunner() {
		runner, err := NewDockerRunner(log, c.DockerEndpoint, c.DockerImage, c.DockerUser, c.DockerContainerName,
			c.DockerGCDelay, c.DockerNetworkMode, c.DockerPrivileged, c.DockerTTY)
		if err != nil {
			log.Fatalf("Failed to create docker runner: %#v", err)
		}
		log.Debug("Using docker runner")
		// Set executables to their image path's
		c.ArangodPath = "/usr/sbin/arangod"
		c.ArangodJSPath = "/usr/share/arangodb3/js"
		// Docker setup uses different volumes with same dataDir, allow that
		allowSameDataDir := true

		return runner, c, allowSameDataDir
	}

	// We must not be running in docker
	if c.RunningInDocker {
		log.Fatalf("When running in docker, you must provide a --docker.endpoint=<endpoint> and --docker.image=<image>")
	}

	// Use process runner
	runner = NewProcessRunner(log)
	log.Debug("Using process runner")

	return runner, c, false
}

// Service implements the actual starter behavior of the ArangoDB starter.
type Service struct {
	cfg                Config
	id                 string      // Unique identifier of this peer
	mode               ServiceMode // Service mode cluster|single
	startedLocalSlaves bool
	jwtSecret          string // JWT secret used for arangod communication
	sslKeyFile         string // Path containing an x509 certificate + private key to be used by the servers.
	log                *logging.Logger
	stopPeer           struct {
		ctx     context.Context    // Context to wait on for stopping the entire peer
		trigger context.CancelFunc // Triggers a stop of the entire peer
	}
	state              State // Current service state (bootstrapMaster, bootstrapSlave, running)
	myPeers            ClusterConfig
	bootstrapCompleted struct {
		ctx     context.Context    // Context to wait on for the bootstrap state to be completed. Once trigger the cluster config is complete.
		trigger context.CancelFunc // Triggers the end of the bootstrap state
	}
	announcePort         int         // Port I can be reached on from the outside
	tlsConfig            *tls.Config // Server side TLS config (if any)
	isNetHost            bool        // Is this process running in a container with `--net=host` or running outside a container?
	mutex                sync.Mutex  // Mutex used to protect access to this datastructure
	allowSameDataDir     bool        // If set, multiple arangdb instances are allowed to have the same dataDir (docker case)
	isLocalSlave         bool
	learnOwnAddress      bool // If set, the HTTP server will update my peer with address information gathered from a /hello request.
	runtimeServerManager runtimeServerManager
}

// NewService creates a new Service instance from the given config.
func NewService(ctx context.Context, log *logging.Logger, config Config, isLocalSlave bool) *Service {
	s := &Service{
		cfg:          config,
		log:          log,
		state:        stateStart,
		isLocalSlave: isLocalSlave,
	}
	s.bootstrapCompleted.ctx, s.bootstrapCompleted.trigger = context.WithCancel(ctx)
	return s
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
	return s.cfg.MasterPort + portOffset + serverType.PortOffset(), nil
}

// serverHostDir returns the path of the folder (in host namespace) containing data for the given server.
func (s *Service) serverHostDir(serverType ServerType) (string, error) {
	myPort, err := s.serverPort(serverType)
	if err != nil {
		return "", maskAny(err)
	}
	return filepath.Join(s.cfg.DataDir, fmt.Sprintf("%s%d", serverType, myPort)), nil
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
	s.stopPeer.trigger()
}

// startRunning starts all relevant servers and keeps the running.
func (s *Service) startRunning(runner Runner, config Config, bsCfg BootstrapConfig) {
	// Always start running as slave. Runtime process will elect master
	s.state = stateRunningSlave

	// Ensure we have a valid peer
	if _, ok := s.myPeers.PeerByID(s.id); !ok {
		s.log.Fatalf("Cannot find peer information for my ID ('%s')", s.id)
	}

	// Start the runtime server manager
	s.runtimeServerManager.Run(s.stopPeer.ctx, s.log, s, runner, config, bsCfg)
}

// Run runs the service in either master or slave mode.
func (s *Service) Run(rootCtx context.Context, bsCfg BootstrapConfig, myPeers ClusterConfig, shouldRelaunch bool) error {
	// Prepare a context that is cancelled when we need to stop
	s.stopPeer.ctx, s.stopPeer.trigger = context.WithCancel(rootCtx)

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
	var err error
	if s.tlsConfig, err = bsCfg.CreateTLSConfig(); err != nil {
		return maskAny(err)
	}

	// Guess own IP address if not specified
	s.cfg = s.cfg.GuessOwnAddress(s.log, bsCfg)

	// Find the port mapping if running in a docker container
	s.cfg, s.announcePort, s.isNetHost = s.cfg.GetNetworkEnvironment(s.log)

	// Create a runner
	var runner Runner
	runner, s.cfg, s.allowSameDataDir = s.cfg.CreateRunner(s.log)

	// Is this a new start or a restart?
	if shouldRelaunch {
		s.myPeers = myPeers
		s.log.Infof("Relaunching service with id '%s' on %s:%d...", s.id, s.cfg.OwnAddress, s.announcePort)
		s.startHTTPServer(s.cfg)
		wg := &sync.WaitGroup{}
		if bsCfg.StartLocalSlaves {
			s.startLocalSlaves(wg, s.cfg, bsCfg, myPeers.Peers)
		}
		s.startRunning(runner, s.cfg, bsCfg)
		wg.Wait()
	} else {
		// Bootstrap new cluster
		// Do we have to register?
		if s.cfg.MasterAddress != "" {
			s.state = stateBootstrapSlave
			s.bootstrapSlave(s.cfg.MasterAddress, runner, s.cfg, bsCfg)
		} else {
			s.state = stateBootstrapMaster
			s.bootstrapMaster(s.stopPeer.ctx, runner, s.cfg, bsCfg)
		}
	}

	return nil
}
