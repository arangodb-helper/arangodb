//
// DISCLAIMER
//
// Copyright 2017-2024 ArangoDB GmbH, Cologne, Germany
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

package service

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"

	driver "github.com/arangodb/go-driver"
	"github.com/arangodb/go-driver/agency"
	driver_http "github.com/arangodb/go-driver/http"
	"github.com/arangodb/go-driver/jwt"

	"github.com/arangodb-helper/arangodb/client"
	"github.com/arangodb-helper/arangodb/pkg/definitions"
	"github.com/arangodb-helper/arangodb/pkg/logging"
	"github.com/arangodb-helper/arangodb/pkg/utils"
	"github.com/arangodb-helper/arangodb/service/options"
)

const (
	DefaultMasterPort = 8528
)

// Config holds all configuration for a single service.
type Config struct {
	ArangodPath          string
	ArangodJSPath        string
	ArangoSyncPath       string
	AdvertisedEndpoint   string
	MasterPort           int
	RrPath               string
	DataDir              string
	LogDir               string // Custom directory to which log files are written (default "")
	OwnAddress           string // IP address of used to reach this process
	BindAddress          string // IP address the HTTP server binds to (typically '0.0.0.0')
	MasterAddresses      []string
	Verbose              bool
	ServerThreads        int  // If set to something other than 0, this will be added to the commandline of each server with `--server.threads`...
	AllPortOffsetsUnique bool // If set, all peers will get a unique port offset. If false (default) only portOffset+peerAddress pairs will be unique.
	Configuration        *options.Configuration
	DebugCluster         bool
	LogRotateFilesToKeep int
	LogRotateInterval    time.Duration
	InstanceUpTimeout    time.Duration

	DockerConfig       DockerConfig
	DockerStarterImage string
	RunningInDocker    bool

	SyncEnabled             bool   // If set, arangosync servers are activated
	SyncMasterKeyFile       string // TLS keyfile of local sync master
	SyncMasterClientCAFile  string // CA Certificate used for client certificate verification
	SyncMasterJWTSecretFile string // File containing JWT secret used to access the Sync Master (from Sync Worker)
	SyncMonitoringToken     string // Bearer token used for arangosync --monitoring.token
	SyncMQType              string // MQType used by sync master
	SyncBinaryFoundErr      error  // If set, the sync binary was not found

	ProjectVersion string
	ProjectBuild   string
}

// UseDockerRunner returns true if the docker runner should be used.
// (instead of the local process runner).
func (c Config) UseDockerRunner() bool {
	return c.DockerConfig.Endpoint != "" && c.DockerConfig.ImageArangoD != ""
}

// GuessOwnAddress fills in the OwnAddress field if needed and returns an update config.
func (c Config) GuessOwnAddress(log zerolog.Logger, bsCfg BootstrapConfig) Config {
	// Guess own IP address if not specified
	if c.OwnAddress == "" && bsCfg.Mode.IsSingleMode() && !c.UseDockerRunner() {
		addr, err := GuessOwnAddress()
		if err != nil {
			log.Fatal().Err(err).Msg("starter.address must be specified, it cannot be guessed because")
		}
		log.Info().Msgf("Using auto-detected starter.address: %s", addr)
		c.OwnAddress = addr
	}
	return c
}

// GetNetworkEnvironment loads information about the network environment
// based on the given config and returns an updated config, the announce port and isNetHost.
func (c Config) GetNetworkEnvironment(log zerolog.Logger) (Config, int, bool) {
	// Find the port mapping if running in a docker container
	if c.RunningInDocker {
		if c.OwnAddress == "" {
			log.Fatal().Msg("starter.address must be specified")
		}
		if c.DockerConfig.HostContainerName == "" {
			log.Fatal().Msg("docker.container must be specified")
		}
		if c.DockerConfig.Endpoint == "" {
			log.Fatal().Msg("docker.endpoint must be specified")
		}
		hostPort, isNetHost, networkMode, hasTTY, err := findDockerExposedAddress(c.DockerConfig.Endpoint, c.DockerConfig.HostContainerName, c.MasterPort)
		if err != nil {
			log.Fatal().Err(err).Msg("Failed to detect port mapping")
		}
		if c.DockerConfig.NetworkMode == "" && networkMode != "" && networkMode != "default" && networkMode != "bridge" {
			log.Info().Msgf("Auto detected network mode: %s", networkMode)
			c.DockerConfig.NetworkMode = networkMode
		}
		if !hasTTY {
			c.DockerConfig.TTY = false
		}
		return c, hostPort, isNetHost
	}
	// Not running as container, so net=host
	return c, c.MasterPort, true
}

// CreateRunner creates a process runner based on given configuration.
// Returns: Runner, updated configuration, allowSameDataDir
func (c Config) CreateRunner(log zerolog.Logger) (Runner, Config, bool) {
	var runner Runner
	if c.UseDockerRunner() {
		runner, err := NewDockerRunner(log, c.DockerConfig)
		if err != nil {
			log.Fatal().Err(err).Msg("Failed to create docker runner")
		}
		log.Debug().Msg("Using docker runner")
		// Set executables to their image path's
		c.ArangodPath = "/usr/sbin/arangod"
		c.ArangodJSPath = "/usr/share/arangodb3/js"
		c.ArangoSyncPath = "/usr/sbin/arangosync"
		// Docker setup uses different volumes with same dataDir, allow that
		allowSameDataDir := true

		return runner, c, allowSameDataDir
	}

	// We must not be running in docker
	if c.RunningInDocker {
		log.Fatal().Msg("When running in docker, you must provide a --docker.endpoint=<endpoint> and --docker.image=<image>")
	}

	// Use process runner
	runner = NewProcessRunner(log)
	log.Debug().Msg("Using process runner")

	return runner, c, false
}

