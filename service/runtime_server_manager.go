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
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	logging "github.com/op/go-logging"
)

// runtimeServerManager implements the start, monitor, stop behavior of database servers in a runtime
// state.
type runtimeServerManager struct {
	logMutex        sync.Mutex // Mutex used to synchronize server log output
	agentProc       Process
	dbserverProc    Process
	coordinatorProc Process
	singleProc      Process
	syncMasterProc  Process
	syncWorkerProc  Process
	stopping        bool
}

// runtimeServerManagerContext provides a context for the runtimeServerManager.
type runtimeServerManagerContext interface {
	// ClusterConfig returns the current cluster configuration and the current peer
	ClusterConfig() (ClusterConfig, *Peer, ServiceMode)

	// serverPort returns the port number on which my server of given type will listen.
	serverPort(serverType ServerType) (int, error)

	// serverHostDir returns the path of the folder (in host namespace) containing data for the given server.
	serverHostDir(serverType ServerType) (string, error)
	// serverContainerDir returns the path of the folder (in container namespace) containing data for the given server.
	serverContainerDir(serverType ServerType) (string, error)

	// serverHostLogFile returns the path of the logfile (in host namespace) to which the given server will write its logs.
	serverHostLogFile(serverType ServerType) (string, error)
	// serverContainerLogFile returns the path of the logfile (in container namespace) to which the given server will write its logs.
	serverContainerLogFile(serverType ServerType) (string, error)

	// removeRecoveryFile removes any recorded RECOVERY file.
	removeRecoveryFile()

	// UpgradeManager returns the upgrade manager service.
	UpgradeManager() UpgradeManager

	// TestInstance checks the `up` status of an arangod server instance.
	TestInstance(ctx context.Context, serverType ServerType, address string, port int,
		statusChanged chan StatusItem) (up, correctRole bool, version, role, mode string, statusTrail []int, cancelled bool)

	// IsLocalSlave returns true if this peer is running as a local slave
	IsLocalSlave() bool

	// Stop the peer
	Stop()
}

