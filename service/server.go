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
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/rs/zerolog"

	driver "github.com/arangodb/go-driver"

	"github.com/arangodb-helper/arangodb/client"
	"github.com/arangodb-helper/arangodb/pkg/definitions"
	"github.com/arangodb-helper/arangodb/pkg/logging"
)

var (
	httpClient = client.DefaultHTTPClient()
)

const (
	contentTypeHeader = "Content-Type"
	contentTypeJSON   = "application/json"
)

// HelloRequest is the data structure send of the wire in a `/hello` POST request.
type HelloRequest struct {
	SlaveID      string // Unique ID of the slave
	SlaveAddress string // IP address used to reach the slave (if empty, this will be derived from the request)
	SlavePort    int    // Port used to reach the slave
	DataDir      string // Directory used for data by this slave
	IsSecure     bool   // If set, servers started by this peer are using an SSL connection
	Agent        *bool  `json:",omitempty"` // If not nil, sets if server gets an agent or not. If nil, default handling applies
	DBServer     *bool  `json:",omitempty"` // If not nil, sets if server gets an dbserver or not. If nil, default handling applies
	Coordinator  *bool  `json:",omitempty"` // If not nil, sets if server gets an coordinator or not. If nil, default handling applies
}

type httpServer struct {
	//config Config
	log                  zerolog.Logger
	server               *http.Server
	context              httpServerContext
	versionInfo          client.VersionInfo
	idInfo               client.IDInfo
	runtimeServerManager *runtimeServerManager
	masterPort           int
}

// httpServerContext provides a context for the httpServer.
type httpServerContext interface {
	ClientBuilder

	// ClusterConfig returns the current cluster configuration and the current peer
	ClusterConfig() (ClusterConfig, *Peer, ServiceMode)

	// IsRunningMaster returns if the starter is the running master.
	IsRunningMaster() (isRunningMaster, isRunning bool, masterURL string)

	// serverHostLogFile returns the path of the logfile (in host namespace) to which the given server will write its logs.
	serverHostLogFile(serverType definitions.ServerType) (string, error)

	// sendMasterLeaveCluster informs the master that we're leaving for good.
	// The master will remove the database servers from the cluster and update
	// the cluster configuration.
	sendMasterLeaveCluster() error

	// Stop the peer
	Stop()

	// UpgradeManager returns the database upgrade manager
	UpgradeManager() UpgradeManager

	// HandleHello - handle a hello request.
	// If req==nil, this is a GET request, otherwise it is a POST request.
	HandleHello(ownAddress, remoteAddress string, req *HelloRequest, isUpdateRequest, isRecoveryRequest bool) (ClusterConfig, error)

	// HandleGoodbye removes the database servers started by the peer with given id
	// from the cluster and alters the cluster configuration, removing the peer.
	HandleGoodbye(id string, force bool) (peerRemoved bool, err error)

	// DatabaseVersion returns the version, enterprise status, and V8 support status
	// of the `arangod` binary that is being used by this starter.
	DatabaseVersion(context.Context) (driver.Version, bool, bool, error)

	GetLocalFolder() string

	DatabaseFeatures() DatabaseFeatures

	serverHostDir(serverType definitions.ServerType) (string, error)
}

// newHTTPServer initializes and an HTTP server.
func newHTTPServer(log zerolog.Logger, context httpServerContext, runtimeServerManager *runtimeServerManager, config Config, serverID string) *httpServer {
	// Create HTTP server
	return &httpServer{
		log:     log,
		context: context,
		server: &http.Server{
			ErrorLog: logging.NewAdapter(log.With().Str("logger", "http-server").Logger()),
		},
		idInfo: client.IDInfo{
			ID: serverID,
		},
		versionInfo: client.VersionInfo{
			Version: config.ProjectVersion,
			Build:   config.ProjectBuild,
		},
		runtimeServerManager: runtimeServerManager,
		masterPort:           config.MasterPort,
	}
}

// Start listening for requests.
// This method will return directly after starting.
func (s *httpServer) Start(hostAddr, containerAddr string, tlsConfig *tls.Config) {
	go func() {
		if err := s.Run(hostAddr, containerAddr, tlsConfig, false); err != nil {
			s.log.Error().Err(err).Msgf("Failed to listen on %s", containerAddr)
		}
	}()
}

