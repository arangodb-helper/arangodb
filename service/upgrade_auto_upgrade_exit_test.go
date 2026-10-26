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
	"testing"
	"time"

	"github.com/arangodb-helper/arangodb/pkg/definitions"
	"github.com/arangodb-helper/arangodb/pkg/utils"
	"github.com/arangodb/go-driver"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

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
	require.Equal(t, int64(len("previous server shutdown\n")), serverHostLogOffset(staticLogContext{logPath: path}, definitions.ServerTypeDBServer))
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