// startServer starts a single Arangod/Arangosync server of the given type.
func startServer(ctx context.Context, log *logging.Logger, runtimeContext runtimeServerManagerContext, runner Runner,
	config Config, bsCfg BootstrapConfig, myHostAddress string, serverType ServerType, restart int) (Process, bool, error) {
	myPort, err := runtimeContext.serverPort(serverType)
	if err != nil {
		return nil, false, maskAny(err)
	}
	myHostDir, err := runtimeContext.serverHostDir(serverType)
	if err != nil {
		return nil, false, maskAny(err)
	}
	myContainerDir, err := runtimeContext.serverContainerDir(serverType)
	if err != nil {
		return nil, false, maskAny(err)
	}
	myContainerLogFile, err := runtimeContext.serverContainerLogFile(serverType)
	if err != nil {
		return nil, false, maskAny(err)
	}

	os.MkdirAll(filepath.Join(myHostDir, "data"), 0755)
	os.MkdirAll(filepath.Join(myHostDir, "apps"), 0755)

	// Check if the server is already running
	log.Infof("Looking for a running instance of %s on port %d", serverType, myPort)
	p, err := runner.GetRunningServer(myHostDir)
	if err != nil {
		return nil, false, maskAny(err)
	}
	if p != nil {
		log.Infof("%s seems to be running already, checking port %d...", serverType, myPort)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
		up, correctRole, _, _, _, _, _ := runtimeContext.TestInstance(ctx, serverType, myHostAddress, myPort, nil)
		cancel()
		if up && correctRole {
			log.Infof("%s is already running on %d. No need to start anything.", serverType, myPort)
			return p, false, nil
		} else if !up {
			log.Infof("%s is not up on port %d. Terminating existing process and restarting it...", serverType, myPort)
		} else if !correctRole {
			expectedRole, expectedMode := serverType.ExpectedServerRole()
			log.Infof("%s is not of role '%s.%s' on port %d. Terminating existing process and restarting it...", serverType, expectedRole, expectedMode, myPort)
		}
		p.Terminate()
	}

	// Check availability of port
	if !WaitUntilPortAvailable(myPort, time.Second*3) {
		return nil, true, maskAny(fmt.Errorf("Cannot start %s, because port %d is already in use", serverType, myPort))
	}

	log.Infof("Starting %s on port %d", serverType, myPort)
	processType := serverType.ProcessType()
	// Create/read arangod.conf
	var confVolumes []Volume
	var arangodConfig configFile
	var containerSecretFileName string
	if processType == ProcessTypeArangod {
		var err error
		confVolumes, arangodConfig, err = createArangodConf(log, bsCfg, myHostDir, myContainerDir, strconv.Itoa(myPort), serverType)
		if err != nil {
			return nil, false, maskAny(err)
		}
	} else if processType == ProcessTypeArangoSync {
		var err error
		confVolumes, containerSecretFileName, err = createArangoSyncClusterSecretFile(log, bsCfg, myHostDir, myContainerDir, serverType)
		if err != nil {
			return nil, false, maskAny(err)
		}
	}
	// Collect volumes
	v := collectServerConfigVolumes(serverType, arangodConfig)
	confVolumes = append(confVolumes, v...)

	// Create server command line arguments
	clusterConfig, myPeer, _ := runtimeContext.ClusterConfig()
	upgradeManager := runtimeContext.UpgradeManager()
	databaseAutoUpgrade := upgradeManager.ServerDatabaseAutoUpgrade(serverType)
	args, err := createServerArgs(log, config, clusterConfig, myContainerDir, myContainerLogFile, myPeer.ID, myHostAddress, strconv.Itoa(myPort), serverType, arangodConfig, containerSecretFileName, bsCfg.RecoveryAgentID, databaseAutoUpgrade)
	if err != nil {
		return nil, false, maskAny(err)
	}
	writeCommand(log, filepath.Join(myHostDir, processType.CommandFileName()), config.serverExecutable(processType), args)
	// Collect volumes
	vols := addVolume(confVolumes, myHostDir, myContainerDir, false)
	// Start process/container
	containerNamePrefix := ""
	if config.DockerContainerName != "" {
		containerNamePrefix = fmt.Sprintf("%s-", config.DockerContainerName)
	}
	containerName := fmt.Sprintf("%s%s-%s-%d-%s-%d", containerNamePrefix, serverType, myPeer.ID, restart, myHostAddress, myPort)
	ports := []int{myPort}
	p, err = runner.Start(ctx, processType, args[0], args[1:], vols, ports, containerName, myHostDir)
	if err != nil {
		return nil, false, maskAny(err)
	}
	if databaseAutoUpgrade {
		// Notify the context that we've succesfully started a server with database.auto-upgrade on.
		upgradeManager.ServerDatabaseAutoUpgradeStarter(serverType)
	}
	return p, false, nil
}

// showRecentLogs dumps the most recent log lines of the server of given type to the console.
func (s *runtimeServerManager) showRecentLogs(log *logging.Logger, runtimeContext runtimeServerManagerContext, serverType ServerType) {
	logPath, err := runtimeContext.serverHostLogFile(serverType)
	if err != nil {
		log.Errorf("Cannot find server host log file: %#v", err)
		return
	}
	logFile, err := os.Open(logPath)
	if os.IsNotExist(err) {
		log.Infof("Log file for %s is empty", serverType)
	} else if err != nil {
		log.Errorf("Cannot open log file for %s: %#v", serverType, err)
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
		log.Infof("## Start of %s log", serverType)
		for i := maxLines - 1; i >= 0; i-- {
			fmt.Println("\t" + strings.TrimSuffix(lines[i], "\n"))
		}
		log.Infof("## End of %s log", serverType)
	}
}