// Service implements the actual starter behavior of the ArangoDB starter.
type Service struct {
	cfg                Config
	bsCfg              BootstrapConfig
	id                 string      // Unique identifier of this peer
	mode               ServiceMode // Service mode cluster|single
	startedLocalSlaves bool
	jwtSecret          string // JWT secret used for arangod communication
	sslKeyFile         string // Path containing an x509 certificate + private key to be used by the servers.
	log                zerolog.Logger
	logService         logging.Service
	stopPeer           struct {
		ctx     context.Context    // Context to wait on for stopping the entire peer
		trigger context.CancelFunc // Triggers a stop of the entire peer
	}
	state              State // Current service state (bootstrapMaster, bootstrapSlave, running)
	bootstrapCompleted struct {
		ctx     context.Context    // Context to wait on for the bootstrap state to be completed. Once trigger the cluster config is complete.
		trigger context.CancelFunc // Triggers the end of the bootstrap state
	}
	announcePort          int         // Port I can be reached on from the outside
	tlsConfig             *tls.Config // Server side TLS config (if any)
	isNetHost             bool        // Is this process running in a container with `--net=host` or running outside a container?
	mutex                 sync.Mutex  // Mutex used to protect access to this datastructure
	allowSameDataDir      bool        // If set, multiple arangodb instances are allowed to have the same dataDir (docker case)
	isLocalSlave          bool
	learnOwnAddress       bool   // If set, the HTTP server will update my peer with address information gathered from a /hello request.
	recoveryFile          string // Path of RECOVERY file (if any)
	runner                Runner
	runtimeServerManager  runtimeServerManager
	runtimeClusterManager runtimeClusterManager
	upgradeManager        UpgradeManager
	databaseFeatures      DatabaseFeatures
}

func (s *Service) GetLocalFolder() string {
	return s.bsCfg.JWTFolderDir()
}

// NewService creates a new Service instance from the given config.
func NewService(ctx context.Context, log zerolog.Logger, logService logging.Service, config Config, bsCfg BootstrapConfig, isLocalSlave bool) *Service {
	// Fix up master addresses
	for i, addr := range config.MasterAddresses {
		if !strings.Contains(addr, ":") {
			// Address has no port, add default master port
			config.MasterAddresses[i] = net.JoinHostPort(addr, strconv.Itoa(DefaultMasterPort))
		}
	}
	s := &Service{
		cfg:          config,
		bsCfg:        bsCfg,
		log:          log,
		logService:   logService,
		state:        stateStart,
		isLocalSlave: isLocalSlave,
	}
	s.upgradeManager = NewUpgradeManager(log, s)
	s.bootstrapCompleted.ctx, s.bootstrapCompleted.trigger = context.WithCancel(ctx)
	return s
}

// detectDatabaseFeatures queries the database version and sets the
// databaseFeatures field.
func (s *Service) detectDatabaseFeatures(ctx context.Context) error {
	v, enterprise, err := s.DatabaseVersion(ctx)
	if err != nil {
		return maskAny(err)
	}
	s.databaseFeatures = NewDatabaseFeatures(v, enterprise)
	return nil
}

// DatabaseFeatures returns the detected database features.
func (s *Service) DatabaseFeatures() DatabaseFeatures {
	return s.databaseFeatures
}

// IsSecure returns true when the cluster is using SSL for connections, false otherwise.
func (s *Service) IsSecure() bool {
	if s.sslKeyFile != "" {
		return true
	}
	return s.runtimeClusterManager.myPeers.IsSecure()
}

// ClusterConfig returns the current cluster configuration and the current peer
func (s *Service) ClusterConfig() (ClusterConfig, *Peer, ServiceMode) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if myPeer, found := s.runtimeClusterManager.myPeers.PeerByID(s.id); !found {
		return s.runtimeClusterManager.myPeers, nil, s.mode
	} else {
		return s.runtimeClusterManager.myPeers, &myPeer, s.mode
	}
}

// IsRunningMaster returns if the starter is the running master.
func (s *Service) IsRunningMaster() (isRunningMaster, isRunning bool, masterURL string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	masterURL = s.runtimeClusterManager.GetMasterURL()
	if s.state == stateRunningMaster {
		return true, true, masterURL
	}
	return false, s.state.IsRunning(), masterURL
}