// Run listening for requests.
// This method will return after the server has been closed.
func (s *httpServer) Run(hostAddr, containerAddr string, tlsConfig *tls.Config, idOnly bool) error {
	mux := http.NewServeMux()
	if !idOnly {
		// Starter to starter API
		mux.HandleFunc("/hello", s.helloHandler)
		mux.HandleFunc("/goodbye", s.goodbyeHandler)
	}
	// External API
	mux.HandleFunc("/id", s.idHandler)
	if !idOnly {
		mux.HandleFunc("/local/inventory", s.localInventory)
		mux.HandleFunc("/cluster/inventory", s.clusterInventory)
		mux.HandleFunc("/process", s.processListHandler)
		mux.HandleFunc("/endpoints", s.endpointsHandler)
		mux.HandleFunc("/logs/agent", s.agentLogsHandler)
		mux.HandleFunc("/logs/dbserver", s.dbserverLogsHandler)
		mux.HandleFunc("/logs/coordinator", s.coordinatorLogsHandler)
		mux.HandleFunc("/logs/single", s.singleLogsHandler)
		mux.HandleFunc("/version", s.versionHandler)
		mux.HandleFunc("/database-version", s.databaseVersionHandler)
		mux.HandleFunc("/shutdown", s.shutdownHandler)
		mux.HandleFunc("/database-auto-upgrade", s.databaseAutoUpgradeHandler)

		// JWT Rotation
		s.registerJWTFunctions(mux)
	}

	s.server.Addr = containerAddr
	s.server.Handler = mux
	if tlsConfig != nil {
		s.log.Info().Msgf("ArangoDB Starter listening on %s (%s) using TLS", containerAddr, hostAddr)
		s.server.TLSConfig = tlsConfig
		if err := s.server.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			return maskAny(err)
		}
	} else {
		s.log.Info().Msgf("ArangoDB Starter listening on %s (%s)", containerAddr, hostAddr)
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			return maskAny(err)
		}
	}
	return nil
}

// Close the server
func (s *httpServer) Close() error {
	if err := s.server.Close(); err != nil {
		return maskAny(err)
	}
	return nil
}

// HTTP service function:

func (s *httpServer) helloHandler(w http.ResponseWriter, r *http.Request) {
	s.log.Debug().Msgf("Received %s /hello request from %s", r.Method, r.RemoteAddr)

	// Derive own address
	host, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Cannot derive own host address: %v", err))
		return
	}
	ownAddress := normalizeHostName(host)
	isUpdateRequest, _ := strconv.ParseBool(r.FormValue("update"))
	isRecoveryRequest, _ := strconv.ParseBool(r.FormValue(recoveryQueryParam))

	var result ClusterConfig
	if r.Method == "GET" {
		// Let service handle get request
		result, err = s.context.HandleHello(ownAddress, r.RemoteAddr, nil, isUpdateRequest, isRecoveryRequest)
		if err != nil {
			handleError(w, err)
			return
		}
	} else if r.Method == "POST" {
		// Read request
		var req HelloRequest
		defer r.Body.Close()
		if body, err := io.ReadAll(r.Body); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("Cannot read request body: %v", err.Error()))
			return
		} else if err := json.Unmarshal(body, &req); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("Cannot parse request body: %v", err.Error()))
			return
		}

		// Let service handle post request
		result, err = s.context.HandleHello(ownAddress, r.RemoteAddr, &req, false, false)
		if err != nil {
			handleError(w, err)
			return
		}
	} else {
		// Invalid method
		writeError(w, http.StatusMethodNotAllowed, "GET or POST required")
		return
	}

	// Send result
	b, err := json.Marshal(result)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
	} else {
		w.Write(b)
	}
}