// runServer starts a single Arangod/Arangosync server of the given type and keeps restarting it when needed.
func (s *runtimeServerManager) runServer(ctx context.Context, log *logging.Logger, runtimeContext runtimeServerManagerContext, runner Runner,
	config Config, bsCfg BootstrapConfig, myPeer Peer, serverType ServerType, processVar *Process) {
	restart := 0
	recentFailures := 0
	for {
		myHostAddress := myPeer.Address
		startTime := time.Now()
		p, portInUse, err := startServer(ctx, log, runtimeContext, runner, config, bsCfg, myHostAddress, serverType, restart)
		if err != nil {
			log.Errorf("Error while starting %s: %#v", serverType, err)
			if !portInUse {
				break
			}
		} else {
			*processVar = p
			ctx, cancel := context.WithCancel(ctx)
			go func() {
				port, err := runtimeContext.serverPort(serverType)
				if err != nil {
					log.Fatalf("Cannot collect serverPort: %#v", err)
				}
				statusChanged := make(chan StatusItem)
				go func() {
					showLogDuration := time.Minute
					for {
						statusItem, ok := <-statusChanged
						if !ok {
							// Channel closed
							return
						}
						if statusItem.PrevStatusCode != statusItem.StatusCode {
							if config.DebugCluster {
								log.Infof("%s status changed to %d", serverType, statusItem.StatusCode)
							} else {
								log.Debugf("%s status changed to %d", serverType, statusItem.StatusCode)
							}
						}
						if statusItem.Duration > showLogDuration {
							showLogDuration = statusItem.Duration + time.Second*30
							s.showRecentLogs(log, runtimeContext, serverType)
						}
					}
				}()
				if up, correctRole, version, role, mode, statusTrail, cancelled := runtimeContext.TestInstance(ctx, serverType, myHostAddress, port, statusChanged); !cancelled {
					if up && correctRole {
						log.Infof("%s up and running (version %s).", serverType, version)
						if (serverType == ServerTypeCoordinator && !runtimeContext.IsLocalSlave()) || serverType == ServerTypeSingle || serverType == ServerTypeResilientSingle {
							hostPort, err := p.HostPort(port)
							if err != nil {
								if id := p.ContainerID(); id != "" {
									log.Infof("%s can only be accessed from inside a container.", serverType)
								}
							} else {
								ip := myPeer.Address
								urlSchemes := NewURLSchemes(myPeer.IsSecure)
								what := "cluster"
								if serverType == ServerTypeSingle {
									what = "single server"
								} else if serverType == ServerTypeResilientSingle {
									what = "resilient single server"
								}
								s.logMutex.Lock()
								log.Infof("Your %s can now be accessed with a browser at `%s://%s:%d` or", what, urlSchemes.Browser, ip, hostPort)
								log.Infof("using `arangosh --server.endpoint %s://%s:%d`.", urlSchemes.ArangoSH, ip, hostPort)
								s.logMutex.Unlock()
								runtimeContext.removeRecoveryFile()
							}
						}
						if serverType == ServerTypeSyncMaster && !runtimeContext.IsLocalSlave() {
							hostPort, err := p.HostPort(port)
							if err != nil {
								if id := p.ContainerID(); id != "" {
									log.Infof("%s can only be accessed from inside a container.", serverType)
								}
							} else {
								ip := myPeer.Address
								s.logMutex.Lock()
								log.Infof("Your syncmaster can now available at `https://%s:%d`", ip, hostPort)
								s.logMutex.Unlock()
							}
						}
					} else if !up {
						log.Warningf("%s not ready after 5min!: Status trail: %#v", serverType, statusTrail)
					} else if !correctRole {
						expectedRole, expectedMode := serverType.ExpectedServerRole()
						log.Warningf("%s does not have the expected role of '%s,%s' (but '%s,%s'): Status trail: %#v", serverType, expectedRole, expectedMode, role, mode, statusTrail)
					}
				}
			}()
			p.Wait()
			cancel()
		}
		uptime := time.Since(startTime)
		isTerminationExpected := runtimeContext.UpgradeManager().IsServerUpgradeInProgress(serverType)
		if isTerminationExpected {
			log.Debugf("%s stopped as expected", serverType)
		} else {
			var isRecentFailure bool
			if uptime < time.Second*30 {
				recentFailures++
				isRecentFailure = true
			} else {
				recentFailures = 0
				isRecentFailure = false
			}

			if isRecentFailure && !s.stopping {
				if !portInUse {
					log.Infof("%s has terminated quickly, in %s (recent failures: %d)", serverType, uptime, recentFailures)
					if recentFailures >= minRecentFailuresForLog && config.DebugCluster {
						// Show logs of the server
						s.showRecentLogs(log, runtimeContext, serverType)
					}
				}
				if recentFailures >= maxRecentFailures {
					log.Errorf("%s has failed %d times, giving up", serverType, recentFailures)
					runtimeContext.Stop()
					s.stopping = true
					break
				}
			} else {
				log.Infof("%s has terminated", serverType)
				if config.DebugCluster && !s.stopping {
					// Show logs of the server
					s.showRecentLogs(log, runtimeContext, serverType)
				}
			}
			if portInUse {
				time.Sleep(time.Second)
			}
		}

		if s.stopping {
			break
		}

		log.Infof("restarting %s", serverType)
		restart++
	}
}

