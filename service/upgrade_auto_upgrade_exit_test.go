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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/arangodb/go-driver"
	driver_http "github.com/arangodb/go-driver/http"

	"github.com/arangodb-helper/arangodb/client"
	"github.com/arangodb-helper/arangodb/pkg/definitions"
	"github.com/arangodb-helper/arangodb/pkg/utils"
)

func init() {
	// As in main.go, so agency.IsKeyNotFound sees through maskAny.
	driver.WithStack = errors.WithStack
	driver.Cause = errors.Cause
}

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
	status, err := m.Status(ctx)
	require.NoError(t, err)
	require.True(t, status.Failed)
	require.Contains(t, status.Reason, "failed to open _system")
	require.Len(t, status.ServersRemaining, 1)

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
	// The CLI keeps polling while the attempt runs.
	status, err = m.Status(ctx)
	require.NoError(t, err)
	require.False(t, status.Failed)
	require.False(t, status.Ready)
	require.Len(t, status.ServersRemaining, 1)
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
	status, err = m.Status(ctx)
	require.NoError(t, err)
	require.True(t, status.Ready)
	require.Len(t, status.ServersUpgraded, 1)
	require.Empty(t, status.ServersRemaining)
	// The CLI removes the plan when it sees Ready. Without an agency this
	// only clears the finished status.
	require.NoError(t, m.AbortDatabaseUpgrade(ctx))
	_, err = m.Status(ctx)
	require.True(t, client.IsPreconditionFailed(err))
	require.True(t, client.IsBadRequest(m.AbortDatabaseUpgrade(ctx)))
}

type unavailableAgencyContext struct {
	UpgradeManagerContext
	peer     Peer
	wrapper  *processWrapper
	restarts chan definitions.ServerType
	reads    atomic.Int32
	// agency, if set, serves agency requests instead of failing them.
	agency *httptest.Server
}

// planAgency is a fake agency holding only the upgrade plan.
type planAgency struct {
	mu   sync.Mutex
	plan *UpgradePlan // nil: the plan key does not exist
	hang bool         // block every request until it is canceled
	// failPlanReads fails reads of the upgrade plan; other reads succeed.
	failPlanReads bool
}

func (a *planAgency) setPlan(plan *UpgradePlan) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.plan = plan
}

func (a *planAgency) start(t *testing.T) *httptest.Server {
	t.Helper()
	// Close waits for running handlers; release hanging ones first.
	release := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		hang, plan, failPlanReads := a.hang, a.plan, a.failPlanReads
		a.mu.Unlock()
		if hang {
			select {
			case <-r.Context().Done():
			case <-release:
			}
			return
		}
		if r.URL.Path == "/_api/agency/write" {
			a.write(w, r)
			return
		}
		if r.URL.Path != "/_api/agency/read" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if failPlanReads {
			if body, _ := io.ReadAll(r.Body); strings.Contains(string(body), "upgrade-plan") {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
		}
		// A healthy cluster for the remaining-server check.
		health := map[string]interface{}{"PRMR-1": map[string]interface{}{"Role": "DBServer", "Status": "GOOD"}}
		root := map[string]interface{}{"arango": map[string]interface{}{"Supervision": map[string]interface{}{"Health": health}}}
		if plan != nil {
			root["arangodb-helper"] = map[string]interface{}{"arangodb": map[string]interface{}{"upgrade-plan": plan}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]interface{}{root})
	}))
	t.Cleanup(ts.Close)
	t.Cleanup(func() { close(release) })
	return ts
}

// write stores a written upgrade plan, honoring the revision condition.
func (a *planAgency) write(w http.ResponseWriter, r *http.Request) {
	// A transaction is [operations, preconditions, client ID].
	var txns [][]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&txns); err != nil || len(txns) != 1 || len(txns[0]) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	var ops, conds map[string]json.RawMessage
	if err := json.Unmarshal(txns[0][0], &ops); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if len(txns[0]) > 1 {
		_ = json.Unmarshal(txns[0][1], &conds)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if cond, ok := conds["/arangodb-helper/arangodb/upgrade-plan/revision"]; ok {
		var c struct {
			Old *int `json:"old"`
		}
		if err := json.Unmarshal(cond, &c); err != nil || c.Old == nil || a.plan == nil || a.plan.Revision != *c.Old {
			w.WriteHeader(http.StatusPreconditionFailed)
			_, _ = w.Write([]byte(`{"results":[0]}`))
			return
		}
	}
	if op, ok := ops["/arangodb-helper/arangodb/upgrade-plan"]; ok {
		var set struct {
			New *UpgradePlan `json:"new"`
		}
		if err := json.Unmarshal(op, &set); err != nil || set.New == nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		a.plan = set.New
	}
	_, _ = w.Write([]byte(`{"results":[1]}`))
}

// current returns a copy of the stored plan.
func (a *planAgency) current() UpgradePlan {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.plan == nil {
		return UpgradePlan{}
	}
	return a.plan.clone()
}

func shortenAgencyTimeout(t *testing.T) {
	old := localFailureAgencyTimeout
	localFailureAgencyTimeout = 300 * time.Millisecond
	t.Cleanup(func() { localFailureAgencyTimeout = old })
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
		if c.agency != nil {
			conn, err := driver_http.NewConnection(driver_http.ConnectionConfig{Endpoints: []string{c.agency.URL}})
			if err != nil {
				return nil, err
			}
			return driver.NewClient(driver.ClientConfig{Connection: conn})
		}
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
	// The caller's plan is not changed, and the failure is published only
	// after this run has cleaned up its upgrade state.
	require.Zero(t, plan.Entries[0].Failures)
	require.True(t, m.HasLocalUpgradeFailure())
	require.False(t, m.IsServerUpgradeInProgress(definitions.ServerTypeAgent))

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
	// agency answers agency reads with health as the supervision health.
	agency *httptest.Server
	health map[string]driver.ServerHealth
}

