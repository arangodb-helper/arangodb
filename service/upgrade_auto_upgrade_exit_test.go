//
// DISCLAIMER
//
// Copyright 2026 ArangoDB GmbH, Cologne, Germany
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
	"fmt"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/arangodb-helper/arangodb/pkg/definitions"
	"github.com/arangodb-helper/arangodb/pkg/utils"
	"github.com/arangodb/go-driver"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

type standaloneUpgradeTestContext struct {
	UpgradeManagerContext
	peer      Peer
	wrapper   *processWrapper
	restarts  chan definitions.ServerType
	online    atomic.Bool
	queries   atomic.Int32
	premature atomic.Int32
}

func (c *standaloneUpgradeTestContext) ClusterConfig() (ClusterConfig, *Peer, ServiceMode) {
	return ClusterConfig{AllPeers: []Peer{c.peer}}, &c.peer, ServiceModeSingle
}

func (c *standaloneUpgradeTestContext) RestartServer(serverType definitions.ServerType) error {
	c.wrapper.RequestRestart()
	c.restarts <- serverType
	return nil
}

func (c *standaloneUpgradeTestContext) CreateClient(_ []string, _ ConnectionType, _ definitions.ServerType) (driver.Client, error) {
	c.queries.Add(1)
	if !c.online.Load() {
		c.premature.Add(1)
		return nil, fmt.Errorf("database is stopped")
	}
	return standaloneVersionClient{}, nil
}

type standaloneVersionClient struct{ driver.Client }

func (standaloneVersionClient) Version(context.Context) (driver.VersionInfo, error) {
	return driver.VersionInfo{Version: driver.Version("3.12.13")}, nil
}

func TestStandaloneUpgradeFailureCanRetryWithoutDatabase(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p := &processWrapper{ctx: ctx, stopping: make(chan struct{}), serverType: definitions.ServerTypeSingle}
	c := &standaloneUpgradeTestContext{
		peer:    Peer{ID: "single", Address: "localhost", Port: 8528},
		wrapper: p, restarts: make(chan definitions.ServerType, 4),
	}
	m := &upgradeManager{log: zerolog.Nop(), upgradeManagerContext: c}

	// No previous failure: retry must not start an arbitrary upgrade.
	require.ErrorContains(t, m.RetryDatabaseUpgrade(ctx), "no failed single server upgrade")

	// Launch the initial attempt after preflight, then simulate its child exit.
	m.mutex.Lock()
	m.startSingleServerUpgrade(&c.peer, ServiceModeSingle)
	m.mutex.Unlock()
	waitRestart := func() {
		t.Helper()
		select {
		case serverType := <-c.restarts:
			require.Equal(t, definitions.ServerType(definitions.ServerTypeSingle), serverType)
		case <-ctx.Done():
			t.Fatal("standalone upgrade did not request a restart")
		}
		require.True(t, m.ServerDatabaseAutoUpgrade(definitions.ServerTypeSingle, 0))
		m.ServerDatabaseAutoUpgradeStarter(definitions.ServerTypeSingle)
	}
	waitDone := func(failed bool) {
		t.Helper()
		require.Eventually(t, func() bool {
			m.mutex.Lock()
			defer m.mutex.Unlock()
			return !m.singleUpgradeInProgress && (m.singleUpgradeFailure != nil) == failed
		}, 2*time.Second, 10*time.Millisecond)
	}
	waitRestart()
	failed, seen := p.recordAutoUpgradeExit(m, zerolog.Nop(), 1, func() string {
		return "ERROR failed to open _system"
	}, nil)
	require.True(t, failed)
	waitDone(true)
	m.mutex.Lock()
	savedFailure := m.singleUpgradeFailure
	m.mutex.Unlock()
	require.ErrorContains(t, savedFailure, "failed to open _system")
	require.False(t, m.IsServerUpgradeInProgress(definitions.ServerTypeSingle))

	// Start must direct the caller to retry before querying the stopped server.
	require.ErrorContains(t, m.StartDatabaseUpgrade(ctx, false), "retry upgrade")
	require.Zero(t, c.queries.Load())

	// An HTTP request ending must not cancel the background retry.
	requestCtx, cancelRequest := context.WithCancel(ctx)
	require.NoError(t, m.RetryDatabaseUpgrade(requestCtx))
	cancelRequest()
	waitRestart()
	require.NoError(t, p.waitForRestartSince(seen))
	require.Zero(t, c.queries.Load())
	require.ErrorContains(t, m.RetryDatabaseUpgrade(ctx), "already in progress")
	require.ErrorContains(t, m.StartDatabaseUpgrade(ctx, false), "already in progress")

	// A repeated failure must remain retryable.
	failed, seen = p.recordAutoUpgradeExit(m, zerolog.Nop(), -1, func() string {
		return "FATAL second attempt failed"
	}, nil)
	require.True(t, failed)
	waitDone(true)
	require.NoError(t, m.RetryDatabaseUpgrade(ctx))
	waitRestart()
	require.NoError(t, p.waitForRestartSince(seen))
	require.Zero(t, c.queries.Load())

	// After exit 0 the normal server comes up and readiness can query it.
	c.online.Store(true)
	require.True(t, m.ServerDatabaseAutoUpgradeFinished(definitions.ServerTypeSingle, 0, ""))
	waitDone(false)
	require.GreaterOrEqual(t, c.queries.Load(), int32(2))
	require.Zero(t, c.premature.Load())
	require.ErrorContains(t, m.RetryDatabaseUpgrade(ctx), "no failed single server upgrade")
}