// rotateLogFile rotates the log file of a single server.
func (s *runtimeServerManager) rotateLogFile(ctx context.Context, log *logging.Logger, runtimeContext runtimeServerManagerContext, myPeer Peer, serverType ServerType, p Process, filesToKeep int) {
	if p == nil {
		return
	}

	// Prepare log path
	logPath, err := runtimeContext.serverHostLogFile(serverType)
	if err != nil {
		log.Debugf("Failed to get host log file for '%s': %s", serverType, err)
		return
	}
	log.Debugf("Rotating %s log file: %s", serverType, logPath)

	// Move old files
	for i := filesToKeep; i >= 0; i-- {
		var logPathX string
		if i == 0 {
			logPathX = logPath
		} else {
			logPathX = logPath + fmt.Sprintf(".%d", i)
		}
		if _, err := os.Stat(logPathX); err == nil {
			if i == filesToKeep {
				// Remove file
				if err := os.Remove(logPathX); err != nil {
					log.Errorf("Failed to remove %s: %s", logPathX, err)
				} else {
					log.Debugf("Removed old log file: %s", logPathX)
				}
			} else {
				// Rename log[.i] -> log.i+1
				logPathNext := logPath + fmt.Sprintf(".%d", i+1)
				if err := os.Rename(logPathX, logPathNext); err != nil {
					log.Errorf("Failed to move %s to %s: %s", logPathX, logPathNext, err)
				} else {
					log.Debugf("Moved log file %s to %s", logPathX, logPathNext)
				}
			}
		}
	}

	// Send HUP signal
	if err := p.Hup(); err != nil {
		log.Errorf("Failed to send HUP signal: %s", err)
	}
	return
}

// RotateLogFiles rotates the log files of all servers
func (s *runtimeServerManager) RotateLogFiles(ctx context.Context, log *logging.Logger, runtimeContext runtimeServerManagerContext, config Config) {
	log.Info("Rotating log files...")
	_, myPeer, _ := runtimeContext.ClusterConfig()
	if myPeer == nil {
		log.Error("Cannot find my own peer in cluster configuration")
	} else {
		if p := s.syncWorkerProc; p != nil {
			s.rotateLogFile(ctx, log, runtimeContext, *myPeer, ServerTypeSyncWorker, p, config.LogRotateFilesToKeep)
		}
		if p := s.syncMasterProc; p != nil {
			s.rotateLogFile(ctx, log, runtimeContext, *myPeer, ServerTypeSyncMaster, p, config.LogRotateFilesToKeep)
		}
		if p := s.singleProc; p != nil {
			s.rotateLogFile(ctx, log, runtimeContext, *myPeer, ServerTypeSingle, p, config.LogRotateFilesToKeep)
		}
		if p := s.coordinatorProc; p != nil {
			s.rotateLogFile(ctx, log, runtimeContext, *myPeer, ServerTypeCoordinator, p, config.LogRotateFilesToKeep)
		}
		if p := s.dbserverProc; p != nil {
			s.rotateLogFile(ctx, log, runtimeContext, *myPeer, ServerTypeDBServer, p, config.LogRotateFilesToKeep)
		}
		if p := s.agentProc; p != nil {
			s.rotateLogFile(ctx, log, runtimeContext, *myPeer, ServerTypeAgent, p, config.LogRotateFilesToKeep)
		}
	}
}