// HandleGoodbye removes the database servers started by the peer with given id
// from the cluster and alters the cluster configuration, removing the peer.
func (s *Service) HandleGoodbye(id string, force bool) (peerRemoved bool, err error) {
	// Find peer
	s.mutex.Lock()
	peer, peerFound := s.runtimeClusterManager.myPeers.PeerByID(id)
	state := s.state
	s.mutex.Unlock()

	// Check state
	if state != stateRunningMaster {
		return false, maskAny(errors.Wrapf(client.PreconditionFailedError, "Invalid state %d", s.state))
	}

	// Check peer
	if !peerFound {
		return false, nil // Peer not found
	}
	if peer.HasAgent() {
		return false, maskAny(errors.Wrap(client.PreconditionFailedError, "Cannot remove peer with agent"))
	}

	// Prepare cluster client
	ctx := context.Background()
	c, err := s.runtimeClusterManager.myPeers.CreateClusterAPI(ctx, s)
	if err != nil {
		return false, maskAny(err)
	}

	// Remove dbserver from cluster (if any)
	if peer.HasDBServer() {
		shutdownServer := func() error {
			// Find id of dbserver
			s.log.Info().Msg("Finding server ID of dbserver")
			sc, err := peer.CreateDBServerAPI(s)
			if err != nil {
				return maskAny(err)
			}
			sid, err := sc.ServerID(ctx)
			if err != nil {
				return maskAny(err)
			}
			// Clean out DB server
			s.log.Info().Msgf("Starting cleanout of dbserver %s", sid)
			if err := c.CleanOutServer(ctx, sid); err != nil {
				s.log.Warn().Err(err).Msgf("Cleanout requested of dbserver %s failed", sid)
				return maskAny(err)
			}
			// Wait until server is cleaned out
			s.log.Info().Msgf("Waiting for cleanout of dbserver %s to finish", sid)
			for {
				if cleanedOut, err := c.IsCleanedOut(ctx, sid); err != nil {
					s.log.Warn().Err(err).Msgf("IsCleanedOut request of dbserver %s failed", sid)
					return maskAny(err)
				} else if cleanedOut {
					break
				}
				// Wait a bit
				time.Sleep(time.Millisecond * 250)
			}
			// Remove dbserver from cluster
			s.log.Info().Msgf("Removing dbserver %s from cluster", sid)
			if err := sc.Shutdown(ctx, true); err != nil {
				s.log.Warn().Err(err).Msgf("Shutdown request of dbserver %s failed", sid)
				return maskAny(err)
			}
			return nil
		}
		if err := shutdownServer(); err != nil {
			if force {
				s.log.Warn().Err(err).Msg("Failed to properly shutdown dbserver, removing peer anyway")
			} else {
				return false, maskAny(err)
			}
		}
	}

	// Remove coordinator from cluster (if any)
	if peer.HasCoordinator() {
		shutdownServer := func() error {
			// Find id of coordinator
			s.log.Info().Msg("Finding server ID of coordinator")
			sc, err := peer.CreateCoordinatorAPI(s)
			if err != nil {
				return maskAny(err)
			}
			sid, err := sc.ServerID(ctx)
			if err != nil {
				return maskAny(err)
			}
			// Remove coordinator from cluster
			s.log.Info().Msgf("Removing coordinator %s from cluster", sid)
			if err := sc.Shutdown(ctx, true); err != nil {
				s.log.Warn().Err(err).Msgf("Shutdown request of coordinator %s failed", sid)
				return maskAny(err)
			}
			return nil
		}
		if err := shutdownServer(); err != nil {
			if force {
				s.log.Warn().Err(err).Msg("Failed to properly shutdown coordinator, removing peer anyway")
			} else {
				return false, maskAny(err)
			}
		}
	}

	// Remove peer from cluster configuration
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.log.Info().Msgf("Removing peer %s from cluster configuration", id)
	s.runtimeClusterManager.myPeers.RemovePeerByID(id)

	// Peer has been removed, update stored config
	s.log.Info().Msgf("Removed peer %s from cluster configuration, saving setup", id)
	if err := s.saveSetup(); err != nil {
		s.log.Error().Err(err).Msg("Failed to save setup")
	}
	return true, nil
}

// sendMasterLeaveCluster informs the master that we're leaving for good.
func (s *Service) sendMasterLeaveCluster() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Check state
	switch s.state {
	case stateRunningMaster:
		// We're the master, try to stop that and return unavailable so the client should try again
		s.runtimeClusterManager.AvoidBeingMaster()
		return maskAny(errors.Wrap(client.ServiceUnavailableError, "Currently running master, giving up being master, please try again"))
	case stateRunningSlave:
	// OK
	default:
		return maskAny(errors.Wrapf(client.PreconditionFailedError, "Invalid state %d", s.state))
	}

	// Build URL
	masterURL := s.runtimeClusterManager.GetMasterURL()
	if masterURL == "" {
		// Return unavailable so the client should retry
		return maskAny(errors.Wrap(client.ServiceUnavailableError, "Running master is not yet known"))
	}
	u, err := getURLWithPath(masterURL, "/goodbye")
	if err != nil {
		return maskAny(err)
	}
	s.log.Info().Msgf("Saying goodbye to master at %s", u)
	req := client.GoodbyeRequest{SlaveID: s.id}
	data, err := json.Marshal(req)
	if err != nil {
		return maskAny(err)
	}
	resp, err := httpClient.Post(u, contentTypeJSON, bytes.NewReader(data))
	if err != nil {
		return maskAny(err)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		return maskAny(client.ParseResponseError(resp, nil))
	}

	// Remove setup.json
	if err := RemoveSetupConfig(s.log, s.cfg.DataDir); err != nil {
		s.log.Warn().Err(err).Msgf("Failed to remove %s", setupFileName)
	}

	return nil
}

// serverPort returns the port number on which my server of given type will listen.
func (s *Service) serverPort(serverType definitions.ServerType) (int, error) {
	myPeer, found := s.runtimeClusterManager.myPeers.PeerByID(s.id)
	if !found {
		// Cannot find my own peer.
		return 0, maskAny(fmt.Errorf("Cannot find peer %s", s.id))
	}
	// Find log path
	portOffset := myPeer.PortOffset
	return myPeer.Port + portOffset + serverType.PortOffset(), nil
}

// serverHostDir returns the path of the folder (in host namespace) containing data for the given server.
func (s *Service) serverHostDir(serverType definitions.ServerType) (string, error) {
	myPort, err := s.serverPort(serverType)
	if err != nil {
		return "", maskAny(err)
	}
	return filepath.Join(s.cfg.DataDir, fmt.Sprintf("%s%d", serverType, myPort)), nil
}

// serverContainerDir returns the path of the folder (in container namespace) containing data for the given server.
func (s *Service) serverContainerDir(serverType definitions.ServerType) (string, error) {
	hostDir, err := s.serverHostDir(serverType)
	if err != nil {
		return "", maskAny(err)
	}
	if s.runner == nil {
		return "", maskAny(fmt.Errorf("Runner is not yet set"))
	}
	return s.runner.GetContainerDir(hostDir, dockerDataDir), nil
}

// serverLogFileNameSuffix returns the suffix used for the log file of given server type.
func (s *Service) serverLogFileNameSuffix(serverType definitions.ServerType) (string, error) {
	if s.cfg.LogDir != "" {
		// Use custom log dir
		port, err := s.serverPort(serverType)
		if err != nil {
			return "", maskAny(err)
		}
		return fmt.Sprintf("-%s-%d", serverType, port), nil
	}
	return "", nil
}

// serverHostLogFile returns the path of the logfile (in host namespace) to which the given server will write its logs.
func (s *Service) serverHostLogFile(serverType definitions.ServerType) (string, error) {
	suffix, err := s.serverLogFileNameSuffix(serverType)
	if err != nil {
		return "", maskAny(err)
	}
	if s.cfg.LogDir != "" {
		// Use custom log dir
		return filepath.Join(s.cfg.LogDir, serverType.ProcessType().LogFileName(suffix)), nil
	}
	hostDir, err := s.serverHostDir(serverType)
	if err != nil {
		return "", maskAny(err)
	}
	return filepath.Join(hostDir, serverType.ProcessType().LogFileName(suffix)), nil
}