// newFakeAgency serves /_api/agency/read with the supervision health of c.
func (c *coordinatorHealthTestContext) newFakeAgency(t *testing.T) {
	t.Helper()
	c.agency = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body interface{}
		switch r.URL.Path {
		case "/_api/version":
			body = map[string]string{"server": "arango", "version": "3.12.13"}
		case "/_api/agency/read":
			body = []interface{}{map[string]interface{}{
				"arango": map[string]interface{}{"Supervision": map[string]interface{}{"Health": c.health}},
			}}
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(c.agency.Close)
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
	if connectionType == ConnectionTypeAgency && c.agency != nil {
		conn, err := driver_http.NewConnection(driver_http.ConnectionConfig{Endpoints: []string{c.agency.URL}})
		if err != nil {
			return nil, err
		}
		return driver.NewClient(driver.ClientConfig{Connection: conn})
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
	c.health = map[string]driver.ServerHealth{
		"PRMR-1": {Endpoint: "tcp://10.0.0.2:8530", Status: driver.ServerStatusGood},
		"CRDN-1": {Endpoint: "tcp://10.0.0.2:8529", Status: driver.ServerStatusFailed},
	}
	c.newFakeAgency(t)
	m := &upgradeManager{log: zerolog.Nop(), upgradeManagerContext: c}
	allowed := []stoppedServer{{role: driver.ServerRoleCoordinator, address: "10.0.0.2", port: 8529}}

	// The stopped coordinator is not contacted, and its FAILED supervision
	// status is expected.
	require.NoError(t, m.isClusterHealthyExcept(ctx, allowed))
	require.NotContains(t, strings.Join(c.contacted, ","), ":8529")
	require.Contains(t, strings.Join(c.contacted, ","), ":8531")
	require.Contains(t, strings.Join(c.contacted, ","), ":8530")

	c.contacted = nil
	c.dbUnhealthy = true
	require.ErrorContains(t, m.isClusterHealthyExcept(ctx, allowed), "dbserver is down")

	// A DBServer that still answers but that supervision reports as BAD
	// must block the retry, as the normal cluster health check does.
	c.dbUnhealthy = false
	c.health["PRMR-1"] = driver.ServerHealth{Endpoint: "tcp://10.0.0.2:8530", Status: driver.ServerStatusBad}
	require.ErrorContains(t, m.isClusterHealthyExcept(ctx, allowed), "'PRMR-1' has a 'BAD' status")
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
	mark := logFileMarkAt(path)

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

func TestLocalFailedPlanIsIndependentCopy(t *testing.T) {
	c := &unavailableAgencyContext{peer: Peer{ID: "peer1", Address: "10.0.0.2", Port: 8528}}
	m := &upgradeManager{log: zerolog.Nop(), upgradeManagerContext: c}
	plan := UpgradePlan{Revision: 3, Entries: []UpgradePlanEntry{{Type: UpgradeEntryTypeAgent, PeerID: "peer1", Failures: 1, Reason: "failed"}}}
	m.storeLocalFailedPlan(plan)
	plan.ResetFailures()
	require.True(t, m.HasLocalUpgradeFailure())

	snapshot, ok := m.localFailedPlanSnapshot()
	require.True(t, ok)
	snapshot.ResetFailures()
	status, ok := m.LocalUpgradeFailureStatus()
	require.True(t, ok)
	require.True(t, status.Failed)
	require.Equal(t, "failed", status.Reason)

	taken, ok := m.takeLocalFailedPlan()
	require.True(t, ok)
	m.storeLocalFailedPlan(UpgradePlan{Revision: 4, Entries: []UpgradePlanEntry{{Type: UpgradeEntryTypeAgent, PeerID: "peer1", Failures: 1, Reason: "second"}}})
	taken.ResetFailures()
	taken.Entries[0].Reason = "mutated"
	status, ok = m.LocalUpgradeFailureStatus()
	require.True(t, ok)
	require.Equal(t, "second", status.Reason)
}

func TestLocalRetryWaitsForPreviousRun(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p := &processWrapper{ctx: ctx, stopping: make(chan struct{}), serverType: definitions.ServerTypeAgent}
	c := &unavailableAgencyContext{
		peer:     Peer{ID: "peer1", Address: "10.0.0.2", Port: 8528, peerServers: peerServers{HasAgentFlag: true}},
		wrapper:  p,
		restarts: make(chan definitions.ServerType, 2),
	}
	m := &upgradeManager{log: zerolog.Nop(), upgradeManagerContext: c}
	m.storeLocalFailedPlan(UpgradePlan{Entries: []UpgradePlanEntry{{Type: UpgradeEntryTypeAgent, PeerID: "peer1", Failures: 1}}})

	// A previous run that is still cleaning up holds processMu.
	m.processMu.Lock()
	require.NoError(t, m.RetryLocalUpgradeFailure(ctx))
	require.True(t, m.localUpgradePending())
	select {
	case <-c.restarts:
		t.Fatal("retry started before the previous run finished")
	case <-time.After(100 * time.Millisecond):
	}
	m.processMu.Unlock()
	select {
	case <-c.restarts:
	case <-ctx.Done():
		t.Fatal("retry did not start")
	}
	require.True(t, m.ServerDatabaseAutoUpgrade(definitions.ServerTypeAgent, 0))
	require.ErrorContains(t, m.RetryLocalUpgradeFailure(ctx), "no failed upgrade step")
	cancel()
}

func TestWatcherWaitsForLocalFailureToReconcile(t *testing.T) {
	ctx := context.Background()
	c := &unavailableAgencyContext{peer: Peer{ID: "peer1"}}
	m := &upgradeManager{log: zerolog.Nop(), upgradeManagerContext: c}
	agencyPlan := UpgradePlan{Revision: 3, Entries: []UpgradePlanEntry{{Type: UpgradeEntryTypeAgent, PeerID: "peer1"}}}
	local := agencyPlan.clone()
	local.Entries[0].Failures = 1

	// Without a failure the watcher acts as before.
	require.False(t, m.reconcileLocalFailedPlan(ctx, agencyPlan))

	// While the failure cannot be written, the unfailed agency plan must not
	// restart the parked server.
	m.storeLocalFailedPlan(local)
	require.True(t, m.reconcileLocalFailedPlan(ctx, agencyPlan))
	require.True(t, m.HasLocalUpgradeFailure())
	require.Greater(t, c.reads.Load(), int32(0))

	// The agency already holds a failure.
	failedPlan := agencyPlan.clone()
	failedPlan.Entries[0].Failures = 1
	require.True(t, m.reconcileLocalFailedPlan(ctx, failedPlan))
	require.False(t, m.HasLocalUpgradeFailure())

	// The plan has been replaced.
	m.storeLocalFailedPlan(local)
	replaced := agencyPlan.clone()
	replaced.Revision = 7
	require.False(t, m.reconcileLocalFailedPlan(ctx, replaced))
	require.False(t, m.HasLocalUpgradeFailure())

	// A local retry is running.
	m.localRetryActive = true
	require.True(t, m.reconcileLocalFailedPlan(ctx, agencyPlan))
}

func TestServerLogReadsEveryRotation(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/arangod.log"
	require.NoError(t, os.WriteFile(path, []byte("INFO previous server still running\n"), 0600))
	mark := logFileMarkAt(path)
	require.True(t, mark.known)

	appendTo := func(p, line string) {
		f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
		require.NoError(t, err)
		_, err = f.WriteString(line + "\n")
		require.NoError(t, err)
		require.NoError(t, f.Close())
	}
	rotate := func() {
		if _, err := os.Stat(path + ".1"); err == nil {
			require.NoError(t, os.Rename(path+".1", path+".2"))
		}
		require.NoError(t, os.Rename(path, path+".1"))
	}
	appendTo(path, "2026-10-26T10:51:46Z ERROR [first] before the first rotation")
	rotate()
	appendTo(path, "2026-10-26T10:51:47Z ERROR [between] between the rotations")
	rotate()
	appendTo(path, "2026-10-26T10:51:48Z FATAL [last] after the second rotation")

	text, err := readServerLogSinceMark(path, mark)
	require.NoError(t, err)
	require.NotContains(t, text, "previous server still running")
	first := strings.Index(text, "[first]")
	between := strings.Index(text, "[between]")
	last := strings.Index(text, "[last]")
	require.True(t, first >= 0 && first < between && between < last, text)
}

func TestServerLogKeepsFirstErrorAmongManyErrors(t *testing.T) {
	path := t.TempDir() + "/arangod.log"
	var body strings.Builder
	original := "2026-10-26T10:51:46Z ERROR [abcde] {engines} failed to open '_system': shutdown in progress"
	body.WriteString(original)
	body.WriteByte('\n')
	for i := 0; i < 80; i++ {
		body.WriteString(fmt.Sprintf("2026-10-26T10:51:47Z ERROR [follow] secondary error %d\n", i))
	}
	body.WriteString("2026-10-26T10:51:48Z INFO [tail] actual-log-tail-marker\n")
	require.NoError(t, os.WriteFile(path, []byte(body.String()), 0600))

	text, err := readServerLogSince(path, 0)
	require.NoError(t, err)
	require.Contains(t, text, original)
	require.Contains(t, text, "secondary error 79")
	require.Contains(t, text, "actual-log-tail-marker")

	reason := autoUpgradeExitError(definitions.ServerTypeDBServer, 1, text).Error()
	require.Contains(t, reason, original)
	require.Contains(t, reason, "actual-log-tail-marker")
}

func TestFailureReasonReservesLogTail(t *testing.T) {
	var log strings.Builder
	// An oversized first error must not push out the final FATAL line.
	original := "2026-10-26T10:51:46Z ERROR [abcde] {engines} failed to open '_system': " + strings.Repeat("o", 10000)
	log.WriteString(original)
	log.WriteByte('\n')
	fatal := "2026-10-26T10:51:48Z FATAL [67bdc] {startup} assertion failed"
	log.WriteString(fatal)
	log.WriteByte('\n')
	// 20 distinct tail lines of about 300 bytes exceed the budget together.
	for i := 0; i < 20; i++ {
		log.WriteString(fmt.Sprintf("INFO [tail-%02d] %s\n", i, strings.Repeat("t", 290)))
	}
	end := "INFO [end] actual-log-tail-marker"
	log.WriteString(end)

	reason := autoUpgradeExitError(definitions.ServerTypeDBServer, 1, log.String()).Error()
	require.LessOrEqual(t, len(reason), autoUpgradeReasonMaxLen)
	require.Contains(t, reason, "failed to open '_system'")
	require.Contains(t, reason, fatal)
	require.Contains(t, reason, end)
	require.Contains(t, reason, "[tail-19]")
	// The selected lines keep the order of the log.
	require.Less(t, strings.Index(reason, fatal), strings.Index(reason, end))
}

type fakeHTTPContext struct {
	httpServerContext
	isMaster  bool
	masterURL string
	mode      ServiceMode
	config    ClusterConfig
	me        Peer
	upgrade   UpgradeManager
}

func (c *fakeHTTPContext) IsRunningMaster() (bool, bool, string) {
	return c.isMaster, true, c.masterURL
}

func (c *fakeHTTPContext) ClusterConfig() (ClusterConfig, *Peer, ServiceMode) {
	return c.config, &c.me, c.mode
}

func (c *fakeHTTPContext) UpgradeManager() UpgradeManager { return c.upgrade }

type fakeHTTPUpgrade struct {
	UpgradeManager
	retryErr     error
	statusErr    error
	localFailure bool
	// localRetryErr is the refusal of a local retry, e.g. an unhealthy cluster.
	localRetryErr error
	retries       atomic.Int32
	localRetries  atomic.Int32
}

func (u *fakeHTTPUpgrade) RetryDatabaseUpgrade(context.Context) error {
	u.retries.Add(1)
	return u.retryErr
}

func (u *fakeHTTPUpgrade) Status(context.Context) (client.UpgradeStatus, error) {
	return client.UpgradeStatus{}, u.statusErr
}

func (u *fakeHTTPUpgrade) HasLocalUpgradeFailure() bool { return u.localFailure }

func (u *fakeHTTPUpgrade) RetryLocalUpgradeFailure(context.Context) error {
	if !u.localFailure {
		return client.NewNotFoundError("none")
	}
	u.localRetries.Add(1)
	return u.localRetryErr
}

func (u *fakeHTTPUpgrade) LocalUpgradeFailureStatus() (client.UpgradeStatus, bool) {
	if !u.localFailure {
		return client.UpgradeStatus{}, false
	}
	return client.UpgradeStatus{Failed: true, Reason: "agent auto-upgrade exited with code 1"}, true
}

func serveUpgrade(ctx *fakeHTTPContext) *httpServer {
	return &httpServer{log: zerolog.Nop(), context: ctx}
}

func doUpgradeRequest(s *httpServer, method string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	s.databaseAutoUpgradeHandler(w, httptest.NewRequest(method, "/database-auto-upgrade", nil))
	return w
}

func TestStandaloneRetryIsHandledLocally(t *testing.T) {
	um := &fakeHTTPUpgrade{}
	// Standalone starters never become the running master.
	s := serveUpgrade(&fakeHTTPContext{mode: ServiceModeSingle, me: Peer{ID: "single"}, upgrade: um})
	w := doUpgradeRequest(s, http.MethodPut)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, int32(1), um.retries.Load())
}

func TestRetryRoutesToStarterHoldingLocalFailure(t *testing.T) {
	owner := &fakeHTTPUpgrade{localFailure: true}
	ownerCtx := &fakeHTTPContext{mode: ServiceModeCluster, upgrade: owner}
	ts := httptest.NewServer(http.HandlerFunc(serveUpgrade(ownerCtx).databaseAutoUpgradeHandler))
	defer ts.Close()
	ownerPeer := peerFor(t, "owner", ts.URL)
	ownerCtx.me = ownerPeer

	leaderPeer := Peer{ID: "leader"}
	config := ClusterConfig{AllPeers: []Peer{leaderPeer, ownerPeer}}
	ownerCtx.config = config
	agencyDown := fmt.Errorf("Failed to read upgrade plan: agency is stopped")
	leader := &fakeHTTPUpgrade{retryErr: agencyDown, statusErr: agencyDown}
	s := serveUpgrade(&fakeHTTPContext{isMaster: true, mode: ServiceModeCluster, config: config, me: leaderPeer, upgrade: leader})

	// The leader cannot read the agency and holds no failure, so it asks
	// the starter that owns it.
	w := doUpgradeRequest(s, http.MethodPut)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, int32(1), owner.localRetries.Load())

	w = doUpgradeRequest(s, http.MethodGet)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var status client.UpgradeStatus
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &status))
	require.True(t, status.Failed)
	require.Contains(t, status.Reason, "exited with code 1")

	// A non-leader holding the failure retries it itself; there is no
	// reachable leader to forward to.
	direct := serveUpgrade(ownerCtx)
	w = doUpgradeRequest(direct, http.MethodPut)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, int32(1), owner.retries.Load())

	// Without any starter holding a failure, the agency error is reported.
	owner.localFailure = false
	w = doUpgradeRequest(s, http.MethodPut)
	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.Contains(t, w.Body.String(), "agency is stopped")
}