type unavailableAgencyContext struct {
	UpgradeManagerContext
	peer     Peer
	wrapper  *processWrapper
	restarts chan definitions.ServerType
	reads    atomic.Int32
}

func (c *unavailableAgencyContext) ClusterConfig() (ClusterConfig, *Peer, ServiceMode) {
	return ClusterConfig{AllPeers: []Peer{c.peer}, AgencySize: 1}, &c.peer, ServiceModeCluster
}

func (c *unavailableAgencyContext) RestartServer(serverType definitions.ServerType) error {
	c.wrapper.RequestRestart()
	c.restarts <- serverType
	return nil
}

func (c *unavailableAgencyContext) IsRunningMaster() (bool, bool, string) {
	return true, true, ""
}

func (c *unavailableAgencyContext) CreateClient(_ []string, connectionType ConnectionType, _ definitions.ServerType) (driver.Client, error) {
	if connectionType == ConnectionTypeAgency {
		c.reads.Add(1)
		return nil, fmt.Errorf("agency is stopped")
	}
	return nil, fmt.Errorf("unexpected client")
}

func TestSoleAgentFailureIsReportedAndRetriedWithoutAgency(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p := &processWrapper{ctx: ctx, stopping: make(chan struct{}), serverType: definitions.ServerTypeAgent}
	c := &unavailableAgencyContext{
		peer:     Peer{ID: "peer1", Address: "10.0.0.2", Port: 8528, peerServers: peerServers{HasAgentFlag: true}},
		wrapper:  p,
		restarts: make(chan definitions.ServerType, 2),
	}
	m := &upgradeManager{log: zerolog.Nop(), upgradeManagerContext: c}
	plan := UpgradePlan{
		FromVersions: []driver.Version{"3.11.14"},
		ToVersion:    "3.12.13",
		Entries:      []UpgradePlanEntry{{Type: UpgradeEntryTypeAgent, PeerID: "peer1"}},
	}

	waitRestart := func() {
		t.Helper()
		select {
		case serverType := <-c.restarts:
			require.Equal(t, definitions.ServerType(definitions.ServerTypeAgent), serverType)
		case <-ctx.Done():
			t.Fatal("sole-agent upgrade did not request a restart")
		}
		require.True(t, m.ServerDatabaseAutoUpgrade(definitions.ServerTypeAgent, 0))
		m.ServerDatabaseAutoUpgradeStarter(definitions.ServerTypeAgent)
	}

	done := make(chan error, 1)
	go func() {
		done <- m.processUpgradePlan(ctx, plan)
	}()
	waitRestart()
	failed, seen := p.recordAutoUpgradeExit(m, zerolog.Nop(), 1, func() string {
		return "ERROR agent auto-upgrade failed"
	}, nil)
	require.True(t, failed)
	select {
	case err := <-done:
		require.ErrorContains(t, err, "auto-upgrade exited with code 1")
	case <-ctx.Done():
		t.Fatal("failed sole-agent upgrade was not reported")
	}
	require.Greater(t, c.reads.Load(), int32(0))

	status, err := m.Status(ctx)
	require.NoError(t, err)
	require.True(t, status.Failed)
	require.Contains(t, status.Reason, "auto-upgrade exited with code 1")
	require.Contains(t, status.Reason, "agent auto-upgrade failed")
	require.Equal(t, driver.Version("3.12.13"), status.ToVersion)
	require.ErrorContains(t, m.StartDatabaseUpgrade(ctx, false), "retry upgrade")

	requestCtx, cancelRequest := context.WithCancel(ctx)
	require.NoError(t, m.RetryDatabaseUpgrade(requestCtx))
	cancelRequest()
	waitRestart()
	require.NoError(t, p.waitForRestartSince(seen))
	require.True(t, m.IsServerUpgradeInProgress(definitions.ServerTypeAgent))
	require.True(t, m.ServerDatabaseAutoUpgradeFinished(definitions.ServerTypeAgent, 0, ""))
}