// serverContainerLogFile returns the path of the logfile (in container namespace) to which the given server will write its logs.
func (s *Service) serverContainerLogFile(serverType definitions.ServerType) (string, error) {
	suffix, err := s.serverLogFileNameSuffix(serverType)
	if err != nil {
		return "", maskAny(err)
	}
	if s.cfg.LogDir != "" {
		// Use custom log dir.
		// Client has to ensure that directory is mapped into the container
		return filepath.Join(s.cfg.LogDir, serverType.ProcessType().LogFileName(suffix)), nil
	}
	containerDir, err := s.serverContainerDir(serverType)
	if err != nil {
		return "", maskAny(err)
	}
	return filepath.Join(containerDir, serverType.ProcessType().LogFileName(suffix)), nil
}

// UpgradeManager returns the upgrade manager service.
func (s *Service) UpgradeManager() UpgradeManager {
	return s.upgradeManager
}

// StatusItem contain a single point in time for a status feedback channel.
type StatusItem struct {
	PrevStatusCode int
	StatusCode     int
	Duration       time.Duration
}

type instanceUpInfo struct {
	Version  string
	Role     string
	Mode     string
	IsLeader bool
}

// TestInstance checks the `up` status of an arangod server instance.
func (s *Service) TestInstance(ctx context.Context, serverType definitions.ServerType, address string, port int,
	statusChanged chan StatusItem) (up, correctRole bool, version, role, mode string, isLeader bool, statusTrail []int, cancelled bool) {
	instanceUp := make(chan instanceUpInfo)
	statusCodes := make(chan int)
	if statusChanged != nil {
		defer close(statusChanged)
	}
	go func() {
		defer close(instanceUp)
		defer close(statusCodes)
		client := &http.Client{
			Timeout: time.Second * 10,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
				},
			},
		}
		scheme := "http"
		if s.IsSecure() {
			scheme = "https"
		}
		makeArangodVersionRequest := func() (string, int, error) {
			addr := net.JoinHostPort(address, strconv.Itoa(port))

			client, err := s.CreateClient([]string{fmt.Sprintf("%s://%s", scheme, addr)}, ConnectionTypeDatabase, serverType)
			if err != nil {
				return "", -1, maskAny(err)
			}

			c := client.Connection()

			req, err := c.NewRequest("GET", "/_api/version")
			if err != nil {
				return "", -1, maskAny(err)
			}

			resp, err := c.Do(ctx, req)
			if err != nil {
				return "", -3, maskAny(err)
			}
			if resp.StatusCode() != 200 {
				return "", resp.StatusCode(), maskAny(fmt.Errorf("Invalid status %d", resp.StatusCode()))
			}
			versionResponse := struct {
				Version string `json:"version"`
			}{}
			if err := resp.ParseBody("", &versionResponse); err != nil {
				return "", -4, maskAny(fmt.Errorf("Unexpected version response: %#v", err))
			}
			return versionResponse.Version, resp.StatusCode(), nil
		}
		makeArangoSyncVersionRequest := func() (string, int, error) {
			addr := net.JoinHostPort(address, strconv.Itoa(port))
			url := fmt.Sprintf("https://%s/_api/version", addr)
			req, err := http.NewRequest("GET", url, nil)
			if err != nil {
				return "", -1, maskAny(err)
			}
			if err := addBearerTokenHeader(req, s.cfg.SyncMonitoringToken); err != nil {
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
				Build   string `json:"build"`
			}{}
			defer resp.Body.Close()
			decoder := json.NewDecoder(resp.Body)
			if err := decoder.Decode(&versionResponse); err != nil {
				return "", -4, maskAny(fmt.Errorf("Unexpected version response: %#v", err))
			}
			return versionResponse.Version, resp.StatusCode, nil
		}
		makeVersionRequest := func() (string, int, error) {
			switch serverType.ProcessType() {
			case definitions.ProcessTypeArangod:
				return makeArangodVersionRequest()
			case definitions.ProcessTypeArangoSync:
				return makeArangoSyncVersionRequest()
			default:
				return "", 0, maskAny(fmt.Errorf("Unknown process type '%s'", serverType.ProcessType()))
			}
		}
		makeRoleRequest := func() (string, string, int, error) {
			if serverType.ProcessType() == definitions.ProcessTypeArangoSync {
				return "", "", 200, nil
			}
			addr := net.JoinHostPort(address, strconv.Itoa(port))

			client, err := s.CreateClient([]string{fmt.Sprintf("%s://%s", scheme, addr)}, ConnectionTypeDatabase, serverType)
			if err != nil {
				return "", "", -1, maskAny(err)
			}

			c := client.Connection()
			req, err := c.NewRequest("GET", "/_admin/server/role")
			if err != nil {
				return "", "", -1, maskAny(err)
			}

			resp, err := c.Do(ctx, req)
			if err != nil {
				return "", "", -3, maskAny(err)
			}
			if resp.StatusCode() != 200 {
				return "", "", resp.StatusCode(), maskAny(fmt.Errorf("Invalid status %d", resp.StatusCode()))
			}
			roleResponse := struct {
				Role string `json:"role,omitempty"`
				Mode string `json:"mode,omitempty"`
			}{}
			if err := resp.ParseBody("", &roleResponse); err != nil {
				return "", "", -4, maskAny(fmt.Errorf("Unexpected role response: %#v", err))
			}
			return roleResponse.Role, roleResponse.Mode, resp.StatusCode(), nil
		}
		makeIsLeaderRequest := func() (bool, error) {
			if serverType != definitions.ServerTypeResilientSingle {
				return false, nil
			}
			addr := net.JoinHostPort(address, strconv.Itoa(port))

			client, err := s.CreateClient([]string{fmt.Sprintf("%s://%s", scheme, addr)}, ConnectionTypeDatabase, serverType)
			if err != nil {
				return false, maskAny(err)
			}

			c := client.Connection()

			req, err := c.NewRequest("GET", "/_api/database")
			if err != nil {
				return false, maskAny(err)
			}

			resp, err := c.Do(ctx, req)
			if err != nil {
				if driver.IsNoLeader(err) {
					// Server is up, just not the leader
					return false, nil
				}
				return false, maskAny(err)
			}
			if resp.StatusCode() == 200 {
				return true, nil
			}

			var data driver.ArangoError

			if err := resp.ParseBody("", &data); err != nil {
				return false, maskAny(err)
			}

			if IsNoLeaderError(data) {
				// Server is up, just not the leader
				return false, nil
			}

			return false, maskAny(fmt.Errorf("Invalid status %d", resp.StatusCode()))
		}

		checkInstanceOnce := func() bool {
			if version, statusCode, err := makeVersionRequest(); err == nil {
				var role, mode string
				if role, mode, statusCode, err = makeRoleRequest(); err == nil {
					if isLeader, err := makeIsLeaderRequest(); err == nil {
						instanceUp <- instanceUpInfo{
							Version:  version,
							Role:     role,
							Mode:     mode,
							IsLeader: isLeader,
						}
						return true
					}
				}
				statusCodes <- statusCode
			}
			return false
		}

		var maxNumberOfRounds int
		maxNumberOfRounds = int(s.cfg.InstanceUpTimeout * 2 / time.Second)
		for i := 0; i < maxNumberOfRounds; i++ {
			if checkInstanceOnce() {
				return
			}
			time.Sleep(time.Millisecond * 500)
		}
		instanceUp <- instanceUpInfo{}
	}()
	statusTrail = make([]int, 0, 16)
	startTime := time.Now()
	for {
		select {
		case instanceInfo := <-instanceUp:
			expectedRole, expectedMode := serverType.ExpectedServerRole()
			up = instanceInfo.Version != ""
			correctRole = instanceInfo.Role == expectedRole || expectedRole == ""
			correctMode := instanceInfo.Mode == expectedMode || expectedMode == ""
			return up, correctRole && correctMode, instanceInfo.Version, instanceInfo.Role, instanceInfo.Mode, instanceInfo.IsLeader, statusTrail, false
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
			return false, false, "", "", "", false, statusTrail, true
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

// HandleHello handles a hello request.
// If req==nil, this is a GET request, otherwise it is a POST request.
func (s *Service) HandleHello(ownAddress, remoteAddress string, req *HelloRequest, isUpdateRequest bool) (ClusterConfig, error) {
	// Claim exclusive access to our data structures
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.state == stateBootstrapSlave {
		// Redirect to bootstrap master
		if len(s.runtimeClusterManager.myPeers.AllPeers) > 0 {
			// TODO replace by bootstrap master
			master := s.runtimeClusterManager.myPeers.AllPeers[0]
			location := master.CreateStarterURL("/hello")
			return ClusterConfig{}, maskAny(RedirectError{location})
		} else {
			return ClusterConfig{}, maskAny(client.NewBadRequestError("No master known"))
		}
	}

	if s.state == stateRunningSlave {
		// Redirect to running master
		if masterURL := s.runtimeClusterManager.GetMasterURL(); masterURL != "" {
			s.log.Debug().Msgf("Redirecting hello request to %s", masterURL)
			helloURL, err := getURLWithPath(masterURL, "/hello")
			if err != nil {
				return ClusterConfig{}, maskAny(errors.Wrap(client.InternalServerError, err.Error()))
			} else {
				return ClusterConfig{}, maskAny(RedirectError{helloURL})
			}
		} else if req != nil || isUpdateRequest {
			// No master know, service unavailable when handling a POST of GET+update request
			return ClusterConfig{}, maskAny(errors.Wrap(client.ServiceUnavailableError, "No master known"))
		} else {
			// No master know, but initial request.
			// Just return what we know so the other starter can get started
			s.log.Debug().Msgf("Initial hello request from %s without known running master", remoteAddress)
		}
	}

	// Learn my own address (if needed)
	if s.learnOwnAddress {
		_, hostPort, _ := s.GetHTTPServerPort()
		myPeer, found := s.runtimeClusterManager.myPeers.PeerByID(s.id)
		if found {
			myPeer.Address = ownAddress
			myPeer.Port = hostPort
			s.runtimeClusterManager.myPeers.UpdatePeerByID(myPeer)
			s.learnOwnAddress = false
		}
	}

	// Is this a POST request?
	if req != nil {
		slaveAddr := req.SlaveAddress
		if slaveAddr == "" {
			host, _, err := net.SplitHostPort(remoteAddress)
			if err != nil {
				s.log.Warn().Err(err).Msgf("Failed to split host/port from remote address %s", remoteAddress)
				return ClusterConfig{}, maskAny(client.NewBadRequestError("SlaveAddress must be set."))
			}
			slaveAddr = normalizeHostName(host)
		} else {
			slaveAddr = normalizeHostName(slaveAddr)
		}
		slavePort := req.SlavePort

		// Check request
		if req.SlaveID == "" {
			s.log.Warn().Msg("SlaveID must be set. Remote address: " + remoteAddress)
			return ClusterConfig{}, maskAny(client.NewBadRequestError("SlaveID must be set."))
		}

		// Check datadir
		if !s.allowSameDataDir {
			for _, p := range s.runtimeClusterManager.myPeers.AllPeers {
				if p.Address == slaveAddr && p.DataDir == req.DataDir && p.ID != req.SlaveID {
					s.log.Warn().Msgf("Cannot use same directory as peer. Remote address: %s", remoteAddress)
					return ClusterConfig{}, maskAny(client.NewBadRequestError("Cannot use same directory as peer."))
				}
			}
		}

		// Check IsSecure, cannot mix secure / non-secure
		if req.IsSecure != s.IsSecure() {
			s.log.Warn().Msgf("Cannot mix secure / non-secure peers. Remote address: %s", remoteAddress)
			return ClusterConfig{}, maskAny(client.NewBadRequestError("Cannot mix secure / non-secure peers."))
		}

		// If slaveID already known, then return data right away.
		_, idFound := s.runtimeClusterManager.myPeers.PeerByID(req.SlaveID)
		if idFound {
			// ID already found, update peer data
			for i, p := range s.runtimeClusterManager.myPeers.AllPeers {
				if p.ID == req.SlaveID {
					peer := &s.runtimeClusterManager.myPeers.AllPeers[i]
					if s.cfg.AllPortOffsetsUnique {
						peer.Address = slaveAddr
					} else {
						// Need to check if this particular address does appear in
						// another peer, if so, we forbid to change the address:
						addrFoundInOtherPeer := false
						for _, pp := range s.runtimeClusterManager.myPeers.AllPeers {
							if pp.ID != req.SlaveID && pp.Address == slaveAddr {
								addrFoundInOtherPeer = true
								break
							}
						}
						if addrFoundInOtherPeer && isLocalAddress(slaveAddr) && isLocalAddress(p.Address) && s.runtimeClusterManager.myPeers.IsPortOffsetInUse() {
							// This is a default configuration, where host and port are not set. Keep offset.
							s.log.Warn().Msgf("Updating slave with local address (%s). Offset (%d) will be kept. Peer id: %s",
								slaveAddr, p.PortOffset, p.ID)
							peer.Address = slaveAddr
						} else if addrFoundInOtherPeer && p.Address != slaveAddr {
							msg := fmt.Sprintf("Cannot change slave address (%s) to an address that is already in use by another peer (id: %s)", slaveAddr, p.ID)
							s.log.Warn().Msgf(msg)
							return ClusterConfig{}, maskAny(client.NewBadRequestError(msg))
						} else {
							// We accept the new address (it might be the old one):
							peer.Address = slaveAddr
							// However, since we also accept the port, we must set the
							// port offset of that replaced peer to 0 such that the AllPeers
							// information actually contains the right port.
							peer.PortOffset = 0
						}

					}
					peer.Port = req.SlavePort
					peer.DataDir = req.DataDir
					peer.HasAgentFlag = boolFromRef(req.Agent, peer.HasAgentFlag)
					peer.HasCoordinatorFlag = utils.NotNilDefault(req.Coordinator, peer.HasCoordinatorFlag)
					peer.HasDBServerFlag = utils.NotNilDefault(req.DBServer, peer.HasDBServerFlag)
					peer.HasResilientSingleFlag = boolFromRef(req.ResilientSingle, peer.HasResilientSingleFlag)
					peer.HasSyncMasterFlag = boolFromRef(req.SyncMaster, peer.HasSyncMasterFlag)
					peer.HasSyncWorkerFlag = boolFromRef(req.SyncWorker, peer.HasSyncWorkerFlag)
				}
			}
		} else {
			// In single server mode, do not accept new slaves
			if s.mode.IsSingleMode() {
				s.log.Warn().Msg("In single server mode, slaves cannot be added.")
				return ClusterConfig{}, maskAny(client.NewBadRequestError("In single server mode, slaves cannot be added."))
			}
			// Ok. We're now in cluster or resilient single mode.
			// ID not yet found, add it
			portOffset := s.runtimeClusterManager.myPeers.GetFreePortOffset(slaveAddr, slavePort, s.cfg.AllPortOffsetsUnique)
			s.log.Debug().Msgf("Set slave port offset to %d, got slaveAddr=%s, slavePort=%d", portOffset, slaveAddr, slavePort)

			servers := peerServers{
				HasAgentFlag:           !s.runtimeClusterManager.myPeers.HaveEnoughAgents(),
				HasDBServerFlag:        nil,
				HasCoordinatorFlag:     nil,
				HasResilientSingleFlag: s.mode.IsActiveFailoverMode(),
				HasSyncMasterFlag:      s.mode.SupportsArangoSync() && s.cfg.SyncEnabled,
				HasSyncWorkerFlag:      s.mode.SupportsArangoSync() && s.cfg.SyncEnabled,
			}
			if req.Agent != nil {
				servers.HasAgentFlag = *req.Agent
			}
			if req.DBServer != nil && *req.DBServer != true {
				servers.HasDBServerFlag = req.DBServer
			}
			if req.Coordinator != nil && *req.Coordinator != true {
				servers.HasCoordinatorFlag = req.Coordinator
			}
			if req.ResilientSingle != nil {
				servers.HasResilientSingleFlag = *req.ResilientSingle
			}
			if req.SyncMaster != nil {
				servers.HasSyncMasterFlag = *req.SyncMaster
			}
			if req.SyncWorker != nil {
				servers.HasSyncWorkerFlag = *req.SyncWorker
			}
			newPeer := newPeer(req.SlaveID, slaveAddr, slavePort, portOffset, req.DataDir, servers, req.IsSecure)
			s.runtimeClusterManager.myPeers.AddPeer(newPeer)
			s.log.Info().Msgf("Added new peer '%s': %s, portOffset: %d", newPeer.ID, newPeer.Address, newPeer.PortOffset)
		}

		// Start running if we have enough agents
		if s.runtimeClusterManager.myPeers.HaveEnoughAgents() {
			// Save updated configuration
			s.saveSetup()
			// Trigger start running (if needed)
			s.bootstrapCompleted.trigger()
		}
	}

	return s.runtimeClusterManager.myPeers, nil
}

func isLocalAddress(addr string) bool {
	return addr == "127.0.0.1" || addr == "localhost"
}

// ChangeState alters the current state of the service
func (s *Service) ChangeState(newState State) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.state = newState
}

// PrepareDatabaseServerRequestFunc returns a function that is used to
// prepare a request to a database server (including authentication).
func (s *Service) PrepareDatabaseServerRequestFunc() func(*http.Request) error {
	return func(req *http.Request) error {
		addJwtHeader(req, s.jwtSecret)
		return nil
	}
}

// CreateClient creates a go-driver client with authentication for the given endpoints.
func (s *Service) CreateClient(endpoints []string, connectionType ConnectionType, serverType definitions.ServerType) (driver.Client, error) {
	connConfig := driver_http.ConnectionConfig{
		Endpoints:          endpoints,
		DontFollowRedirect: connectionType == ConnectionTypeAgency,
		TLSConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}
	var conn driver.Connection
	var err error
	switch connectionType {
	case ConnectionTypeDatabase:
		conn, err = driver_http.NewConnection(connConfig)
	case ConnectionTypeAgency:
		conn, err = agency.NewAgencyConnection(connConfig)
	default:
		return nil, maskAny(fmt.Errorf("Unknown ConnectionType: %d", connectionType))
	}

	secret := s.jwtSecret

	if s.DatabaseFeatures().GetJWTFolderOption() && s.jwtSecret != "" {
		if serverType != definitions.ServerTypeUnknown {
			if t, err := s.getFolderToken(serverType); err != nil {
				if !os.IsNotExist(err) {
					return nil, err
				}

				if t, err := s.getGlobalToken(); err != nil {
					return nil, err
				} else {
					secret = t
				}
			} else {
				secret = t
			}
		} else {
			if t, err := s.getFolderToken(definitions.AllServerTypes()...); err == nil {
				secret = t
			}
		}
	}

	if err != nil {
		return nil, maskAny(err)
	}
	jwtBearer, err := jwt.CreateArangodJwtAuthorizationHeader(secret, "starter")
	if err != nil {
		return nil, maskAny(err)
	}
	c, err := driver.NewClient(driver.ClientConfig{
		Connection:     conn,
		Authentication: driver.RawAuthentication(jwtBearer),
	})
	if err != nil {
		return nil, maskAny(err)
	}
	return c, nil
}

func (s *Service) getGlobalToken() (string, error) {
	p := s.GetLocalFolder()

	token, err := ioutil.ReadFile(path.Join(p, definitions.ArangodJWTSecretActive))
	if err != nil {
		return "", err
	}
	return string(token), nil
}

func (s *Service) getFolderToken(serverTypes ...definitions.ServerType) (string, error) {
	var currentErr = errors.Errorf("Missing ServerType")
	for _, serverType := range serverTypes {
		p, err := s.serverHostDir(serverType)
		if err != nil {
			currentErr = err
			continue
		}

		token, err := ioutil.ReadFile(path.Join(p, definitions.ArangodJWTSecretFolderName, definitions.ArangodJWTSecretActive))
		if err != nil {
			currentErr = err
			continue
		}

		return strings.TrimSpace(string(token)), nil
	}

	return "", currentErr
}

// UpdateClusterConfig updates the current cluster configuration.
func (s *Service) UpdateClusterConfig(newConfig ClusterConfig) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Perform checks to validate the new config
	if _, found := newConfig.PeerByID(s.id); !found {
		s.log.Warn().Msg("Updated cluster config does not contain myself. Rejecting")
		return maskAny(fmt.Errorf("updated cluster config does not contain myself"))
	}

	// Only update when changed
	if !reflect.DeepEqual(s.runtimeClusterManager.myPeers, newConfig) {
		s.runtimeClusterManager.myPeers = newConfig
		s.log.Debug().Msgf("Updating cluster config - %v", newConfig)
		return s.saveSetup()
	} else {
		s.log.Debug().Msg("Updating cluster config is not needed")
	}
	return nil
}