// Run starts all relevant servers and keeps the running.
func (s *runtimeServerManager) Run(ctx context.Context, log *logging.Logger, runtimeContext runtimeServerManagerContext, runner Runner, config Config, bsCfg BootstrapConfig) {
	_, myPeer, mode := runtimeContext.ClusterConfig()
	if myPeer == nil {
		log.Fatal("Cannot find my own peer in cluster configuration")
	}

	if mode.IsClusterMode() {
		// Start agent:
		if myPeer.HasAgent() {
			go s.runServer(ctx, log, runtimeContext, runner, config, bsCfg, *myPeer, ServerTypeAgent, &s.agentProc)
			time.Sleep(time.Second)
		}

		// Start DBserver:
		if bsCfg.StartDBserver == nil || *bsCfg.StartDBserver {
			go s.runServer(ctx, log, runtimeContext, runner, config, bsCfg, *myPeer, ServerTypeDBServer, &s.dbserverProc)
			time.Sleep(time.Second)
		}

		// Start Coordinator:
		if bsCfg.StartCoordinator == nil || *bsCfg.StartCoordinator {
			go s.runServer(ctx, log, runtimeContext, runner, config, bsCfg, *myPeer, ServerTypeCoordinator, &s.coordinatorProc)
		}

		// Start sync master
		if bsCfg.StartSyncMaster == nil || *bsCfg.StartSyncMaster {
			go s.runServer(ctx, log, runtimeContext, runner, config, bsCfg, *myPeer, ServerTypeSyncMaster, &s.syncMasterProc)
		}

		// Start sync worker
		if bsCfg.StartSyncWorker == nil || *bsCfg.StartSyncWorker {
			go s.runServer(ctx, log, runtimeContext, runner, config, bsCfg, *myPeer, ServerTypeSyncWorker, &s.syncWorkerProc)
		}
	} else if mode.IsActiveFailoverMode() {
		// Start agent:
		if myPeer.HasAgent() {
			go s.runServer(ctx, log, runtimeContext, runner, config, bsCfg, *myPeer, ServerTypeAgent, &s.agentProc)
			time.Sleep(time.Second)
		}

		// Start Single server:
		if myPeer.HasResilientSingle() {
			go s.runServer(ctx, log, runtimeContext, runner, config, bsCfg, *myPeer, ServerTypeResilientSingle, &s.singleProc)
		}
	} else if mode.IsSingleMode() {
		// Start Single server:
		go s.runServer(ctx, log, runtimeContext, runner, config, bsCfg, *myPeer, ServerTypeSingle, &s.singleProc)
	}

	// Wait until context is cancelled, then we'll stop
	<-ctx.Done()
	s.stopping = true

	log.Info("Shutting down services...")
	if p := s.syncWorkerProc; p != nil {
		terminateProcess(log, p, "sync worker", time.Minute)
	}
	if p := s.syncMasterProc; p != nil {
		terminateProcess(log, p, "sync master", time.Minute)
	}
	if p := s.singleProc; p != nil {
		terminateProcess(log, p, "single server", time.Minute)
	}
	if p := s.coordinatorProc; p != nil {
		terminateProcess(log, p, "coordinator", time.Minute)
	}
	if p := s.dbserverProc; p != nil {
		terminateProcess(log, p, "dbserver", time.Minute)
	}
	if p := s.agentProc; p != nil {
		time.Sleep(3 * time.Second)
		terminateProcess(log, p, "agent", time.Minute)
	}

	// Cleanup containers
	if p := s.syncWorkerProc; p != nil {
		if err := p.Cleanup(); err != nil {
			log.Warningf("Failed to cleanup sync worker: %v", err)
		}
	}
	if p := s.syncMasterProc; p != nil {
		if err := p.Cleanup(); err != nil {
			log.Warningf("Failed to cleanup sync master: %v", err)
		}
	}
	if p := s.singleProc; p != nil {
		if err := p.Cleanup(); err != nil {
			log.Warningf("Failed to cleanup single server: %v", err)
		}
	}
	if p := s.coordinatorProc; p != nil {
		if err := p.Cleanup(); err != nil {
			log.Warningf("Failed to cleanup coordinator: %v", err)
		}
	}
	if p := s.dbserverProc; p != nil {
		if err := p.Cleanup(); err != nil {
			log.Warningf("Failed to cleanup dbserver: %v", err)
		}
	}
	if p := s.agentProc; p != nil {
		time.Sleep(3 * time.Second)
		if err := p.Cleanup(); err != nil {
			log.Warningf("Failed to cleanup agent: %v", err)
		}
	}

	// Cleanup runner
	if err := runner.Cleanup(); err != nil {
		log.Warningf("Failed to cleanup runner: %v", err)
	}
}

// RestartServer triggers a restart of the server of the given type.
func (s *runtimeServerManager) RestartServer(log *logging.Logger, serverType ServerType) error {
	var p Process
	var name string
	switch serverType {
	case ServerTypeAgent:
		p = s.agentProc
		name = "agent"
	case ServerTypeDBServer:
		p = s.dbserverProc
		name = "dbserver"
	case ServerTypeCoordinator:
		p = s.coordinatorProc
		name = "coordinator"
	case ServerTypeSingle, ServerTypeResilientSingle:
		p = s.singleProc
		name = "single server"
	case ServerTypeSyncMaster:
		p = s.syncMasterProc
		name = "sync master"
	case ServerTypeSyncWorker:
		p = s.syncWorkerProc
		name = "sync worker"
	default:
		return maskAny(fmt.Errorf("Unknown server type '%s'", serverType))
	}
	if p != nil {
		terminateProcess(log, p, name, time.Minute)
	}
	return nil
}