// goodbyeHandler handles a `/goodbye` request that removes a peer from the list of peers.
func (s *httpServer) goodbyeHandler(w http.ResponseWriter, r *http.Request) {
	// Check method
	if r.Method != "POST" {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}

	// Parse request
	force, _ := strconv.ParseBool(r.FormValue("force"))
	var req client.GoodbyeRequest
	defer r.Body.Close()
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Cannot read request body: %v", err.Error()))
		return
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Cannot parse request body: %v", err.Error()))
		return
	}

	// Check request
	if req.SlaveID == "" {
		writeError(w, http.StatusBadRequest, "SlaveID must be set.")
		return
	}

	// Check state
	ctx := r.Context()
	isRunningMaster, isRunning, masterURL := s.context.IsRunningMaster()
	if !isRunning {
		// Must be running first
		writeError(w, http.StatusServiceUnavailable, "Starter is not in running phase")
	} else if !isRunningMaster {
		// Redirect to master
		if masterURL != "" {
			// Forward the request to the leader.
			c, err := createMasterClient(masterURL)
			if err != nil {
				handleError(w, err)
			} else {
				if err := c.RemovePeer(ctx, req.SlaveID, force); err != nil {
					s.log.Debug().Err(err).Msg("Forwarding RemovePeer failed")
					handleError(w, err)
				} else {
					w.WriteHeader(http.StatusOK)
					w.Write([]byte("OK"))
				}
			}
		} else {
			writeError(w, http.StatusServiceUnavailable, "No runtime master known")
		}
	} else {
		// Remove the peer
		s.log.Info().Bool("force", force).Msgf("Goodbye requested for peer %s", req.SlaveID)
		if removed, err := s.context.HandleGoodbye(req.SlaveID, force); err != nil {
			// Failure
			handleError(w, err)
		} else if !removed {
			// ID not found
			writeError(w, http.StatusNotFound, "Unknown ID")
		} else {
			// Peer removed
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("BYE"))
		}
	}
}

// idHandler returns a JSON object containing the ID of this starter.
func (s *httpServer) idHandler(w http.ResponseWriter, r *http.Request) {
	data, err := json.Marshal(s.idInfo)
	if err != nil {
		s.log.Error().Err(err).Msg("Failed to marshal ID response")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(err.Error()))
	} else {
		w.WriteHeader(http.StatusOK)
		w.Write(data)
	}
}

// processListHandler returns process information of all launched servers.
func (s *httpServer) processListHandler(w http.ResponseWriter, r *http.Request) {
	clusterConfig, myPeer, mode := s.context.ClusterConfig()
	isSecure := clusterConfig.IsSecure()

	// Gather processes
	resp := client.ProcessList{}
	expectedServers := 0
	if myPeer != nil {
		portOffset := myPeer.PortOffset
		ip := myPeer.Address
		if myPeer.HasAgent() {
			expectedServers++
		}
		if myPeer.HasDBServer() {
			expectedServers++
		}
		if myPeer.HasCoordinator() {
			expectedServers++
		}

		createServerProcess := func(serverType definitions.ServerType, p Process) client.ServerProcess {
			return client.ServerProcess{
				Type:        client.ServerType(serverType),
				IP:          ip,
				Port:        s.masterPort + portOffset + serverType.PortOffset(),
				ProcessID:   p.ProcessID(),
				ContainerID: p.ContainerID(),
				ContainerIP: p.ContainerIP(),
				IsSecure:    isSecure,
			}
		}

		if w := s.runtimeServerManager.agentProc; w != nil {
			if p := w.Process(); p != nil {
				resp.Servers = append(resp.Servers, createServerProcess(definitions.ServerTypeAgent, p))
			}
		}
		if w := s.runtimeServerManager.coordinatorProc; w != nil {
			if p := w.Process(); p != nil {
				resp.Servers = append(resp.Servers, createServerProcess(definitions.ServerTypeCoordinator, p))
			}
		}
		if w := s.runtimeServerManager.dbserverProc; w != nil {
			if p := w.Process(); p != nil {
				resp.Servers = append(resp.Servers, createServerProcess(definitions.ServerTypeDBServer, p))
			}
		}
		if w := s.runtimeServerManager.singleProc; w != nil {
			if p := w.Process(); p != nil {
				resp.Servers = append(resp.Servers, createServerProcess(definitions.ServerTypeSingle, p))
			}
		}
	}
	if mode.IsSingleMode() {
		expectedServers = 1
	}
	resp.ServersStarted = len(resp.Servers) == expectedServers
	b, err := json.Marshal(resp)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
	} else {
		w.Header().Set(contentTypeHeader, contentTypeJSON)
		w.Write(b)
	}
}

func urlListToStringSlice(list []url.URL) []string {
	result := make([]string, len(list))
	for i, u := range list {
		result[i] = u.String()
	}
	return result
}

