//
// DISCLAIMER
//
// Copyright 2017-2021 ArangoDB GmbH, Cologne, Germany
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
// Author Adam Janikowski
// Author Tomasz Mielech
//

package service

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/arangodb-helper/arangodb/pkg/definitions"
)

func NewProcessWrapper(s *runtimeServerManager, ctx context.Context, log zerolog.Logger, runtimeContext runtimeServerManagerContext, runner Runner,
	config Config, bsCfg BootstrapConfig, myPeer Peer, serverType definitions.ServerType, gracePeriod time.Duration) ProcessWrapper {
	p := &processWrapper{
		s:              s,
		ctx:            ctx,
		log:            log.With().Str("type", serverType.String()).Logger(),
		runtimeContext: runtimeContext,
		runner:         runner,
		config:         config,
		bsCfg:          bsCfg,
		myPeer:         myPeer,
		serverType:     serverType,
		closed:         make(chan struct{}),
		stopping:       make(chan struct{}),
		gracePeriod:    gracePeriod,
	}

	startedCh := make(chan struct{})

	p.log.Info().Msgf("%s starting routine", p.serverType)

	go p.run(startedCh)

	<-startedCh

	return p
}

type ProcessWrapper interface {
	Wait(timeout time.Duration) bool
	Process() Process
}

type processWrapper struct {
	s              *runtimeServerManager
	ctx            context.Context
	log            zerolog.Logger
	runtimeContext runtimeServerManagerContext
	runner         Runner
	config         Config
	bsCfg          BootstrapConfig
	myPeer         Peer
	serverType     definitions.ServerType
	gracePeriod    time.Duration

	lock sync.Mutex
	proc Process

	closed, stopping chan struct{}
}

func (p *processWrapper) Process() Process {
	return p.proc
}

func (p *processWrapper) Wait(timeout time.Duration) bool {
	p.stop()

	select {
	case <-p.closed:
		return true
	case <-time.After(timeout):
		return false
	}
}

func (p *processWrapper) stop() {
	p.lock.Lock()
	defer p.lock.Unlock()

	select {
	case <-p.stopping:
		break
	default:
		close(p.stopping)
	}
}