func TestAutoUpgradeStopBeforeStartIsNotAFailure(t *testing.T) {
	m := &upgradeManager{}
	m.beginServerUpgrade(definitions.ServerTypeDBServer)

	require.False(t, m.ServerDatabaseAutoUpgradeFinished(definitions.ServerTypeDBServer, -1, "terminated"))
	require.True(t, m.ServerDatabaseAutoUpgrade(definitions.ServerTypeDBServer, 0))
	require.True(t, m.IsServerUpgradeInProgress(definitions.ServerTypeDBServer))
	require.False(t, m.IsServerAutoUpgradeRunning(definitions.ServerTypeDBServer))
}

func TestExpectedStopDoesNotReadServerLog(t *testing.T) {
	m := &upgradeManager{}
	m.beginServerUpgrade(definitions.ServerTypeDBServer)

	p := &processWrapper{
		ctx:        context.Background(),
		stopping:   make(chan struct{}),
		serverType: definitions.ServerTypeDBServer,
	}
	read := false
	failed, _ := p.recordAutoUpgradeExit(m, zerolog.Nop(), -1, func() string {
		read = true
		return "previous server log"
	}, nil)
	require.False(t, failed)
	require.False(t, read)
}

func TestAutoUpgradeExitZeroContinues(t *testing.T) {
	m := &upgradeManager{}
	m.beginServerUpgrade(definitions.ServerTypeDBServer)
	m.ServerDatabaseAutoUpgradeStarter(definitions.ServerTypeDBServer)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- m.waitUntilUpgradeServerStarted(ctx)
	}()

	select {
	case err := <-done:
		t.Fatalf("wait returned before the auto-upgrade process exited: %v", err)
	case <-time.After(200 * time.Millisecond):
	}

	require.True(t, m.ServerDatabaseAutoUpgradeFinished(definitions.ServerTypeDBServer, 0, ""))
	require.False(t, m.ServerDatabaseAutoUpgrade(definitions.ServerTypeDBServer, 0))

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("wait did not return after exit 0")
	}
}

func TestAutoUpgradeNonZeroExitFails(t *testing.T) {
	for _, exitCode := range []int{1, 10, -1} {
		m := &upgradeManager{}
		m.beginServerUpgrade(definitions.ServerTypeDBServer)
		m.ServerDatabaseAutoUpgradeStarter(definitions.ServerTypeDBServer)

		serverLog := "FATAL [67bdc] {startup} failed during auto-upgrade"
		require.True(t, m.ServerDatabaseAutoUpgradeFinished(definitions.ServerTypeDBServer, exitCode, serverLog))
		require.False(t, m.ServerDatabaseAutoUpgradeFinished(definitions.ServerTypeDBServer, exitCode, serverLog))

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		err := m.waitUntilUpgradeServerStarted(ctx)
		cancel()

		require.Error(t, err)
		require.Contains(t, err.Error(), fmt.Sprintf("auto-upgrade exited with code %d", exitCode))
		require.Contains(t, err.Error(), serverLog)
	}
}

func TestAutoUpgradeFailureReasonKeepsFatalAndTail(t *testing.T) {
	m := &upgradeManager{}
	m.beginServerUpgrade(definitions.ServerTypeDBServer)
	m.ServerDatabaseAutoUpgradeStarter(definitions.ServerTypeDBServer)

	var log strings.Builder
	original := "ERROR [abcde] {engines} failed to open '_system': shutdown in progress"
	log.WriteString(original)
	log.WriteByte('\n')
	for i := 0; i < 80; i++ {
		log.WriteString("ERROR [pad] ")
		log.WriteString(strings.Repeat("x", 80))
		log.WriteByte('\n')
	}
	fatal := "FATAL [67bdc] {startup} assertion failed"
	tail := "INFO [tail] actual-log-tail-marker"
	log.WriteString(fatal)
	log.WriteByte('\n')
	log.WriteString(tail)

	require.True(t, m.ServerDatabaseAutoUpgradeFinished(definitions.ServerTypeDBServer, 1, log.String()))
	err := m.waitUntilUpgradeServerStarted(context.Background())
	require.Error(t, err)
	reason := err.Error()
	require.LessOrEqual(t, len(reason), autoUpgradeReasonMaxLen+len("\n..."))
	require.Contains(t, reason, "auto-upgrade exited with code 1")
	require.Contains(t, reason, original)
	require.Contains(t, reason, fatal)
	require.Contains(t, reason, tail)
}