func peerFor(t *testing.T, id, serverURL string) Peer {
	t.Helper()
	u, err := url.Parse(serverURL)
	require.NoError(t, err)
	port, err := strconv.Atoi(u.Port())
	require.NoError(t, err)
	return Peer{ID: id, Address: u.Hostname(), Port: port}
}

func TestPeersAreAskedInParallel(t *testing.T) {
	release := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer slow.Close()
	defer close(release)

	owner := &fakeHTTPUpgrade{localFailure: true}
	ownerCtx := &fakeHTTPContext{mode: ServiceModeCluster, upgrade: owner}
	fast := httptest.NewServer(http.HandlerFunc(serveUpgrade(ownerCtx).databaseAutoUpgradeHandler))
	defer fast.Close()

	leaderPeer := Peer{ID: "leader"}
	// The hanging starter comes first; the owner must still answer quickly.
	config := ClusterConfig{AllPeers: []Peer{leaderPeer, peerFor(t, "slow", slow.URL), peerFor(t, "owner", fast.URL)}}
	ownerCtx.config, ownerCtx.me = config, config.AllPeers[2]
	agencyDown := fmt.Errorf("Failed to read upgrade plan: agency is stopped")
	leader := &fakeHTTPUpgrade{retryErr: agencyDown}
	s := serveUpgrade(&fakeHTTPContext{isMaster: true, mode: ServiceModeCluster, config: config, me: leaderPeer, upgrade: leader})

	start := time.Now()
	w := doUpgradeRequest(s, http.MethodPut)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, int32(1), owner.localRetries.Load())
	require.Less(t, time.Since(start), localUpgradePeerTimeout)
}

// blockedAgencyUpgrade models an unavailable agency whose connection retries
// until the caller gives up, rather than returning an immediate error.
type blockedAgencyUpgrade struct {
	fakeHTTPUpgrade
	agencyCalls atomic.Int32
}

func (u *blockedAgencyUpgrade) RetryDatabaseUpgrade(ctx context.Context) error {
	u.agencyCalls.Add(1)
	<-ctx.Done()
	return ctx.Err()
}

func (u *blockedAgencyUpgrade) Status(ctx context.Context) (client.UpgradeStatus, error) {
	u.agencyCalls.Add(1)
	<-ctx.Done()
	return client.UpgradeStatus{}, ctx.Err()
}