// RotateLogFiles rotates the log files of all servers
func (s *Service) RotateLogFiles(ctx context.Context) {
	s.runtimeServerManager.RotateLogFiles(ctx, s.log, s.logService, s, s.cfg)
}

// runRotateLogFiles keeps rotating log files at the configured interval until the given context has been canceled.
func (s *Service) runRotateLogFiles(ctx context.Context) {
	for {
		select {
		case <-time.After(s.cfg.LogRotateInterval):
			s.RotateLogFiles(ctx)
		case <-ctx.Done():
			return
		}
	}
}

// RestartServer triggers a restart of the server of the given type.
func (s *Service) RestartServer(serverType definitions.ServerType) error {
	if err := s.runtimeServerManager.RestartServer(s.log, serverType); err != nil {
		return maskAny(err)
	}
	return nil
}

func (s *Service) GetHTTPServerPort() (containerPort, hostPort int, err error) {
	containerPort = s.cfg.MasterPort
	hostPort = s.announcePort
	s.log.Debug().Msgf("hostPort=%d masterPort=%d #AllPeers=%d", hostPort, s.cfg.MasterPort, len(s.runtimeClusterManager.myPeers.AllPeers))
	if s.announcePort == s.cfg.MasterPort && len(s.runtimeClusterManager.myPeers.AllPeers) > 0 {
		if myPeer, ok := s.runtimeClusterManager.myPeers.PeerByID(s.id); ok {
			containerPort += myPeer.PortOffset
		} else {
			return 0, 0, maskAny(fmt.Errorf("no peer information found for ID '%s'", s.id))
		}
	}
	if s.isNetHost {
		hostPort = containerPort
	}
	return containerPort, hostPort, nil
}