func TestWaitForRestartSince(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := &processWrapper{
		ctx:      ctx,
		stopping: make(chan struct{}),
	}
	p.restartGen.Store(3)

	done := make(chan error, 1)
	go func() {
		done <- p.waitForRestartSince(3)
	}()

	select {
	case err := <-done:
		t.Fatalf("returned before RequestRestart: %v", err)
	case <-time.After(150 * time.Millisecond):
	}

	p.RequestRestart()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("did not return after RequestRestart")
	}
}

func TestResetFailuresClearsEntries(t *testing.T) {
	plan := UpgradePlan{
		Entries: []UpgradePlanEntry{
			{Type: UpgradeEntryTypeDBServer, Failures: 1, Reason: "dbserver auto-upgrade exited with code 1"},
		},
	}
	plan.ResetFailures()
	require.Equal(t, 0, plan.Entries[0].Failures)
	require.Empty(t, plan.Entries[0].Reason)
	require.False(t, plan.IsFailed())
}

type coordinatorHealthTestContext struct {
	UpgradeManagerContext
	config      ClusterConfig
	contacted   []string
	dbUnhealthy bool
}

func (c *coordinatorHealthTestContext) ClusterConfig() (ClusterConfig, *Peer, ServiceMode) {
	peer := c.config.AllPeers[0]
	return c.config, &peer, ServiceModeCluster
}

func (c *coordinatorHealthTestContext) CreateClient(endpoints []string, connectionType ConnectionType, _ definitions.ServerType) (driver.Client, error) {
	c.contacted = append(c.contacted, endpoints...)
	for _, ep := range endpoints {
		if strings.Contains(ep, ":8529") {
			return nil, fmt.Errorf("coordinator is stopped")
		}
	}
	if connectionType == ConnectionTypeDatabase && c.dbUnhealthy {
		return nil, fmt.Errorf("dbserver is down")
	}
	return pingClient{}, nil
}

type pingClient struct{ driver.Client }

func (pingClient) Version(context.Context) (driver.VersionInfo, error) {
	return driver.VersionInfo{Version: "3.12.13"}, nil
}

func (pingClient) ServerID(context.Context) (string, error) {
	return "db1", nil
}

func TestClusterHealthSkipsStoppedCoordinator(t *testing.T) {
	ctx := context.Background()
	peer := Peer{ID: "peer1", Address: "10.0.0.2", Port: 8528, peerServers: peerServers{HasAgentFlag: true}}
	c := &coordinatorHealthTestContext{config: ClusterConfig{AllPeers: []Peer{peer}, AgencySize: 1}}
	m := &upgradeManager{log: zerolog.Nop(), upgradeManagerContext: c}
	allowed := []stoppedServer{{role: driver.ServerRoleCoordinator, address: "10.0.0.2", port: 8529}}

	require.NoError(t, m.isClusterHealthyExcept(ctx, allowed))
	require.NotContains(t, strings.Join(c.contacted, ","), ":8529")
	require.Contains(t, strings.Join(c.contacted, ","), ":8531")
	require.Contains(t, strings.Join(c.contacted, ","), ":8530")

	c.contacted = nil
	c.dbUnhealthy = true
	require.ErrorContains(t, m.isClusterHealthyExcept(ctx, allowed), "dbserver is down")
}

func TestFailureIsExpectedOnlyForStoppedServer(t *testing.T) {
	allowed := []stoppedServer{{role: driver.ServerRoleDBServer, address: "10.0.0.2", port: 8530}}
	down := driver.ServerHealth{Role: driver.ServerRoleDBServer, Endpoint: "tcp://10.0.0.2:8530", Status: "FAILED"}
	samePortOtherHost := driver.ServerHealth{Role: driver.ServerRoleDBServer, Endpoint: "tcp://10.0.0.8:8530", Status: "FAILED"}
	coordinator := driver.ServerHealth{Role: driver.ServerRoleCoordinator, Endpoint: "tcp://10.0.0.2:8529", Status: "FAILED"}

	require.True(t, failureIsExpected(down, allowed))
	require.False(t, failureIsExpected(samePortOtherHost, allowed))
	require.False(t, failureIsExpected(coordinator, allowed))
}

