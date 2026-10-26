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
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"

	"github.com/arangodb-helper/arangodb/pkg/definitions"
	"github.com/arangodb-helper/arangodb/pkg/utils"
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
	// Wait blocks until process terminates
	// Returns true if process was already terminated or it has terminated in time
	Wait(timeout time.Duration) bool

	Process() Process

	// RequestRestart records an explicit restart request and wakes the wrapper
	// if it is paused after an auto-upgrade failure.
	RequestRestart()
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
	restartGen       atomic.Uint64
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

func (p *processWrapper) RequestRestart() {
	p.restartGen.Add(1)
}

func (p *processWrapper) stop() {
	p.lock.Lock()
	defer p.lock.Unlock()

	select {
	case <-p.stopping:
		p.log.Debug().Msg("stopping channel already closed")
		break
	default:
		p.log.Debug().Msg("closing stopping channel")
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
	exitCode := -1

	close(startedCh)

	logProcess.Info().Msgf("%s started routine", p.serverType)

	for {
		myHostAddress := p.myPeer.Address
		startTime := time.Now()
		features := p.runtimeContext.DatabaseFeatures()
		output := utils.NewLimitedBuffer(80 * 100)

		var logMark logFileMark
		proc, portInUse, err := startServer(p.ctx, logProcess, p.runtimeContext, p.runner, p.config, p.bsCfg, myHostAddress, p.serverType, features, restart, output, exitCode, &logMark)
		autoUpgradeFailed := false
		var failedRestartGen uint64
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

							p.showProcessOutput(logProcess, output, p.serverType)
							p.s.showRecentLogs(logProcess, p.runtimeContext, p.serverType)
						}
					}
				}()
				if up, correctRole, version, role, mode, statusTrail, cancelled := p.runtimeContext.TestInstance(ctx, p.serverType, myHostAddress, port, statusChanged); !cancelled {
					if up && correctRole {
						msgPostfix := ""

						logProcess.Info().Msgf("%s up and running%s (version %s).", p.serverType, msgPostfix, version)
						if (p.serverType == definitions.ServerTypeCoordinator && !p.runtimeContext.IsLocalSlave()) || p.serverType == definitions.ServerTypeSingle {
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
								}

								p.s.logMutex.Lock()
								logProcess.Info().Msgf("Your %s can now be accessed with a browser at `%s://%s:%d` or", what, urlSchemes.Browser, ip, hostPort)
								logProcess.Info().Msgf("using `arangosh --server.endpoint %s://%s:%d`.", urlSchemes.ArangoSH, ip, hostPort)
								p.s.logMutex.Unlock()

								p.runtimeContext.removeRecoveryFile()
							}
						}
					} else if !up {
						logProcess.Warn().Msgf("%s not ready after %s! Status trail: %#v", p.config.InstanceUpTimeout, p.serverType, statusTrail)
					} else if !correctRole {
						expectedRole, expectedMode := p.serverType.ExpectedServerRole()
						logProcess.Warn().Msgf("%s does not have the expected role of '%s,%s' (but '%s,%s'): Status trail: %#v", p.serverType, expectedRole, expectedMode, role, mode, statusTrail)
					}
				}
			}()

			procC := proc.WaitCh()

			select {
			case exitCode = <-procC:
				logProcess.Info().Msgf("Terminated %s. Exit code: %d (%s)", p.serverType, exitCode, definitions.GetExitCodeReason(exitCode))
				var failed bool
				failed, failedRestartGen = p.recordAutoUpgradeExit(p.runtimeContext.UpgradeManager(), logProcess, exitCode, func() string {
					return p.serverLogSince(logMark)
				}, output)
				autoUpgradeFailed = failed
				break
			case <-p.stopping:
				var initialTimeout time.Duration
				if p.s.stopping {
					// Starter is being closed, so we add timeout
					initialTimeout = time.Second
				}
				exitCode = terminateProcessWithActions(logProcess, p.proc, p.serverType, initialTimeout, time.Minute)

				// ensure we read from procC channel
				<-procC
				break
			}
			cancel()
		}
		if autoUpgradeFailed {
			// failedRestartGen was taken before the failure was published.
			// A retry that arrives while that publication or its log is
			// printed is newer than this value and releases the pause. The
			// notification that started this attempt is already included.
			if err := p.waitForRestartSince(failedRestartGen); err != nil {
				break
			}
			logProcess.Info().Msgf("restarting %s", p.serverType)
			restart++
			continue
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
					p.showProcessOutput(logProcess, output, p.serverType)
					p.s.showRecentLogs(logProcess, p.runtimeContext, p.serverType)
				}
				retryingWillNotHelp := !definitions.ExitCodeIsRecoverable(p.serverType.ProcessType(), exitCode)
				if retryingWillNotHelp || recentFailures >= definitions.MaxRecentFailures {
					logProcess.Error().Msgf(
						"%s has failed %d times, giving up. Recent exit code: %d (%s)",
						p.serverType, recentFailures, exitCode, definitions.GetExitCodeReason(exitCode),
					)
					p.runtimeContext.Stop()
					p.s.stopping = true
					break
				}
			} else {
				logProcess.Info().Msgf("%s has terminated", p.serverType)
				if p.config.DebugCluster && !p.s.stopping {
					p.showProcessOutput(logProcess, output, p.serverType)
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

// recordAutoUpgradeExit publishes a non-zero auto-upgrade exit. The returned
// generation is taken before the log is read and before the failure is
// published, so a retry that arrives during either step is not treated as
// the baseline of the pause.
func (p *processWrapper) recordAutoUpgradeExit(upgrade UpgradeManager, logProcess zerolog.Logger, exitCode int, readLog func() string, output *utils.LimitedBuffer) (bool, uint64) {
	seen := p.restartGen.Load()
	if upgrade == nil {
		return false, seen
	}
	// A successful exit, or the SIGTERM of the previous server, does not
	// need the server log. IsServerUpgradeInProgress is already true then.
	if exitCode == 0 || !upgrade.IsServerAutoUpgradeRunning(p.serverType) {
		upgrade.ServerDatabaseAutoUpgradeFinished(p.serverType, exitCode, "")
		return false, seen
	}
	attemptLog := ""
	if readLog != nil {
		attemptLog = readLog()
	}
	if !upgrade.ServerDatabaseAutoUpgradeFinished(p.serverType, exitCode, attemptLog) {
		return false, seen
	}
	logProcess.Error().Msgf("%s auto-upgrade failed with exit code %d (%s)", p.serverType, exitCode, definitions.GetExitCodeReason(exitCode))
	if output != nil {
		p.showProcessOutput(logProcess, output, p.serverType)
	}
	if attemptLog != "" {
		logProcess.Info().Msg(formatRecentServerLog(p.serverType, attemptLog))
	} else if p.s != nil {
		p.s.showRecentLogs(logProcess, p.runtimeContext, p.serverType)
	}
	return true, seen
}

// waitForRestartSince blocks until a RequestRestart newer than seen arrives,
// or the wrapper is stopping. seen must be the generation after the attempt
// that just failed, so the notification that started that attempt does not
// release the pause.
func (p *processWrapper) waitForRestartSince(seen uint64) error {
	for {
		if p.restartGen.Load() != seen {
			return nil
		}
		select {
		case <-p.stopping:
			return fmt.Errorf("stopping")
		case <-p.ctx.Done():
			return p.ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (p *processWrapper) serverLogSince(mark logFileMark) string {
	logPath, err := p.runtimeContext.serverHostLogFile(p.serverType)
	if err != nil {
		return ""
	}
	body, err := readServerLogSinceMark(logPath, mark)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(body)
}

func (p *processWrapper) showProcessOutput(log zerolog.Logger, b *utils.LimitedBuffer, serverType definitions.ServerType) {
	result := &bytes.Buffer{}
	result.WriteString(fmt.Sprintf("## Start of %s output:\n", serverType))
	for _, line := range strings.Split(b.String(), "\n") {
		result.WriteString("\t")
		result.WriteString(line)
		result.WriteByte('\n')
	}
	result.WriteString(fmt.Sprintf("## End of %s output.", serverType))

	log.Info().Msg(result.String())
}