func TestCachedUpgradeFailureBypassesUnavailableAgency(t *testing.T) {
	for _, method := range []string{http.MethodPut, http.MethodGet} {
		for _, isMaster := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/master=%t", method, isMaster), func(t *testing.T) {
				owner := &fakeHTTPUpgrade{localFailure: true}
				ownerCtx := &fakeHTTPContext{mode: ServiceModeCluster, upgrade: owner}
				ts := httptest.NewServer(http.HandlerFunc(serveUpgrade(ownerCtx).databaseAutoUpgradeHandler))
				defer ts.Close()
				ownerPeer := peerFor(t, "owner", ts.URL)
				requestPeer := Peer{ID: "request-peer"}
				config := ClusterConfig{AllPeers: []Peer{requestPeer, ownerPeer}, AgencySize: 1}
				ownerCtx.config, ownerCtx.me = config, ownerPeer
				um := &blockedAgencyUpgrade{}
				s := serveUpgrade(&fakeHTTPContext{isMaster: isMaster, mode: ServiceModeCluster, config: config, me: requestPeer, upgrade: um})
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				w := httptest.NewRecorder()
				s.databaseAutoUpgradeHandler(w, httptest.NewRequest(method, "/database-auto-upgrade", nil).WithContext(ctx))
				require.Equal(t, http.StatusOK, w.Code, w.Body.String())
				require.NoError(t, ctx.Err())
				require.Zero(t, um.agencyCalls.Load())
				if method == http.MethodPut {
					require.Equal(t, int32(1), owner.localRetries.Load())
				} else {
					var status client.UpgradeStatus
					require.NoError(t, json.Unmarshal(w.Body.Bytes(), &status))
					require.True(t, status.Failed)
					require.Contains(t, status.Reason, "exited with code 1")
				}
			})
		}
	}
}

func TestLocalUpgradeFailureStatusBypassesUnavailableAgency(t *testing.T) {
	um := &blockedAgencyUpgrade{fakeHTTPUpgrade: fakeHTTPUpgrade{localFailure: true}}
	s := serveUpgrade(&fakeHTTPContext{mode: ServiceModeCluster, me: Peer{ID: "owner"}, upgrade: um})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	w := httptest.NewRecorder()
	s.databaseAutoUpgradeHandler(w, httptest.NewRequest(http.MethodGet, "/database-auto-upgrade", nil).WithContext(ctx))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, ctx.Err())
	require.Zero(t, um.agencyCalls.Load())
	var status client.UpgradeStatus
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &status))
	require.True(t, status.Failed)
}

func TestCachedDBServerFailureIsRetriedOnlyWhenOthersAreHealthy(t *testing.T) {
	ctx := context.Background()
	peer := Peer{ID: "peer1", Address: "10.0.0.2", Port: 8528, peerServers: peerServers{HasAgentFlag: true}}
	// The only coordinator (port 8529) is not reachable, so the other
	// servers cannot be shown to be healthy.
	c := &coordinatorHealthTestContext{config: ClusterConfig{AllPeers: []Peer{peer}, AgencySize: 3}}
	m := &upgradeManager{log: zerolog.Nop(), upgradeManagerContext: c}
	cached := UpgradePlan{Revision: 2, Entries: []UpgradePlanEntry{{Type: UpgradeEntryTypeDBServer, PeerID: "peer1", Failures: 1, Reason: "failed"}}}
	m.storeLocalFailedPlan(cached)

	err := m.RetryLocalUpgradeFailure(ctx)
	require.True(t, client.IsPreconditionFailed(err), "%v", err)
	require.ErrorContains(t, err, "unhealthy cluster")
	// The failure stays cached for a later retry.
	require.True(t, m.HasLocalUpgradeFailure())
	require.False(t, m.localUpgradePending() && m.localRetryActive)
}

func TestPeerRefusalOfLocalRetryIsReported(t *testing.T) {
	owner := &fakeHTTPUpgrade{localFailure: true, localRetryErr: client.NewPreconditionFailedError("Found unhealthy cluster: dbserver is down")}
	ownerCtx := &fakeHTTPContext{mode: ServiceModeCluster, upgrade: owner}
	ts := httptest.NewServer(http.HandlerFunc(serveUpgrade(ownerCtx).databaseAutoUpgradeHandler))
	defer ts.Close()
	ownerPeer := peerFor(t, "owner", ts.URL)
	leaderPeer := Peer{ID: "leader"}
	config := ClusterConfig{AllPeers: []Peer{leaderPeer, ownerPeer}}
	ownerCtx.config, ownerCtx.me = config, ownerPeer
	leader := &blockedAgencyUpgrade{}
	s := serveUpgrade(&fakeHTTPContext{isMaster: true, mode: ServiceModeCluster, config: config, me: leaderPeer, upgrade: leader})

	w := doUpgradeRequest(s, http.MethodPut)
	require.Equal(t, http.StatusPreconditionFailed, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "dbserver is down")
	require.Zero(t, leader.agencyCalls.Load())
}

func TestReconcileReleasesMutexWhenAgencyHangs(t *testing.T) {
	shortenAgencyTimeout(t)
	agency := &planAgency{hang: true}
	c := &unavailableAgencyContext{peer: Peer{ID: "peer1", Address: "10.0.0.2", Port: 8528, peerServers: peerServers{HasAgentFlag: true}}}
	c.agency = agency.start(t)
	m := &upgradeManager{log: zerolog.Nop(), upgradeManagerContext: c}
	agencyPlan := UpgradePlan{Revision: 3, Entries: []UpgradePlanEntry{{Type: UpgradeEntryTypeAgent, PeerID: "peer1"}}}
	local := agencyPlan.clone()
	local.Entries[0].Failures = 1
	m.storeLocalFailedPlan(local)

	done := make(chan bool, 1)
	go func() { done <- m.reconcileLocalFailedPlan(context.Background(), agencyPlan) }()
	select {
	case skipped := <-done:
		require.True(t, skipped)
	case <-time.After(5 * time.Second):
		t.Fatal("reconcile kept waiting for the agency")
	}
	require.True(t, m.mutex.TryLock())
	m.mutex.Unlock()
	require.True(t, m.HasLocalUpgradeFailure())
}

func TestFailureIsPublishedWhenAgencyWriteHangs(t *testing.T) {
	shortenAgencyTimeout(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	agency := &planAgency{hang: true}
	p := &processWrapper{ctx: ctx, stopping: make(chan struct{}), serverType: definitions.ServerTypeAgent}
	c := &unavailableAgencyContext{
		peer:     Peer{ID: "peer1", Address: "10.0.0.2", Port: 8528, peerServers: peerServers{HasAgentFlag: true}},
		wrapper:  p,
		restarts: make(chan definitions.ServerType, 2),
	}
	c.agency = agency.start(t)
	m := &upgradeManager{log: zerolog.Nop(), upgradeManagerContext: c}
	plan := UpgradePlan{Entries: []UpgradePlanEntry{{Type: UpgradeEntryTypeAgent, PeerID: "peer1"}}}

	done := make(chan error, 1)
	go func() { done <- m.processUpgradePlan(ctx, plan) }()
	<-c.restarts
	m.ServerDatabaseAutoUpgradeStarter(definitions.ServerTypeAgent)
	require.True(t, m.ServerDatabaseAutoUpgradeFinished(definitions.ServerTypeAgent, 1, "FATAL agent failed"))
	select {
	case err := <-done:
		require.ErrorContains(t, err, "exited with code 1")
	case <-time.After(5 * time.Second):
		t.Fatal("failure was not published while the agency write hung")
	}
	require.True(t, m.HasLocalUpgradeFailure())
}

func TestWatcherRedeliversPlanSkippedDuringRetry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	agency := &planAgency{}
	plan := UpgradePlan{Revision: 4, CreatedAt: time.Unix(1700000000, 0).UTC(), Entries: []UpgradePlanEntry{{Type: UpgradeEntryTypeDBServer, PeerID: "peer1"}}}
	agency.setPlan(&plan)
	c := &unavailableAgencyContext{peer: Peer{ID: "peer1", Address: "10.0.0.2", Port: 8528, peerServers: peerServers{HasAgentFlag: true}}}
	c.agency = agency.start(t)
	m := &upgradeManager{log: zerolog.Nop(), upgradeManagerContext: c}

	// A local retry is running; it published the plan's next revision.
	m.localRetryActive = true
	ch := m.waitForPlanChanges(ctx)
	next := func() *UpgradePlan {
		t.Helper()
		select {
		case p := <-ch:
			return p
		case <-time.After(5 * time.Second):
			t.Fatal("plan was not delivered")
			return nil
		}
	}
	// While the retry runs, the watcher skips every delivery.
	require.Equal(t, 4, next().Revision)
	require.True(t, m.reconcileLocalFailedPlan(ctx, plan))
	require.Equal(t, 4, next().Revision)
	require.True(t, m.reconcileLocalFailedPlan(ctx, plan))

	// Once the retry ends, the unchanged revision must be delivered again,
	// because the skipped deliveries were not acknowledged.
	m.stateMu.Lock()
	m.localRetryActive = false
	m.stateMu.Unlock()
	delivered := next()
	require.Equal(t, 4, delivered.Revision)
	require.False(t, m.reconcileLocalFailedPlan(ctx, *delivered))
}