func (p *processWrapper) run(startedCh chan<- struct{}) {
	logProcess := p.log
	defer func() {
		logProcess.Info().Msg("Exited")
		defer close(p.closed)
	}()
	restart := 0
	recentFailures := 0

	close(startedCh)

	logProcess.Info().Msgf("%s started routine", p.serverType)

	for {
		myHostAddress := p.myPeer.Address
		startTime := time.Now()
		features := p.runtimeContext.DatabaseFeatures()
		proc, portInUse, err := startServer(p.ctx, logProcess, p.runtimeContext, p.runner, p.config, p.bsCfg, myHostAddress, p.serverType, features, restart)
		if err != nil {
			logProcess.Error().Err(err).Msgf("Error while starting %s", p.serverType)
			if !portInUse {
				break
			}
		} else {
			logProcess = proc.GetLogger(logProcess)

			logProcess.Info().Msg("server started")
			p.proc = proc
			ctx, cancel := context.WithCancel(p.ctx)
			go func() {
				port, err := p.runtimeContext.serverPort(p.serverType)
				if err != nil {
					logProcess.Fatal().Err(err).Msg("Cannot collect serverPort")
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
							if p.config.DebugCluster {
								logProcess.Info().Msgf("%s status changed to %d", p.serverType, statusItem.StatusCode)
							} else {
								logProcess.Debug().Msgf("%s status changed to %d", p.serverType, statusItem.StatusCode)
							}
						}
						if statusItem.Duration > showLogDuration {
							showLogDuration = statusItem.Duration + time.Second*30
							p.s.showRecentLogs(logProcess, p.runtimeContext, p.serverType)
						}
					}
				}()
				if up, correctRole, version, role, mode, isLeader, statusTrail, cancelled := p.runtimeContext.TestInstance(ctx, p.serverType, myHostAddress, port, statusChanged); !cancelled {
					if up && correctRole {
						msgPostfix := ""
						if p.serverType == definitions.ServerTypeResilientSingle && !isLeader {
							msgPostfix = " as follower"
						}
						logProcess.Info().Msgf("%s up and running%s (version %s).", p.serverType, msgPostfix, version)
						if (p.serverType == definitions.ServerTypeCoordinator && !p.runtimeContext.IsLocalSlave()) || p.serverType == definitions.ServerTypeSingle || p.serverType == definitions.ServerTypeResilientSingle {
							hostPort, err := proc.HostPort(port)
							if err != nil {
								if id := proc.ContainerID(); id != "" {
									logProcess.Info().Msgf("%s can only be accessed from inside a container.", p.serverType)
								}
							} else {
								ip := p.myPeer.Address
								urlSchemes := NewURLSchemes(p.myPeer.IsSecure)
								what := ServiceModeCluster
								if p.serverType == definitions.ServerTypeSingle {
									what = "single server"
								} else if p.serverType == definitions.ServerTypeResilientSingle {
									what = "resilient single server"
								}
								if p.serverType != definitions.ServerTypeResilientSingle || isLeader {
									p.s.logMutex.Lock()
									logProcess.Info().Msgf("Your %s can now be accessed with a browser at `%s://%s:%d` or", what, urlSchemes.Browser, ip, hostPort)
									logProcess.Info().Msgf("using `arangosh --server.endpoint %s://%s:%d`.", urlSchemes.ArangoSH, ip, hostPort)
									p.s.logMutex.Unlock()
								}
								p.runtimeContext.removeRecoveryFile()
							}
						}
						if p.serverType == definitions.ServerTypeSyncMaster && !p.runtimeContext.IsLocalSlave() {
							hostPort, err := proc.HostPort(port)
							if err != nil {
								if id := proc.ContainerID(); id != "" {
									logProcess.Info().Msgf("%s can only be accessed from inside a container.", p.serverType)
								}
							} else {
								ip := p.myPeer.Address
								p.s.logMutex.Lock()
								logProcess.Info().Msgf("Your syncmaster can now available at `https://%s:%d`", ip, hostPort)
								p.s.logMutex.Unlock()
							}
						}
					} else if !up {
						logProcess.Warn().Msgf("%s not ready after 5min!: Status trail: %#v", p.serverType, statusTrail)
					} else if !correctRole {
						expectedRole, expectedMode := p.serverType.ExpectedServerRole()
						logProcess.Warn().Msgf("%s does not have the expected role of '%s,%s' (but '%s,%s'): Status trail: %#v", p.serverType, expectedRole, expectedMode, role, mode, statusTrail)
					}
				}
			}()

			procC := proc.WaitCh()

			select {
			case <-procC:
				logProcess.Info().Msgf("Terminated %s", p.serverType)
				break
			case <-p.stopping:
				if p.s.stopping {
					// Starter is being closed
					terminateProcessWithActions(logProcess, p.proc, p.serverType, time.Second, time.Minute)
				} else {
					// Process restart
					terminateProcessWithActions(logProcess, p.proc, p.serverType, 0, time.Minute)
				}
				break
			}
			cancel()
		}
		uptime := time.Since(startTime)
		isTerminationExpected := p.runtimeContext.UpgradeManager().IsServerUpgradeInProgress(p.serverType)
		if isTerminationExpected {
			logProcess.Debug().Msgf("%s stopped as expected", p.serverType)
		} else {
			var isRecentFailure bool
			if uptime < time.Second*30 {
				recentFailures++
				isRecentFailure = true
			} else {
				recentFailures = 0
				isRecentFailure = false
			}

			if isRecentFailure && !p.s.stopping {
				if !portInUse {
					logProcess.Info().Msgf("%s has terminated quickly, in %s (recent failures: %d)", p.serverType, uptime, recentFailures)
					if recentFailures >= definitions.MinRecentFailuresForLog {
						// Show logs of the server
						p.s.showRecentLogs(logProcess, p.runtimeContext, p.serverType)
					}
				}
				if recentFailures >= definitions.MaxRecentFailures {
					logProcess.Error().Msgf("%s has failed %d times, giving up", p.serverType, recentFailures)
					p.runtimeContext.Stop()
					p.s.stopping = true
					break
				}
			} else {
				logProcess.Info().Msgf("%s has terminated", p.serverType)
				if p.config.DebugCluster && !p.s.stopping {
					// Show logs of the server
					p.s.showRecentLogs(logProcess, p.runtimeContext, p.serverType)
				}
			}
			if portInUse {
				time.Sleep(time.Second)
			}
		}

		if p.s.stopping {
			break
		}

		logProcess.Info().Msgf("restarting %s", p.serverType)
		restart++
	}
}