// endpointsHandler returns the URL's needed to reach all starters, agents & coordinators in the cluster.
func (s *httpServer) endpointsHandler(w http.ResponseWriter, r *http.Request) {
	// IsRunningMaster returns if the starter is the running master.
	isRunningMaster, isRunning, masterURL := s.context.IsRunningMaster()

	// Check state
	if isRunning && !isRunningMaster {
		// Redirect to master
		if masterURL != "" {
			location, err := getURLWithPath(masterURL, "/endpoints")
			if err != nil {
				handleError(w, err)
			} else {
				handleError(w, RedirectError{Location: location})
			}
		} else {
			writeError(w, http.StatusServiceUnavailable, "No runtime master known")
		}
	} else {
		// Gather & send endpoints list
		clusterConfig, _, _ := s.context.ClusterConfig()

		// Gather endpoints
		resp := client.EndpointList{}
		if endpoints, err := clusterConfig.GetPeerEndpoints(); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		} else {
			resp.Starters = endpoints
		}
		if isRunning {
			if endpoints, err := clusterConfig.GetAgentEndpoints(); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			} else {
				resp.Agents = endpoints
			}
			if endpoints, err := clusterConfig.GetCoordinatorEndpoints(); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			} else {
				resp.Coordinators = endpoints
			}
		}

		b, err := json.Marshal(resp)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
		} else {
			w.Header().Set(contentTypeHeader, contentTypeJSON)
			w.Write(b)
		}
	}
}

// agentLogsHandler serves the entire agent log (if any).
// If there is no agent running a 404 is returned.
func (s *httpServer) agentLogsHandler(w http.ResponseWriter, r *http.Request) {
	_, myPeer, _ := s.context.ClusterConfig()

	if myPeer != nil && myPeer.HasAgent() {
		s.logsHandler(w, r, definitions.ServerTypeAgent)
	} else {
		w.WriteHeader(http.StatusNotFound)
	}
}

// dbserverLogsHandler serves the entire dbserver log.
func (s *httpServer) dbserverLogsHandler(w http.ResponseWriter, r *http.Request) {
	_, myPeer, _ := s.context.ClusterConfig()

	if myPeer != nil && myPeer.HasDBServer() {
		s.logsHandler(w, r, definitions.ServerTypeDBServer)
	} else {
		w.WriteHeader(http.StatusNotFound)
	}
}

// coordinatorLogsHandler serves the entire coordinator log.
func (s *httpServer) coordinatorLogsHandler(w http.ResponseWriter, r *http.Request) {
	_, myPeer, _ := s.context.ClusterConfig()

	if myPeer != nil && myPeer.HasCoordinator() {
		s.logsHandler(w, r, definitions.ServerTypeCoordinator)
	} else {
		w.WriteHeader(http.StatusNotFound)
	}
}

// singleLogsHandler serves the entire single server log.
func (s *httpServer) singleLogsHandler(w http.ResponseWriter, r *http.Request) {
	s.logsHandler(w, r, definitions.ServerTypeSingle)
}

func (s *httpServer) logsHandler(w http.ResponseWriter, r *http.Request, serverType definitions.ServerType) {
	// Find log path
	logPath, err := s.context.serverHostLogFile(serverType)
	if err != nil {
		// Not ready yet
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	s.log.Debug().Msgf("Fetching logs in %s", logPath)
	rd, err := os.Open(logPath)
	if os.IsNotExist(err) {
		// Log file not there (yet), we allow this
		w.WriteHeader(http.StatusOK)
	} else if err != nil {
		s.log.Error().Err(err).Msgf("Failed to open log file '%s'", logPath)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(err.Error()))
	} else {
		// Log open
		defer rd.Close()
		w.WriteHeader(http.StatusOK)
		io.Copy(w, rd)
	}
}

// versionHandler returns a JSON object containing the current version & build number.
func (s *httpServer) versionHandler(w http.ResponseWriter, r *http.Request) {
	data, err := json.Marshal(s.versionInfo)
	if err != nil {
		s.log.Error().Err(err).Msg("Failed to marshal version response")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(err.Error()))
	} else {
		w.WriteHeader(http.StatusOK)
		w.Write(data)
	}
}

// databaseVersionHandler returns a JSON object containing the current arangod version.
func (s *httpServer) databaseVersionHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	version, _, _, err := s.context.DatabaseVersion(r.Context())
	if err != nil {
		handleError(w, err)
	} else {
		data, err := json.Marshal(client.DatabaseVersionResponse{
			Version: version,
		})
		if err != nil {
			s.log.Error().Err(err).Msg("Failed to marshal datbase-version response")
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(err.Error()))
		} else {
			w.WriteHeader(http.StatusOK)
			w.Write(data)
		}
	}
}