func TestWatcherDropsFailureOfRemovedPlan(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	agency := &planAgency{} // no plan: it was aborted
	c := &unavailableAgencyContext{peer: Peer{ID: "peer1", Address: "10.0.0.2", Port: 8528}}
	c.agency = agency.start(t)
	m := &upgradeManager{log: zerolog.Nop(), upgradeManagerContext: c}
	cached := UpgradePlan{Revision: 3, CreatedAt: time.Unix(1700000000, 0).UTC(), Entries: []UpgradePlanEntry{{Type: UpgradeEntryTypeAgent, PeerID: "peer1", Failures: 1}}}
	m.storeLocalFailedPlan(cached)

	m.waitForPlanChanges(ctx)
	require.Eventually(t, func() bool { return !m.HasLocalUpgradeFailure() }, 3*time.Second, 20*time.Millisecond)
	_, found := m.LocalUpgradeFailureStatus()
	require.False(t, found)
}

func TestStandaloneCompletionIsClearedThroughHTTP(t *testing.T) {
	c := &standaloneUpgradeTestContext{peer: Peer{ID: "single"}}
	m := &upgradeManager{log: zerolog.Nop(), upgradeManagerContext: c, singleUpgradeDone: true}
	// Standalone starters have no elected leader. The CLI acknowledges
	// completion with DELETE after polling GET and seeing Ready.
	s := serveUpgrade(&fakeHTTPContext{mode: ServiceModeSingle, me: c.peer, upgrade: m})
	w := doUpgradeRequest(s, http.MethodGet)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var status client.UpgradeStatus
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &status))
	require.True(t, status.Ready)

	w = doUpgradeRequest(s, http.MethodDelete)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w = doUpgradeRequest(s, http.MethodGet)
	require.Equal(t, http.StatusPreconditionFailed, w.Code, w.Body.String())
	// There is no longer a completed upgrade to acknowledge.
	w = doUpgradeRequest(s, http.MethodDelete)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

func TestDropKeepsNewerLocalFailure(t *testing.T) {
	m := &upgradeManager{}
	old := UpgradePlan{Revision: 3, CreatedAt: time.Unix(1700000000, 0).UTC(), Entries: []UpgradePlanEntry{{Type: UpgradeEntryTypeAgent, Failures: 1}}}
	newer := UpgradePlan{Revision: 1, CreatedAt: time.Unix(1700000500, 0).UTC(), Entries: []UpgradePlanEntry{{Type: UpgradeEntryTypeAgent, Failures: 1}}}
	m.storeLocalFailedPlan(newer)
	// A drop based on a snapshot of the old failure must not remove it.
	m.dropLocalFailedPlan(old)
	require.True(t, m.HasLocalUpgradeFailure())
	m.dropLocalFailedPlan(newer)
	require.False(t, m.HasLocalUpgradeFailure())
}

func TestAbandonedAutoUpgradeExitIsStillRecorded(t *testing.T) {
	m := &upgradeManager{}
	m.beginServerUpgrade(definitions.ServerTypeSingle)
	m.ServerDatabaseAutoUpgradeStarter(definitions.ServerTypeSingle)

	// The wait gives up on its deadline while the process still runs.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	require.ErrorContains(t, m.waitUntilUpgradeServerStarted(ctx), autoUpgradeTimeoutReason)
	m.endServerUpgrade()

	// Its later non-zero exit is still an auto-upgrade exit, so the wrapper
	// parks the server instead of treating it as an ordinary crash.
	require.True(t, m.IsServerAutoUpgradeRunning(definitions.ServerTypeSingle))
	p := &processWrapper{serverType: definitions.ServerTypeSingle}
	failed, _ := p.recordAutoUpgradeExit(m, zerolog.Nop(), 1, func() string { return "FATAL late failure" }, nil)
	require.True(t, failed)
	// Its identity is released once it has exited.
	require.False(t, m.IsServerUpgradeInProgress(definitions.ServerTypeSingle))
	require.False(t, m.IsServerAutoUpgradeRunning(definitions.ServerTypeSingle))

	// A new attempt is unaffected.
	m.beginServerUpgrade(definitions.ServerTypeSingle)
	require.True(t, m.ServerDatabaseAutoUpgrade(definitions.ServerTypeSingle, 0))
}

func TestWatcherSkipsOutdatedPlanRevision(t *testing.T) {
	ctx := context.Background()
	created := time.Unix(1700000000, 0).UTC()
	agency := &planAgency{}
	// A local retry already advanced the plan past the delivered revision.
	agency.setPlan(&UpgradePlan{Revision: 5, CreatedAt: created, Entries: []UpgradePlanEntry{{Type: UpgradeEntryTypeCoordinator, PeerID: "peer1"}}})
	c := &unavailableAgencyContext{
		peer:     Peer{ID: "peer1", Address: "10.0.0.2", Port: 8528, peerServers: peerServers{HasAgentFlag: true}},
		restarts: make(chan definitions.ServerType, 1),
	}
	c.agency = agency.start(t)
	m := &upgradeManager{log: zerolog.Nop(), upgradeManagerContext: c}
	stale := UpgradePlan{Revision: 4, CreatedAt: created, Entries: []UpgradePlanEntry{{Type: UpgradeEntryTypeDBServer, PeerID: "peer1"}}}

	require.NoError(t, m.processWatchedUpgradePlan(ctx, stale))
	require.Empty(t, c.restarts, "an outdated entry restarted a server")
	require.False(t, m.IsServerUpgradeInProgress(definitions.ServerTypeDBServer))

	// The same check rejects a plan that was removed.
	agency.setPlan(nil)
	require.NoError(t, m.processWatchedUpgradePlan(ctx, stale))
	require.Empty(t, c.restarts)
}

func TestAutoUpgradeFailureFallsBackToProcessOutput(t *testing.T) {
	m := &upgradeManager{}
	m.beginServerUpgrade(definitions.ServerTypeDBServer)
	m.ServerDatabaseAutoUpgradeStarter(definitions.ServerTypeDBServer)
	output := utils.NewLimitedBuffer(1000)
	_, err := output.Write([]byte("cannot open log file '/data/arangod.log': permission denied\n"))
	require.NoError(t, err)

	p := &processWrapper{serverType: definitions.ServerTypeDBServer}
	failed, _ := p.recordAutoUpgradeExit(m, zerolog.Nop(), 1, func() string { return "" }, output)
	require.True(t, failed)
	err = m.waitUntilUpgradeServerStarted(context.Background())
	require.ErrorContains(t, err, "exited with code 1")
	require.ErrorContains(t, err, "permission denied")
}

func TestServerLogKeepsLogOrder(t *testing.T) {
	path := t.TempDir() + "/arangod.log"
	body := "2026-10-26T10:51:46Z ERROR [cause] failed to open '_system'\n" +
		"2026-10-26T10:51:47Z INFO [stop] shutting down\n" +
		"2026-10-26T10:51:48Z FATAL [crash] assertion failed\n"
	require.NoError(t, os.WriteFile(path, []byte(body), 0600))

	text, err := readServerLogSince(path, 0)
	require.NoError(t, err)
	require.Equal(t, strings.TrimSuffix(body, "\n"), text)

	reason := autoUpgradeExitError(definitions.ServerTypeDBServer, 1, text).Error()
	require.True(t, strings.HasSuffix(reason, "FATAL [crash] assertion failed"), reason)
}