// createHTTPServer initializes an HTTP server.
func (s *Service) createHTTPServer(config Config) (srv *httpServer, containerPort int, hostAddr, containerAddr string, err error) {
	// Create address to listen on
	containerPort, hostPort, err := s.GetHTTPServerPort()
	if err != nil {
		return nil, 0, "", "", maskAny(err)
	}
	containerAddr = net.JoinHostPort(config.BindAddress, strconv.Itoa(containerPort))
	hostAddr = net.JoinHostPort(config.OwnAddress, strconv.Itoa(hostPort))

	// Create HTTP server
	s.log.Debug().Msgf("Creating HTTP server on %s", hostAddr)
	return newHTTPServer(s.log, s, &s.runtimeServerManager, config, s.id), containerPort, hostAddr, containerAddr, nil
}

// startHTTPServer initializes and runs the HTTP server.
// If will return directly after starting it.
func (s *Service) startHTTPServer(config Config) {
	// Create server
	srv, _, hostAddr, containerAddr, err := s.createHTTPServer(config)
	if err != nil {
		s.log.Fatal().Err(err).Msg("Failed to get create HTTP server")
	}

	// Start HTTP server
	srv.Start(hostAddr, containerAddr, s.tlsConfig)
}

// startRunning starts all relevant servers and keeps the running.
func (s *Service) startRunning(runner Runner, config Config, bsCfg BootstrapConfig) {
	// Always start running as slave. Runtime process will elect master
	s.state = stateRunningSlave

	// Ensure we have a valid peer
	if _, ok := s.runtimeClusterManager.myPeers.PeerByID(s.id); !ok {
		s.log.Fatal().Msgf("Cannot find peer information for my ID ('%s')", s.id)
	}

	// If we're a local slave, do not try to become master (because we have no port mapping in docker)
	if s.isLocalSlave {
		s.runtimeClusterManager.AvoidBeingMaster()
	}

	wg := sync.WaitGroup{}

	// Start the runtime server manager
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.runtimeServerManager.Run(s.stopPeer.ctx, s.log, s, runner, config, bsCfg)
	}()

	// Start the runtime cluster manager
	wg.Add(1)
	go func() {
		defer wg.Done()

		s.runtimeClusterManager.Run(s.stopPeer.ctx, s.log, s)
	}()

	// Start the upgrade manager
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.upgradeManager.RunWatchUpgradePlan(s.stopPeer.ctx)
	}()

	// Wait until managers have terminated
	wg.Wait()
}