// shutdownHandler initiates a shutdown of this process and all servers started by it.
func (s *httpServer) shutdownHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	if r.FormValue("mode") == "goodbye" {
		// Inform the master we're leaving for good
		if err := s.context.sendMasterLeaveCluster(); err != nil {
			s.log.Error().Err(err).Msg("Failed to send master goodbye")
			handleError(w, err)
			return
		}
	}

	// Stop my services
	s.context.Stop()
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

// databaseAutoUpgradeHandler initiates an upgrade of the database version.
func (s *httpServer) databaseAutoUpgradeHandler(w http.ResponseWriter, r *http.Request) {
	// IsRunningMaster returns if the starter is the running master.
	isRunningMaster, isRunning, masterURL := s.context.IsRunningMaster()
	_, _, mode := s.context.ClusterConfig()

	if !isRunning {
		// We must have reached the running state before we can handle this kind of request
		s.log.Debug().Msg("Received /database-auto-upgrade request while not in running phase")
		writeError(w, http.StatusBadRequest, "Must be in running state to do upgrades")
		return
	}

	ctx := r.Context()
	switch r.Method {
	case "POST":
		forceMinorUpgrade, _ := strconv.ParseBool(r.URL.Query().Get("forceMinorUpgrade"))

		// Start the upgrade process
		if isRunningMaster || mode.IsSingleMode() {
			// We're the starter leader, process the request
			if err := s.context.UpgradeManager().StartDatabaseUpgrade(ctx, forceMinorUpgrade); err != nil {
				handleError(w, err)
			} else {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("OK"))
			}
		} else {
			// We're not the starter leader.
			// Forward the request to the leader.
			c, err := createMasterClient(masterURL)
			if err != nil {
				handleError(w, err)
			} else {
				if err := c.StartDatabaseUpgrade(ctx, forceMinorUpgrade); err != nil {
					s.log.Debug().Err(err).Msg("Forwarding StartDatabaseUpgrade failed")
					handleError(w, err)
				} else {
					w.WriteHeader(http.StatusOK)
					w.Write([]byte("OK"))
				}
			}
		}
	case "PUT":
		// Retry the upgrade process
		um := s.context.UpgradeManager()
		if isLocalUpgradeRequest(r) {
			// Another starter asks for a failure this starter could not record
			// in the agency.
			err := um.RetryLocalUpgradeFailure(ctx)
			if !client.IsNotFound(err) {
				markLocalUpgradeFailureHeld(w)
			}
			writeUpgradeResult(w, err)
			return
		}
		// A stopped sole agent can keep agency reads blocked beyond the CLI
		// timeout. Retry cached failures before querying the agency or
		// forwarding to a leader that would query it.
		if mode.HasAgency() && !um.HasLocalUpgradeFailure() {
			if held, err := s.retryPeerLocalUpgradeFailure(ctx); held {
				// The starter holding the failure retried it, or refused to,
				// e.g. because other servers are unhealthy.
				writeUpgradeResult(w, err)
				return
			}
		}
		var err error
		if isRunningMaster || mode.IsSingleMode() || um.HasLocalUpgradeFailure() {
			// Standalone starters never elect a leader, and a failure that is
			// held locally can only be retried here.
			err = um.RetryDatabaseUpgrade(ctx)
		} else {
			// We're not the starter leader.
			// Forward the request to the leader.
			var c client.API
			if c, err = createMasterClient(masterURL); err == nil {
				if err = c.RetryDatabaseUpgrade(ctx); err != nil {
					s.log.Debug().Err(err).Msg("Forwarding RetryDatabaseUpgrade failed")
				}
			}
		}
		writeUpgradeResult(w, err)
	case "DELETE":
		// Abort the upgrade process
		if !isRunningMaster && !mode.IsSingleMode() {
			// We're not the starter leader.
			// Forward the request to the leader.
			c, err := createMasterClient(masterURL)
			if err != nil {
				handleError(w, err)
			} else {
				if err := c.AbortDatabaseUpgrade(ctx); err != nil {
					s.log.Debug().Err(err).Msg("Forwarding AbortDatabaseUpgrade failed")
					handleError(w, err)
				} else {
					w.WriteHeader(http.StatusOK)
					w.Write([]byte("OK"))
				}
			}
		} else {
			// The leader or a standalone starter handles cleanup locally.
			if err := s.context.UpgradeManager().AbortDatabaseUpgrade(ctx); err != nil {
				handleError(w, err)
			} else {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("OK"))
			}
		}
	case "GET":
		um := s.context.UpgradeManager()
		var status client.UpgradeStatus
		var err error
		if isLocalUpgradeRequest(r) {
			var found bool
			if status, found = um.LocalUpgradeFailureStatus(); !found {
				err = client.NewNotFoundError("This starter holds no failed upgrade step")
			} else {
				markLocalUpgradeFailureHeld(w)
			}
		} else {
			// Cached failures must remain visible while the agency is down.
			// Check them before an agency read can consume the request timeout.
			var found bool
			status, found = um.LocalUpgradeFailureStatus()
			if !found && mode.HasAgency() {
				status, found = s.peerLocalUpgradeFailureStatus(ctx)
			}
			if !found {
				status, err = um.Status(ctx)
			}
		}
		if err != nil {
			handleError(w, err)
		} else {
			b, err := json.Marshal(status)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
			} else {
				w.Header().Set(contentTypeHeader, contentTypeJSON)
				w.Write(b)
			}
		}
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// localUpgradeQuery marks a starter-to-starter request that must be served
// from this starter's own state, without the agency and without forwarding.
const localUpgradeQuery = "local"