func TestUnrelatedPeerErrorAllowsNormalRetry(t *testing.T) {
	// A starter that is still bootstrapping answers 400 before it looks at
	// the local flag. That answer must not be taken as the owner's refusal.
	bootstrapping := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusBadRequest, "Must be in running state to do upgrades")
	}))
	defer bootstrapping.Close()
	// An older starter ignores the local flag and answers its normal status.
	older := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(contentTypeHeader, contentTypeJSON)
		_, _ = w.Write([]byte(`{"ready":false,"failed":false}`))
	}))
	defer older.Close()

	leaderPeer := Peer{ID: "leader"}
	config := ClusterConfig{AllPeers: []Peer{leaderPeer, peerFor(t, "booting", bootstrapping.URL), peerFor(t, "older", older.URL)}}
	leader := &fakeHTTPUpgrade{}
	s := serveUpgrade(&fakeHTTPContext{isMaster: true, mode: ServiceModeCluster, config: config, me: leaderPeer, upgrade: leader})

	w := doUpgradeRequest(s, http.MethodPut)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, int32(1), leader.retries.Load(), "the normal agency retry did not run")

	_, found := s.peerLocalUpgradeFailureStatus(context.Background())
	require.False(t, found)
}

func TestCleanupFailureKeepsFirstReason(t *testing.T) {
	entry := UpgradePlanEntry{Type: UpgradeEntryTypeDBServer}
	first := fmt.Errorf("dbserver auto-upgrade exited with code 1 (exit with error)\nFATAL [67bdc] assertion failed")
	recordEntryFailure(&entry, first, false)
	recordEntryFailure(&entry, fmt.Errorf("Failed to enable supervision: agency timeout"), true)

	require.Equal(t, 1, entry.Failures)
	require.True(t, strings.HasPrefix(entry.Reason, first.Error()), entry.Reason)
	require.Contains(t, entry.Reason, "Failed to enable supervision")
}

// stopAtRestartContext records execution without starting an actual server.
type stopAtRestartContext struct {
	unavailableAgencyContext
}

func (c *stopAtRestartContext) RestartServer(serverType definitions.ServerType) error {
	c.restarts <- serverType
	return fmt.Errorf("test stopped at restart")
}

func TestWatcherRetriesValidationAfterAgencyRecovery(t *testing.T) {
	shortenAgencyTimeout(t)
	for _, advanced := range []bool{false, true} {
		t.Run(fmt.Sprintf("advanced=%t", advanced), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			plan := UpgradePlan{Revision: 4, CreatedAt: time.Unix(1700000000, 0).UTC(), Entries: []UpgradePlanEntry{{Type: UpgradeEntryTypeAgent, PeerID: "peer1"}}}
			a := &planAgency{plan: &plan}
			c := &stopAtRestartContext{unavailableAgencyContext: unavailableAgencyContext{
				peer:     Peer{ID: "peer1", Address: "10.0.0.2", Port: 8528, peerServers: peerServers{HasAgentFlag: true}},
				restarts: make(chan definitions.ServerType, 2),
				agency:   a.start(t),
			}}
			m := &upgradeManager{log: zerolog.Nop(), upgradeManagerContext: c}
			changes := m.waitForPlanChanges(ctx)
			next := func() UpgradePlan {
				t.Helper()
				select {
				case p := <-changes:
					require.NotNil(t, p)
					return *p
				case <-time.After(5 * time.Second):
					t.Fatal("plan was not redelivered")
					return UpgradePlan{}
				}
			}
			delivered := next()
			// The agency goes down between delivery and validation.
			a.mu.Lock()
			a.hang = true
			a.mu.Unlock()
			require.ErrorContains(t, m.processWatchedUpgradePlan(ctx, delivered), "Failed to validate upgrade plan")
			require.Empty(t, c.restarts)
			require.True(t, m.processMu.TryLock(), "validation must not block explicit retry")
			m.processMu.Unlock()
			require.False(t, m.IsServerUpgradeInProgress(definitions.ServerTypeAgent))

			recovered := plan.clone()
			if advanced {
				recovered.Revision++
				recovered.Entries[0].Type = UpgradeEntryTypeCoordinator
			}
			a.mu.Lock()
			a.plan, a.hang = &recovered, false
			a.mu.Unlock()
			fresh := next()
			require.Equal(t, recovered.Revision, fresh.Revision)
			if advanced {
				// Even a delayed old delivery cannot execute after recovery.
				require.NoError(t, m.processWatchedUpgradePlan(ctx, delivered))
				require.Empty(t, c.restarts)
			}
			require.ErrorContains(t, m.processWatchedUpgradePlan(ctx, fresh), "test stopped at restart")
			require.Len(t, c.restarts, 1)
			if advanced {
				require.Equal(t, definitions.ServerType(definitions.ServerTypeCoordinator), <-c.restarts)
			} else {
				require.Equal(t, definitions.ServerType(definitions.ServerTypeAgent), <-c.restarts)
			}
		})
	}
}

func TestLateStandaloneFailureUpdatesStatus(t *testing.T) {
	for _, beforeCleanup := range []bool{false, true} {
		t.Run(fmt.Sprintf("before-cleanup=%t", beforeCleanup), func(t *testing.T) {
			c := &standaloneUpgradeTestContext{peer: Peer{ID: "single"}}
			m := &upgradeManager{log: zerolog.Nop(), upgradeManagerContext: c, singleUpgradeFailure: fmt.Errorf(autoUpgradeTimeoutReason)}
			m.beginServerUpgrade(definitions.ServerTypeSingle)
			m.ServerDatabaseAutoUpgradeStarter(definitions.ServerTypeSingle)
			if !beforeCleanup {
				m.endServerUpgrade()
			}
			p := &processWrapper{serverType: definitions.ServerTypeSingle}
			failed, _ := p.recordAutoUpgradeExit(m, zerolog.Nop(), 1, func() string { return "FATAL late database failure" }, nil)
			require.True(t, failed)
			if beforeCleanup {
				m.endServerUpgrade()
			}
			require.Eventually(t, func() bool {
				status, err := m.Status(context.Background())
				return err == nil && status.Failed && strings.Contains(status.Reason, "exited with code 1") && strings.Contains(status.Reason, "late database failure")
			}, time.Second, time.Millisecond)
		})
	}
}