func TestRecentServerLogExcludesHistoricalErrors(t *testing.T) {
	path := t.TempDir() + "/arangod.log"
	previous := "2026-10-26T10:00:00Z ERROR previous attempt failed\n2026-10-26T10:00:01Z FATAL previous attempt crashed\n"
	// Keep repeated and blank lines in the tail, including a final line
	// without a newline, just as they appear in the log.
	tail := strings.Repeat("INFO current attempt\n", 17) + "\nERROR current failure\nINFO final line"
	require.NoError(t, os.WriteFile(path, []byte(previous+tail), 0600))

	text, err := readRecentServerLog(path)
	require.NoError(t, err)
	require.Equal(t, tail, text)

	// Auto-upgrade diagnostics still retain errors before the tail.
	text, err = readServerLogSince(path, 0)
	require.NoError(t, err)
	require.Contains(t, text, "ERROR previous attempt failed")
	require.Contains(t, text, "FATAL previous attempt crashed")
}

func TestServerLogKeepsOriginalError(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/arangod.log"
	var body strings.Builder
	body.WriteString("2026-10-26T10:51:46Z ERROR [abcde] {engines} failed to open '_system': shutdown in progress\n")
	for i := 0; i < 40; i++ {
		body.WriteString("2026-10-26T10:51:47Z INFO [stack] frame ")
		body.WriteString(strings.Repeat("x", 20))
		body.WriteByte('\n')
	}
	body.WriteString("2026-10-26T10:51:48Z FATAL [67bdc] {startup} assertion failed\n")
	require.NoError(t, os.WriteFile(path, []byte(body.String()), 0600))

	text, err := readServerLogSince(path, 0)
	require.NoError(t, err)
	require.Contains(t, text, "failed to open '_system': shutdown in progress")
	require.Contains(t, text, "FATAL [67bdc]")

	previous := []byte("2026-10-26T10:00:00Z INFO [old] previous attempt\n")
	require.NoError(t, os.WriteFile(path, append(previous, []byte(body.String())...), 0600))
	text, err = readServerLogSince(path, int64(len(previous)))
	require.NoError(t, err)
	require.NotContains(t, text, "previous attempt")
	require.Contains(t, text, "failed to open '_system'")
}

type staticLogContext struct {
	runtimeServerManagerContext
	logPath string
}

func (c staticLogContext) serverHostLogFile(definitions.ServerType) (string, error) {
	return c.logPath, nil
}

func TestServerHostLogOffsetIsFileSize(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/arangod.log"
	require.NoError(t, os.WriteFile(path, []byte("previous server shutdown\n"), 0600))
	mark := serverHostLogMark(staticLogContext{logPath: path}, definitions.ServerTypeDBServer)
	require.Equal(t, int64(len("previous server shutdown\n")), mark.size)
}

func TestServerLogKeepsErrorAcrossRotation(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/arangod.log"
	previous := []byte("2026-10-26T10:00:00Z INFO previous server still running\n")
	require.NoError(t, os.WriteFile(path, previous, 0600))
	mark := logFileMarkFromInfo(os.Stat(path))

	require.NoError(t, os.Rename(path, path+".1"))
	rotated, err := os.OpenFile(path+".1", os.O_APPEND|os.O_WRONLY, 0600)
	require.NoError(t, err)
	_, err = rotated.WriteString("2026-10-26T10:51:46Z ERROR [abcde] {engines} failed to open '_system': shutdown in progress\n")
	require.NoError(t, err)
	require.NoError(t, rotated.Close())

	require.NoError(t, os.WriteFile(path, []byte("2026-10-26T10:51:48Z FATAL [67bdc] {startup} assertion failed\n"), 0600))

	text, err := readServerLogSinceMark(path, mark)
	require.NoError(t, err)
	require.NotContains(t, text, "previous server still running")
	require.Contains(t, text, "failed to open '_system': shutdown in progress")
	require.Contains(t, text, "FATAL [67bdc]")
}