// localUpgradeHeldHeader is set on an answer to a local upgrade request
// when this starter holds the failure. Any other answer, e.g. a 400 from a
// starter that is still bootstrapping, says nothing about the failure.
const localUpgradeHeldHeader = "X-Arangodb-Starter-Upgrade-Failure-Held"

func markLocalUpgradeFailureHeld(w http.ResponseWriter) {
	w.Header().Set(localUpgradeHeldHeader, "true")
}

func isLocalUpgradeRequest(r *http.Request) bool {
	local, _ := strconv.ParseBool(r.URL.Query().Get(localUpgradeQuery))
	return local
}

func writeUpgradeResult(w http.ResponseWriter, err error) {
	if err != nil {
		handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

// retryPeerLocalUpgradeFailure asks the other starters to retry a failure
// that they could not record in the agency. Only the starter whose server
// failed holds such a failure. held is true when one of them answered for
// it; err is then its refusal, if any.
func (s *httpServer) retryPeerLocalUpgradeFailure(ctx context.Context) (held bool, err error) {
	_, held, err = s.askOtherPeers(ctx, http.MethodPut, func() interface{} { return nil })
	return held, err
}

// peerLocalUpgradeFailureStatus returns the status of a failure that another
// starter could not record in the agency.
func (s *httpServer) peerLocalUpgradeFailureStatus(ctx context.Context) (client.UpgradeStatus, bool) {
	result, held, err := s.askOtherPeers(ctx, http.MethodGet, func() interface{} { return &client.UpgradeStatus{} })
	if !held || err != nil {
		return client.UpgradeStatus{}, false
	}
	return *result.(*client.UpgradeStatus), true
}

// localUpgradePeerTimeout bounds the wait for other starters, so that asking
// them stays within the timeout of the CLI request that triggered it.
const localUpgradePeerTimeout = 5 * time.Second

type peerUpgradeAnswer struct {
	peerID string
	result interface{}
	// held is true when the peer marked its answer as coming from the
	// starter that holds the failure.
	held bool
	err  error
}

// askOtherPeers sends a local upgrade request to all other starters at once.
// Only answers marked with localUpgradeHeldHeader count. The first such OK
// answer wins; otherwise the first such refusal is returned.
// newResult returns where to decode the answer; nil for none.
func (s *httpServer) askOtherPeers(ctx context.Context, method string, newResult func() interface{}) (result interface{}, held bool, err error) {
	config, myPeer, _ := s.context.ClusterConfig()
	ctx, cancel := context.WithTimeout(ctx, localUpgradePeerTimeout)
	defer cancel()
	answers := make(chan peerUpgradeAnswer, len(config.AllPeers))
	asked := 0
	for _, p := range config.AllPeers {
		if myPeer != nil && p.ID == myPeer.ID {
			continue
		}
		asked++
		go func(p Peer) {
			result := newResult()
			held, err := s.sendLocalUpgradeRequest(ctx, p, method, result)
			answers <- peerUpgradeAnswer{peerID: p.ID, result: result, held: held, err: err}
		}(p)
	}
	var refusal error
	for ; asked > 0; asked-- {
		a := <-answers
		if !a.held {
			// Not the owner, not reachable, not running yet, or a starter
			// that does not know local upgrade requests.
			if a.err != nil && !client.IsNotFound(a.err) {
				s.log.Debug().Err(a.err).Str("peer", a.peerID).Msg("Local upgrade request to peer failed")
			}
			continue
		}
		if a.err != nil {
			s.log.Info().Err(a.err).Str("peer", a.peerID).Str("method", method).Msg("Peer holding the upgrade failure refused the request")
			if refusal == nil {
				refusal = a.err
			}
			continue
		}
		s.log.Info().Str("peer", a.peerID).Str("method", method).Msg("Upgrade failure is held by peer")
		return a.result, true, nil
	}
	if refusal != nil {
		return nil, true, refusal
	}
	return nil, false, nil
}

var localUpgradeHTTPClient = client.DefaultHTTPClient()

// sendLocalUpgradeRequest sends a local upgrade request to p. held is true
// when p marked its answer as coming from the starter that holds the failure.
func (s *httpServer) sendLocalUpgradeRequest(ctx context.Context, p Peer, method string, result interface{}) (held bool, err error) {
	target := p.CreateStarterURL("/database-auto-upgrade?" + localUpgradeQuery + "=true")
	req, err := http.NewRequestWithContext(ctx, method, target, nil)
	if err != nil {
		return false, maskAny(err)
	}
	resp, err := localUpgradeHTTPClient.Do(req)
	if err != nil {
		return false, maskAny(err)
	}
	defer resp.Body.Close()
	held = resp.Header.Get(localUpgradeHeldHeader) == "true"
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, maskAny(err)
	}
	if resp.StatusCode != http.StatusOK {
		var er client.ErrorResponse
		_ = json.Unmarshal(body, &er)
		msg := er.Error
		if msg == "" {
			msg = fmt.Sprintf("%s %s returned status %d", method, target, resp.StatusCode)
		}
		switch resp.StatusCode {
		case http.StatusNotFound:
			return held, maskAny(client.NewNotFoundError(msg))
		case http.StatusBadRequest:
			return held, maskAny(client.NewBadRequestError(msg))
		case http.StatusPreconditionFailed:
			return held, maskAny(client.NewPreconditionFailedError(msg))
		case http.StatusServiceUnavailable:
			return held, maskAny(client.NewServiceUnavailableError(msg))
		default:
			return held, maskAny(client.NewInternalServerError(msg))
		}
	}
	if result != nil {
		if err := json.Unmarshal(body, result); err != nil {
			return held, maskAny(err)
		}
	}
	return held, nil
}

func handleError(w http.ResponseWriter, err error) {
	if loc, ok := IsRedirect(err); ok {
		header := w.Header()
		header.Add("Location", loc)
		w.WriteHeader(http.StatusTemporaryRedirect)
	} else if client.IsBadRequest(err) {
		writeError(w, http.StatusBadRequest, err.Error())
	} else if client.IsPreconditionFailed(err) {
		writeError(w, http.StatusPreconditionFailed, err.Error())
	} else if client.IsServiceUnavailable(err) {
		writeError(w, http.StatusServiceUnavailable, err.Error())
	} else if st, ok := client.IsStatusError(err); ok {
		writeError(w, st, err.Error())
	} else {
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	if message == "" {
		message = "Unknown error"
	}
	resp := client.ErrorResponse{Error: message}
	b, _ := json.Marshal(resp)
	w.Header().Set(contentTypeHeader, contentTypeJSON)
	w.WriteHeader(status)
	w.Write(b)
}

func createMasterClient(masterURL string) (client.API, error) {
	if masterURL == "" {
		return nil, maskAny(fmt.Errorf("Starter master is not known"))
	}
	ep, err := url.Parse(masterURL)
	if err != nil {
		return nil, maskAny(err)
	}
	c, err := client.NewArangoStarterClient(*ep)
	if err != nil {
		return nil, maskAny(err)
	}
	return c, nil
}
