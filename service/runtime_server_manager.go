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

	// TestInstance checks the `up` status of an arangod server instance.
	TestInstance(ctx context.Context, address string, port int, statusChanged chan StatusItem) (up bool, version string, statusTrail []int, cancelled bool)

	// IsLocalSlave returns true if this peer is running as a local slave
	IsLocalSlave() bool

	// Stop the peer
	Stop()
}

// startArangod starts a single Arango server of the given type.
func startArangod(log *logging.Logger, runtimeContext runtimeServerManagerContext, runner Runner,
	config Config, bsCfg BootstrapConfig, myHostAddress string, serverType ServerType, restart int) (Process, bool, error) {
	myPort, err := runtimeContext.serverPort(serverType)
	if err != nil {
		return nil, false, maskAny(err)
	}
	myHostDir, err := runtimeContext.serverHostDir(serverType)
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
		up, _, _, _ := runtimeContext.TestInstance(ctx, myHostAddress, myPort, nil)
		cancel()
		if up {
			log.Infof("%s is already running on %d. No need to start anything.", serverType, myPort)
			return p, false, nil
		}
		log.Infof("%s is not up on port %d. Terminating existing process and restarting it...", serverType, myPort)
		p.Terminate()
	}

	// Check availability of port
	if !WaitUntilPortAvailable(myPort, time.Second*3) {
		return nil, true, maskAny(fmt.Errorf("Cannot start %s, because port %d is already in use", serverType, myPort))
	}

	log.Infof("Starting %s on port %d", serverType, myPort)
	myContainerDir := runner.GetContainerDir(myHostDir, dockerDataDir)
	// Create/read arangod.conf
	confVolumes, arangodConfig, err := createArangodConf(log, bsCfg, myHostDir, myContainerDir, strconv.Itoa(myPort), serverType)
	if err != nil {
		return nil, false, maskAny(err)
	}
	// Create arangod command line arguments
	clusterConfig, myPeer, _ := runtimeContext.ClusterConfig()
	args := createArangodArgs(log, config, clusterConfig, myContainerDir, myPeer.ID, myHostAddress, strconv.Itoa(myPort), serverType, arangodConfig)
	writeCommand(log, filepath.Join(myHostDir, "arangod_command.txt"), config.serverExecutable(), args)
	// Collect volumes
	configVolumes := collectConfigVolumes(arangodConfig)
	vols := addVolume(append(confVolumes, configVolumes...), myHostDir, myContainerDir, false)
	// Start process/container
	containerNamePrefix := ""
	if config.DockerContainerName != "" {
		containerNamePrefix = fmt.Sprintf("%s-", config.DockerContainerName)
	}
	containerName := fmt.Sprintf("%s%s-%s-%d-%s-%d", containerNamePrefix, serverType, myPeer.ID, restart, myHostAddress, myPort)
	ports := []int{myPort}
	if p, err := runner.Start(args[0], args[1:], vols, ports, containerName, myHostDir); err != nil {
		return nil, false, maskAny(err)
	} else {
		return p, false, nil
	}
}

// showRecentLogs dumps the most recent log lines of the server of given type to the console.
func (s *runtimeServerManager) showRecentLogs(log *logging.Logger, runtimeContext runtimeServerManagerContext, serverType ServerType) {
	myHostDir, err := runtimeContext.serverHostDir(serverType)
	if err != nil {
		log.Errorf("Cannot find server host dir: %#v", err)
		return
	}
	logPath := filepath.Join(myHostDir, logFileName)
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

// runArangod starts a single Arango server of the given type and keeps restarting it when needed.
func (s *runtimeServerManager) runArangod(ctx context.Context, log *logging.Logger, runtimeContext runtimeServerManagerContext, runner Runner,
	config Config, bsCfg BootstrapConfig, myPeer Peer, serverType ServerType, processVar *Process) {
	restart := 0
	recentFailures := 0
	for {
		myHostAddress := myPeer.Address
		startTime := time.Now()
		p, portInUse, err := startArangod(log, runtimeContext, runner, config, bsCfg, myHostAddress, serverType, restart)
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
				if up, version, statusTrail, cancelled := runtimeContext.TestInstance(ctx, myHostAddress, port, statusChanged); !cancelled {
					if up {
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
								}
								s.logMutex.Lock()
								log.Infof("Your %s can now be accessed with a browser at `%s://%s:%d` or", what, urlSchemes.Browser, ip, hostPort)
								log.Infof("using `arangosh --server.endpoint %s://%s:%d`.", urlSchemes.ArangoSH, ip, hostPort)
								s.logMutex.Unlock()
							}
						}
					} else {
						log.Warningf("%s not ready after 5min!: Status trail: %#v", serverType, statusTrail)
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

		if isRecentFailure && !s.stopping {
			if !portInUse {
				log.Infof("%s has terminated, quickly, in %s (recent failures: %d)", serverType, uptime, recentFailures)
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

		if s.stopping {
			break
		}

		log.Infof("restarting %s", serverType)
		restart++
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
			go s.runArangod(ctx, log, runtimeContext, runner, config, bsCfg, *myPeer, ServerTypeAgent, &s.agentProc)
			time.Sleep(time.Second)
		}

		// Start DBserver:
		if bsCfg.StartDBserver == nil || *bsCfg.StartDBserver {
			go s.runArangod(ctx, log, runtimeContext, runner, config, bsCfg, *myPeer, ServerTypeDBServer, &s.dbserverProc)
			time.Sleep(time.Second)
		}

		// Start Coordinator:
		if bsCfg.StartCoordinator == nil || *bsCfg.StartCoordinator {
			go s.runArangod(ctx, log, runtimeContext, runner, config, bsCfg, *myPeer, ServerTypeCoordinator, &s.coordinatorProc)
		}
	} else if mode.IsResilientSingleMode() {
		// Start agent:
		if myPeer.HasAgent() {
			go s.runArangod(ctx, log, runtimeContext, runner, config, bsCfg, *myPeer, ServerTypeAgent, &s.agentProc)
			time.Sleep(time.Second)
		}

		// Start Single server:
		if myPeer.HasResilientSingle() {
			go s.runArangod(ctx, log, runtimeContext, runner, config, bsCfg, *myPeer, ServerTypeResilientSingle, &s.singleProc)
		}
	} else if mode.IsSingleMode() {
		// Start Single server:
		go s.runArangod(ctx, log, runtimeContext, runner, config, bsCfg, *myPeer, ServerTypeSingle, &s.singleProc)
	}

	// Wait until context is cancelled, then we'll stop
	<-ctx.Done()
	s.stopping = true

	log.Info("Shutting down services...")
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