func TestServerLogKeepsTailPastOneMegabyte(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/arangod.log"
	f, err := os.Create(path)
	require.NoError(t, err)

	_, err = io.WriteString(f, "2026-10-26T10:51:46Z ERROR [abcde] {engines} failed to open '_system': shutdown in progress\n")
	require.NoError(t, err)
	pad := []byte("2026-10-26T10:51:47Z INFO [stack] padding line for a long auto-upgrade log\n")
	written := 0
	for written < (1<<20)+len(pad) {
		n, err := f.Write(pad)
		require.NoError(t, err)
		written += n
	}
	_, err = io.WriteString(f, "2026-10-26T10:51:48Z ERROR [late] {engines} error after the first megabyte\n")
	require.NoError(t, err)
	_, err = io.WriteString(f, "2026-10-26T10:51:49Z INFO [tail] actual-log-tail-marker\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	text, err := readServerLogSince(path, 0)
	require.NoError(t, err)
	require.Contains(t, text, "failed to open '_system': shutdown in progress")
	require.Contains(t, text, "error after the first megabyte")
	require.Contains(t, text, "actual-log-tail-marker")
	require.Less(t, strings.Count(text, "\n")+1, 80)
}

type retryOnPublishUpgrade struct {
	UpgradeManager
	onFinish func()
}

func (u retryOnPublishUpgrade) ServerDatabaseAutoUpgradeFinished(serverType definitions.ServerType, exitCode int, serverLog string) bool {
	ok := u.UpgradeManager.ServerDatabaseAutoUpgradeFinished(serverType, exitCode, serverLog)
	if u.onFinish != nil {
		u.onFinish()
	}
	return ok
}

func TestRetryDuringFailurePublicationIsNotLost(t *testing.T) {
	m := &upgradeManager{}
	m.beginServerUpgrade(definitions.ServerTypeDBServer)
	m.ServerDatabaseAutoUpgradeStarter(definitions.ServerTypeDBServer)

	p := &processWrapper{
		ctx:        context.Background(),
		stopping:   make(chan struct{}),
		serverType: definitions.ServerTypeDBServer,
	}
	p.restartGen.Store(4)

	upgrade := retryOnPublishUpgrade{UpgradeManager: m, onFinish: p.RequestRestart}
	failed, seen := p.recordAutoUpgradeExit(upgrade, zerolog.Nop(), 1, func() string {
		return "2026-10-26T10:51:46Z ERROR [abcde] {engines} failed to open '_system': shutdown in progress"
	}, utils.NewLimitedBuffer(80))

	require.True(t, failed)
	require.Equal(t, uint64(4), seen)
	require.Equal(t, uint64(5), p.restartGen.Load())

	done := make(chan error, 1)
	go func() {
		done <- p.waitForRestartSince(seen)
	}()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("retry that arrived while the failure was published did not restart the server")
	}
}

func TestAutoUpgradeFailureStaysStoppedUntilRetry(t *testing.T) {
	m := &upgradeManager{}
	m.beginServerUpgrade(definitions.ServerTypeDBServer)
	m.ServerDatabaseAutoUpgradeStarter(definitions.ServerTypeDBServer)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	upgradeDone := make(chan error, 1)
	go func() {
		upgradeDone <- m.waitUntilUpgradeServerStarted(ctx)
	}()
	select {
	case err := <-upgradeDone:
		t.Fatalf("upgrade finished before the auto-upgrade process exited: %v", err)
	case <-time.After(150 * time.Millisecond):
	}

	reasonLog := "2026-10-26T10:51:46Z ERROR [abcde] {engines} failed to open '_system': shutdown in progress\n2026-10-26T10:51:48Z FATAL [67bdc] {startup} assertion failed"
	require.True(t, m.ServerDatabaseAutoUpgradeFinished(definitions.ServerTypeDBServer, 1, reasonLog))

	var upgradeErr error
	select {
	case upgradeErr = <-upgradeDone:
	case <-time.After(time.Second):
		t.Fatal("failure was not reported")
	}
	require.Error(t, upgradeErr)
	require.Contains(t, upgradeErr.Error(), "auto-upgrade exited with code 1")
	require.Contains(t, upgradeErr.Error(), "failed to open '_system'")
	require.Contains(t, upgradeErr.Error(), "FATAL [67bdc]")

	p := &processWrapper{
		ctx:      context.Background(),
		stopping: make(chan struct{}),
	}
	p.restartGen.Store(4)
	restarted := make(chan error, 1)
	go func() {
		restarted <- p.waitForRestartSince(4)
	}()
	select {
	case err := <-restarted:
		t.Fatalf("server restarted without an explicit retry: %v", err)
	case <-time.After(150 * time.Millisecond):
	}

	p.RequestRestart()
	select {
	case err := <-restarted:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("explicit retry did not restart the stopped server")
	}
}