// Run runs the service in either master or slave mode.
func (s *Service) Run(rootCtx context.Context, bsCfg BootstrapConfig, clusterConfig ClusterConfig, shouldRelaunch bool) error {
	// Prepare a context that is cancelled when we need to stop
	s.stopPeer.ctx, s.stopPeer.trigger = context.WithCancel(rootCtx)

	// Load settings from BootstrapConfig
	s.id = bsCfg.ID
	s.mode = bsCfg.Mode
	s.startedLocalSlaves = bsCfg.StartLocalSlaves
	s.jwtSecret = bsCfg.JwtSecret
	s.sslKeyFile = bsCfg.SslKeyFile

	// Check mode & flags
	if bsCfg.Mode.IsClusterMode() || bsCfg.Mode.IsActiveFailoverMode() {
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
	s.runner = runner

	// Detect database version
	ctx := context.Background()
	if err := s.detectDatabaseFeatures(ctx); err != nil {
		return errors.Wrap(err, "Failed to detect database features")
	}

	if !s.DatabaseFeatures().SupportsActiveFailover() {
		if bsCfg.Mode.IsActiveFailoverMode() || boolFromRef(bsCfg.StartResilientSingle, false) {
			return fmt.Errorf("this ArangoDB version does not support running in Active-Failover mode (resilient-single is not supported since 3.12)")
		}
	}

	if !s.DatabaseFeatures().SupportsArangoSync() {
		if s.cfg.SyncEnabled {
			return fmt.Errorf("this ArangoDB version does not support running with ArangoSync (it is not supported since 3.12)")
		}
	}

	if s.cfg.SyncBinaryFoundErr != nil {
		return s.cfg.SyncBinaryFoundErr
	}

	if s.jwtSecret != "" {
		if s.DatabaseFeatures().GetJWTFolderOption() {
			os.MkdirAll(bsCfg.JWTFolderDir(), 0700)

			if _, err := os.Stat(bsCfg.JWTFolderDirFile(definitions.ArangodJWTSecretActive)); err != nil {
				if !os.IsNotExist(err) {
					return err
				}

				if err := os.WriteFile(bsCfg.JWTFolderDirFile(definitions.ArangodJWTSecretActive), []byte(s.jwtSecret), 0600); err != nil {
					return err
				}
			}
		}
	}

	// Check storage engine
	if err := s.validateStorageEngine(bsCfg.ServerStorageEngine, s.DatabaseFeatures()); err != nil {
		return maskAny(err)
	}

	// Start a rotate log file time
	if s.cfg.LogRotateInterval > 0 {
		go s.runRotateLogFiles(rootCtx)
	}

	// Is this a new start or a restart?
	if shouldRelaunch {
		s.runtimeClusterManager.myPeers = clusterConfig

		s.adjustClusterConfigForRelaunch(bsCfg)
		s.saveSetup()

		storageEngine, err := s.readActualStorageEngine()
		if err != nil {
			return maskAny(err)
		}
		s.runtimeClusterManager.myPeers.ServerStorageEngine = storageEngine
		bsCfg.ServerStorageEngine = storageEngine
		s.log.Info().Msgf("Using storage engine '%s'", bsCfg.ServerStorageEngine)
		s.startHTTPServer(s.cfg)
		wg := &sync.WaitGroup{}
		if bsCfg.StartLocalSlaves {
			s.startLocalSlaves(wg, s.cfg, bsCfg, clusterConfig.AllPeers)
		}
		s.startRunning(runner, s.cfg, bsCfg)
		wg.Wait()
	} else {
		// Bootstrap new cluster
		// Do we have to register?
		isBootstrapMaster, masterAddr, err := s.shouldActAsBootstrapMaster(rootCtx, s.cfg)
		if err != nil {
			return maskAny(err)
		}
		if !isBootstrapMaster {
			s.state = stateBootstrapSlave
			s.bootstrapSlave(masterAddr, runner, s.cfg, bsCfg)
		} else {
			s.state = stateBootstrapMaster
			s.bootstrapMaster(s.stopPeer.ctx, runner, s.cfg, bsCfg)
		}
	}

	return nil
}

func (s *Service) adjustClusterConfigForRelaunch(bsCfg BootstrapConfig) {
	if !s.mode.IsClusterMode() {
		return
	}

	s.runtimeClusterManager.myPeers.ForEachPeer(func(p Peer) Peer {
		if bsCfg.ID == p.ID {
			s.log.Debug().Msgf("Adjusting current memeber cluster config (locally) after restart (port: %d)", p.Port)
			p.peerServers = preparePeerServers(s.mode, bsCfg, s.cfg, &p)
		}
		return p
	})
}