func TestLateClusterFailureUpdatesCachedAndAgencyReason(t *testing.T) {
	writes := make(chan string, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_api/agency/write" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		encoded, _ := json.Marshal(body)
		writes <- string(encoded)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[1]}`))
	}))
	defer ts.Close()
	c := &unavailableAgencyContext{peer: Peer{ID: "peer1", Address: "localhost", Port: 8528}, agency: ts}
	m := &upgradeManager{log: zerolog.Nop(), upgradeManagerContext: c}
	plan := UpgradePlan{Revision: 4, CreatedAt: time.Unix(1700000000, 0).UTC(), Entries: []UpgradePlanEntry{{Type: UpgradeEntryTypeAgent, PeerID: "peer1", Failures: 1, Reason: autoUpgradeTimeoutReason}}}
	m.beginServerUpgrade(definitions.ServerTypeAgent)
	m.ServerDatabaseAutoUpgradeStarter(definitions.ServerTypeAgent)
	m.endServerUpgradePlan(&plan)
	m.storeLocalFailedPlan(plan)
	p := &processWrapper{serverType: definitions.ServerTypeAgent}
	failed, _ := p.recordAutoUpgradeExit(m, zerolog.Nop(), 1, func() string { return "FATAL late agent failure" }, nil)
	require.True(t, failed)
	require.Eventually(t, func() bool {
		status, ok := m.LocalUpgradeFailureStatus()
		return ok && strings.Contains(status.Reason, "late agent failure") && strings.Contains(status.Reason, "exited with code 1")
	}, time.Second, time.Millisecond)
	// An agency plan that already records the timeout must be updated too.
	require.True(t, m.reconcileLocalFailedPlan(context.Background(), plan))
	select {
	case body := <-writes:
		require.Contains(t, body, "late agent failure")
		require.Contains(t, body, "exited with code 1")
	case <-time.After(time.Second):
		t.Fatal("late exit was not written to the agency")
	}
	require.False(t, m.HasLocalUpgradeFailure())
}

func TestLateFailureCannotOverwriteNewAttempt(t *testing.T) {
	m := &upgradeManager{singleUpgradeFailure: fmt.Errorf("new attempt failure")}
	m.beginServerUpgrade(definitions.ServerTypeSingle)
	oldGeneration := m.autoUpgradeGeneration
	m.beginServerUpgrade(definitions.ServerTypeSingle)
	m.publishLateUpgradeFailure(oldGeneration, nil, fmt.Errorf("old late exit"))
	require.EqualError(t, m.singleUpgradeFailure, "new attempt failure")
}

func TestLateFailureCannotRestoreRemovedPlan(t *testing.T) {
	m := &upgradeManager{}
	m.beginServerUpgrade(definitions.ServerTypeAgent)
	m.ServerDatabaseAutoUpgradeStarter(definitions.ServerTypeAgent)
	plan := UpgradePlan{Revision: 4, Entries: []UpgradePlanEntry{{Type: UpgradeEntryTypeAgent, Failures: 1}}}
	m.endServerUpgradePlan(&plan)
	generation := m.autoUpgradeGeneration
	m.discardAbandonedPlan(nil)
	m.publishLateUpgradeFailure(generation, &plan, fmt.Errorf("late failure"))
	require.False(t, m.HasLocalUpgradeFailure())
}

func TestAcceptedRetryInvalidatesQueuedLateFailure(t *testing.T) {
	m := &upgradeManager{}
	m.beginServerUpgrade(definitions.ServerTypeAgent)
	generation := m.autoUpgradeGeneration
	plan := UpgradePlan{Revision: 4, Entries: []UpgradePlanEntry{{Type: UpgradeEntryTypeAgent, Failures: 1}}}
	m.storeLocalFailedPlan(plan)
	_, ok := m.takeLocalFailedPlan()
	require.True(t, ok)
	m.publishLateUpgradeFailure(generation, &plan, fmt.Errorf("previous attempt exited"))
	require.False(t, m.HasLocalUpgradeFailure())
}

func TestLateFailureWakesPlanWatcher(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	plan := UpgradePlan{Revision: 4, CreatedAt: time.Unix(1700000000, 0).UTC(), Entries: []UpgradePlanEntry{{Type: UpgradeEntryTypeAgent, PeerID: "peer1", Failures: 1, Reason: autoUpgradeTimeoutReason}}}
	a := &planAgency{plan: &plan}
	c := &unavailableAgencyContext{peer: Peer{ID: "peer1", Address: "10.0.0.2", Port: 8528}}
	c.agency = a.start(t)
	m := &upgradeManager{log: zerolog.Nop(), upgradeManagerContext: c}
	m.beginServerUpgrade(definitions.ServerTypeAgent)
	m.ServerDatabaseAutoUpgradeStarter(definitions.ServerTypeAgent)
	m.endServerUpgradePlan(&plan)

	changes := m.waitForPlanChanges(ctx)
	select {
	case <-changes:
	case <-time.After(5 * time.Second):
		t.Fatal("plan was not delivered")
	}
	// Let the watcher settle into its idle poll interval.
	time.Sleep(1500 * time.Millisecond)

	p := &processWrapper{serverType: definitions.ServerTypeAgent}
	failed, _ := p.recordAutoUpgradeExit(m, zerolog.Nop(), 1, func() string { return "FATAL late agent failure" }, nil)
	require.True(t, failed)
	// The stored failure is delivered for reconciliation at once, not
	// after the remaining poll interval.
	select {
	case delivered := <-changes:
		require.NotNil(t, delivered)
		require.True(t, m.HasLocalUpgradeFailure())
	case <-time.After(time.Second):
		t.Fatal("late failure did not wake the plan watcher")
	}
}

func TestRetryIsNotSentToPeersWithoutTheFailure(t *testing.T) {
	// An older starter ignores local=true; a PUT would be an ordinary retry
	// that it forwards to the leader or applies to the agency plan.
	var olderPuts atomic.Int32
	older := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			olderPuts.Add(1)
		}
		w.Header().Set(contentTypeHeader, contentTypeJSON)
		_, _ = w.Write([]byte(`{"ready":false,"failed":true}`))
	}))
	defer older.Close()
	owner := &fakeHTTPUpgrade{localFailure: true}
	ownerCtx := &fakeHTTPContext{mode: ServiceModeCluster, upgrade: owner}
	ts := httptest.NewServer(http.HandlerFunc(serveUpgrade(ownerCtx).databaseAutoUpgradeHandler))
	defer ts.Close()

	leaderPeer := Peer{ID: "leader"}
	config := ClusterConfig{AllPeers: []Peer{leaderPeer, peerFor(t, "older", older.URL), peerFor(t, "owner", ts.URL)}}
	ownerCtx.config, ownerCtx.me = config, config.AllPeers[2]
	leader := &fakeHTTPUpgrade{}
	s := serveUpgrade(&fakeHTTPContext{isMaster: true, mode: ServiceModeCluster, config: config, me: leaderPeer, upgrade: leader})

	// Only the starter that reports holding the failure is asked to retry.
	w := doUpgradeRequest(s, http.MethodPut)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, int32(1), owner.localRetries.Load())
	require.Zero(t, leader.retries.Load())
	require.Zero(t, olderPuts.Load())

	// Without a holder, the normal retry runs and no peer gets a PUT.
	owner.localFailure = false
	w = doUpgradeRequest(s, http.MethodPut)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, int32(1), leader.retries.Load())
	require.Zero(t, olderPuts.Load())
}

// blockingRestartContext holds a restart until the test releases it.
type blockingRestartContext struct {
	unavailableAgencyContext
	config  ClusterConfig
	release chan struct{}
}

func (c *blockingRestartContext) ClusterConfig() (ClusterConfig, *Peer, ServiceMode) {
	return c.config, &c.peer, ServiceModeCluster
}

func (c *blockingRestartContext) RestartServer(serverType definitions.ServerType) error {
	c.restarts <- serverType
	<-c.release
	return fmt.Errorf("test stopped at restart")
}

func TestLocalRetryResetsFailureHeldByAgency(t *testing.T) {
	no := false
	peer := Peer{ID: "peer1", Address: "10.0.0.2", Port: 8528, peerServers: peerServers{HasCoordinatorFlag: &no, HasDBServerFlag: &no}}
	timedOut := UpgradePlan{Revision: 4, CreatedAt: time.Unix(1700000000, 0).UTC(), Entries: []UpgradePlanEntry{{Type: UpgradeEntryTypeCoordinator, PeerID: "peer1", Failures: 1, Reason: autoUpgradeTimeoutReason}}}
	a := &planAgency{plan: &timedOut}
	c := &blockingRestartContext{
		unavailableAgencyContext: unavailableAgencyContext{peer: peer, restarts: make(chan definitions.ServerType, 1)},
		config:                   ClusterConfig{AllPeers: []Peer{peer}, AgencySize: 3},
		release:                  make(chan struct{}),
	}
	c.agency = a.start(t)
	m := &upgradeManager{log: zerolog.Nop(), upgradeManagerContext: c}
	// A late exit added its details to the timeout the agency already holds.
	late := timedOut.clone()
	late.Entries[0].Reason = "coordinator auto-upgrade exited with code 1"
	m.storeLocalFailedPlan(late)

	require.NoError(t, m.RetryLocalUpgradeFailure(context.Background()))
	// Every starter reads the agency, so it must show the retry in progress.
	current := a.current()
	require.False(t, current.IsFailed(), "the agency still reports the old failure")
	require.Equal(t, timedOut.Revision+1, current.Revision)
	status, err := m.Status(context.Background())
	require.NoError(t, err)
	require.False(t, status.Failed)

	require.Equal(t, definitions.ServerType(definitions.ServerTypeCoordinator), <-c.restarts)
	close(c.release)
	// The retry records its own result against the reset revision.
	require.Eventually(t, func() bool {
		p := a.current()
		return p.IsFailed() && strings.Contains(p.Entries[0].Reason, "test stopped at restart")
	}, 5*time.Second, 10*time.Millisecond)
}

func TestSupervisionCleanupFailureFailsSuccessfulAction(t *testing.T) {
	shortenAgencyTimeout(t)
	for _, hang := range []bool{false, true} {
		t.Run(fmt.Sprintf("hang=%t", hang), func(t *testing.T) {
			release := make(chan struct{})
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/_api/version":
					_, _ = w.Write([]byte(`{"server":"arango","version":"3.12.4"}`))
				case "/_api/agency/read":
					_, _ = w.Write([]byte(`[{"arango":{"Supervision":{"State":{"Mode":"Maintenance"}}}}]`))
				case "/_api/agency/write":
					body, _ := io.ReadAll(r.Body)
					if !strings.Contains(string(body), `"delete"`) {
						_, _ = w.Write([]byte(`{"results":[1]}`))
					} else if hang {
						select {
						case <-r.Context().Done():
						case <-release:
						}
					} else {
						w.WriteHeader(http.StatusInternalServerError)
						_, _ = w.Write([]byte(`{"error":true,"code":500,"errorMessage":"supervision write failed"}`))
					}
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer ts.Close()
			defer close(release)
			c := &unavailableAgencyContext{peer: Peer{ID: "peer1", Address: "10.0.0.2", Port: 8528, peerServers: peerServers{HasAgentFlag: true}}, agency: ts}
			m := &upgradeManager{log: zerolog.Nop(), upgradeManagerContext: c}
			var recorded []error
			recordFailure := func(err error) error {
				recorded = append(recorded, err)
				return err
			}

			done := make(chan error, 1)
			go func() {
				// The upgrade action itself succeeds.
				done <- m.withMaintenance(context.Background(), recordFailure)(func() error { return nil })
			}()
			select {
			case err := <-done:
				// The entry must not be completed while supervision may be off.
				require.ErrorContains(t, err, "Failed to enable supervision")
				require.Len(t, recorded, 1)
			case <-time.After(5 * time.Second):
				t.Fatal("enabling supervision was not bounded")
			}
		})
	}
}

func TestStaleLocalFailureIsNotRetried(t *testing.T) {
	no := false
	peer := Peer{ID: "peer1", Address: "10.0.0.2", Port: 8528, peerServers: peerServers{HasCoordinatorFlag: &no, HasDBServerFlag: &no}}
	created := time.Unix(1700000000, 0).UTC()
	cached := UpgradePlan{Revision: 4, CreatedAt: created, Entries: []UpgradePlanEntry{{Type: UpgradeEntryTypeCoordinator, PeerID: "peer1", Failures: 1, Reason: "coordinator auto-upgrade exited with code 1"}}}
	// The plan moved on to another starter's entry meanwhile.
	newerFailed := UpgradePlan{Revision: 6, CreatedAt: created, Entries: []UpgradePlanEntry{{Type: UpgradeEntryTypeDBServer, PeerID: "peer2", Failures: 1, Reason: "dbserver auto-upgrade exited with code 1"}}}
	newerRunning := UpgradePlan{Revision: 6, CreatedAt: created, Entries: []UpgradePlanEntry{{Type: UpgradeEntryTypeDBServer, PeerID: "peer2"}}}
	for _, tc := range []struct {
		name    string
		agency  *UpgradePlan
		refusal func(error) bool
	}{
		{"advanced-and-failed", &newerFailed, client.IsPreconditionFailed},
		{"advanced", &newerRunning, client.IsPreconditionFailed},
		{"removed", nil, client.IsBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &planAgency{plan: tc.agency}
			c := &blockingRestartContext{
				unavailableAgencyContext: unavailableAgencyContext{peer: peer, restarts: make(chan definitions.ServerType, 1)},
				config:                   ClusterConfig{AllPeers: []Peer{peer}, AgencySize: 3},
				release:                  make(chan struct{}),
			}
			defer close(c.release)
			c.agency = a.start(t)
			m := &upgradeManager{log: zerolog.Nop(), upgradeManagerContext: c}
			m.storeLocalFailedPlan(cached)

			err := m.RetryLocalUpgradeFailure(context.Background())
			require.True(t, tc.refusal(err), "%v", err)
			// The newer entry's failure is left for its own retry.
			if tc.agency != nil {
				require.Equal(t, *tc.agency, a.current())
			}
			// The obsolete entry is not restarted, and its failure is gone.
			require.False(t, m.localUpgradePending())
			select {
			case st := <-c.restarts:
				t.Fatalf("obsolete %s entry was restarted", st)
			case <-time.After(200 * time.Millisecond):
			}
		})
	}
}

func TestLocalRetryNeedsReadableAgency(t *testing.T) {
	shortenAgencyTimeout(t)
	no := false
	peer := Peer{ID: "peer1", Address: "10.0.0.2", Port: 8528, peerServers: peerServers{HasCoordinatorFlag: &no, HasDBServerFlag: &no}}
	cached := UpgradePlan{Revision: 4, CreatedAt: time.Unix(1700000000, 0).UTC(), Entries: []UpgradePlanEntry{{Type: UpgradeEntryTypeCoordinator, PeerID: "peer1", Failures: 1, Reason: "coordinator auto-upgrade exited with code 1"}}}
	agencyPlan := cached.clone()
	// The cluster health can be read, but the plan read fails transiently.
	a := &planAgency{plan: &agencyPlan, failPlanReads: true}
	c := &blockingRestartContext{
		unavailableAgencyContext: unavailableAgencyContext{peer: peer, restarts: make(chan definitions.ServerType, 1)},
		config:                   ClusterConfig{AllPeers: []Peer{peer}, AgencySize: 3},
		release:                  make(chan struct{}),
	}
	defer close(c.release)
	c.agency = a.start(t)
	m := &upgradeManager{log: zerolog.Nop(), upgradeManagerContext: c}
	m.storeLocalFailedPlan(cached)

	err := m.RetryLocalUpgradeFailure(context.Background())
	require.True(t, client.IsServiceUnavailable(err), "%v", err)
	// The failure stays cached for a retry once the agency answers again,
	// and nothing was restarted or written.
	require.True(t, m.HasLocalUpgradeFailure())
	require.False(t, m.localRetryActive)
	require.Equal(t, cached, a.current())
	select {
	case st := <-c.restarts:
		t.Fatalf("%s was restarted without validating the plan", st)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestHolderRetryErrorIsNotReplacedByNormalRetry(t *testing.T) {
	for _, tc := range []struct {
		name string
		// put answers the retry after the GET identified the holder.
		put            func(w http.ResponseWriter)
		code           int
		leaderRetries  int32
		reasonContains string
	}{
		{
			name: "connection-lost",
			put: func(w http.ResponseWriter) {
				// The holder may have accepted the retry before the
				// connection was lost.
				conn, _, err := w.(http.Hijacker).Hijack()
				if err == nil {
					_ = conn.Close()
				}
			},
			code:           http.StatusServiceUnavailable,
			reasonContains: "held by starter owner",
		},
		{
			name: "no-longer-held",
			put: func(w http.ResponseWriter) {
				writeError(w, http.StatusNotFound, "This starter holds no failed upgrade step")
			},
			code:          http.StatusOK,
			leaderRetries: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var puts atomic.Int32
			owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPut {
					puts.Add(1)
					tc.put(w)
					return
				}
				markLocalUpgradeFailureHeld(w)
				w.Header().Set(contentTypeHeader, contentTypeJSON)
				_, _ = w.Write([]byte(`{"ready":false,"failed":true}`))
			}))
			defer owner.Close()
			leaderPeer := Peer{ID: "leader"}
			config := ClusterConfig{AllPeers: []Peer{leaderPeer, peerFor(t, "owner", owner.URL)}}
			leader := &fakeHTTPUpgrade{}
			s := serveUpgrade(&fakeHTTPContext{isMaster: true, mode: ServiceModeCluster, config: config, me: leaderPeer, upgrade: leader})

			w := doUpgradeRequest(s, http.MethodPut)
			require.Equal(t, tc.code, w.Code, w.Body.String())
			require.Contains(t, w.Body.String(), tc.reasonContains)
			require.Equal(t, int32(1), puts.Load())
			// Only an explicit "no longer held" falls back to the normal retry.
			require.Equal(t, tc.leaderRetries, leader.retries.Load())
		})
	}
}
