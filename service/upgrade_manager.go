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
	"net/http"
	"sync"
	"time"

	logging "github.com/op/go-logging"

	"github.com/arangodb-helper/arangodb/service/arangod"
)

// UpgradeManager is the API of a service used to control the upgrade process from 1 database version to the next.
type UpgradeManager interface {
	// StartDatabaseUpgrade is called to start the upgrade process
	StartDatabaseUpgrade() error

	// ServerDatabaseAutoUpgrade returns true if the server of given type must be started with --database.auto-upgrade
	ServerDatabaseAutoUpgrade(serverType ServerType) bool

	// ServerDatabaseAutoUpgradeStarter is called when a server of given type has been be started with --database.auto-upgrade
	ServerDatabaseAutoUpgradeStarter(serverType ServerType)
}

// UpgradeManagerContext holds methods used by the upgrade manager to control its context.
type UpgradeManagerContext interface {
	// ClusterConfig returns the current cluster configuration and the current peer
	ClusterConfig() (ClusterConfig, *Peer, ServiceMode)
	// PrepareDatabaseServerRequestFunc returns a function that is used to
	// prepare a request to a database server (including authentication).
	PrepareDatabaseServerRequestFunc() func(*http.Request) error
	// RestartServer triggers a restart of the server of the given type.
	RestartServer(serverType ServerType) error
}

// NewUpgradeManager creates a new upgrade manager.
func NewUpgradeManager(log *logging.Logger, upgradeManagerContext UpgradeManagerContext) UpgradeManager {
	return &upgradeManager{
		log: log,
		upgradeManagerContext: upgradeManagerContext,
	}
}

var (
	upgradeManagerLockKey = []string{"arangodb-helper", "arangodb", "upgrade-manager"}
)

const (
	upgradeManagerLockTTL = time.Minute * 5
)

// upgradeManager is a helper used to control the upgrade process from 1 database version to the next.
type upgradeManager struct {
	mutex                 sync.Mutex
	log                   *logging.Logger
	upgradeManagerContext UpgradeManagerContext
	upgradeServerType     ServerType
}

// StartDatabaseUpgrade is called to start the upgrade process
func (m *upgradeManager) StartDatabaseUpgrade() error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// Fetch mode
	_, _, mode := m.upgradeManagerContext.ClusterConfig()

	// Create lock (if needed)
	ctx := context.Background()
	var lock arangod.Lock
	if mode.HasAgency() {
		m.log.Debug("Creating agency API")
		api, err := m.createAgencyAPI()
		if err != nil {
			return maskAny(err)
		}
		m.log.Debug("Creating lock")
		lock, err = arangod.NewLock(m.log, api, upgradeManagerLockKey, "", upgradeManagerLockTTL)
		if err != nil {
			return maskAny(err)
		}

		// Claim the upgrade lock
		m.log.Debug("Locking lock")
		if err := lock.Lock(ctx); err != nil {
			m.log.Debugf("Lock failed: %v", err)
			return maskAny(err)
		}
	}

	// Run the upgrade process
	go m.runUpgradeProcess(ctx, mode, lock)

	return nil
}

// serverDatabaseAutoUpgrade returns true if the server of given type must be started with --database.auto-upgrade
func (m *upgradeManager) ServerDatabaseAutoUpgrade(serverType ServerType) bool {
	return m.upgradeServerType == serverType
}

// serverDatabaseAutoUpgradeStarter is called when a server of given type has been be started with --database.auto-upgrade
func (m *upgradeManager) ServerDatabaseAutoUpgradeStarter(serverType ServerType) {
	if m.upgradeServerType == serverType {
		m.upgradeServerType = ""
	}
}

// Create a client for the agency
func (m *upgradeManager) createAgencyAPI() (arangod.AgencyAPI, error) {
	prepareReq := m.upgradeManagerContext.PrepareDatabaseServerRequestFunc()
	// Get cluster config
	clusterConfig, _, _ := m.upgradeManagerContext.ClusterConfig()
	// Create client
	return clusterConfig.CreateAgencyAPI(prepareReq)
}

// runUpgradeProcess runs the entire upgrade process until it is finished.
func (m *upgradeManager) runUpgradeProcess(ctx context.Context, mode ServiceMode, lock arangod.Lock) {
	// Unlock when we're done
	defer func() {
		m.upgradeServerType = ""
		if lock != nil {
			lock.Unlock(context.Background())
		}
	}()

	if mode.HasAgency() {
		// First wait for the agency to be health
		if err := m.waitUntil(ctx, m.isAgencyHealth, "Agency is not yet healthy: %v"); err != nil {
			return
		}

		// Restart the agency in auto-upgrade mode
		m.log.Info("Upgrading agent")
		m.upgradeServerType = ServerTypeAgent
		if err := m.upgradeManagerContext.RestartServer(ServerTypeAgent); err != nil {
			m.log.Errorf("Failed to restart agent: %v", err)
			return
		}

		// Wait until agency restarted
		if err := m.waitUntilUpgradeServerStarted(ctx); err != nil {
			return
		}

		// Wait until agency happy again
		if err := m.waitUntil(ctx, m.isAgencyHealth, "Agency is not yet healthy: %v"); err != nil {
			return
		}
	}

	if mode.IsSingleMode() || mode.IsActiveFailoverMode() {
		// Restart the single server in auto-upgrade mode
		m.log.Info("Upgrading single server")
		m.upgradeServerType = ServerTypeSingle
		if err := m.upgradeManagerContext.RestartServer(ServerTypeSingle); err != nil {
			m.log.Errorf("Failed to restart single server: %v", err)
			return
		}

		// Wait until single server restarted
		if err := m.waitUntilUpgradeServerStarted(ctx); err != nil {
			return
		}

		// Wait until all single servers respond
		if err := m.waitUntil(ctx, m.areSingleServersResponding, "Single server is not yet responding: %v"); err != nil {
			return
		}
	} else if mode.IsClusterMode() {
		// Restart the dbserver in auto-upgrade mode
		m.log.Info("Upgrading dbserver")
		m.upgradeServerType = ServerTypeDBServer
		if err := m.upgradeManagerContext.RestartServer(ServerTypeDBServer); err != nil {
			m.log.Errorf("Failed to restart dbserver: %v", err)
			return
		}

		// Wait until dbserver restarted
		if err := m.waitUntilUpgradeServerStarted(ctx); err != nil {
			return
		}

		// Wait until all dbservers respond
		if err := m.waitUntil(ctx, m.areDBServersResponding, "DBServers are not yet all responding: %v"); err != nil {
			return
		}

		// Restart the coordinator in auto-upgrade mode
		m.log.Info("Upgrading coordinator")
		m.upgradeServerType = ServerTypeCoordinator
		if err := m.upgradeManagerContext.RestartServer(ServerTypeCoordinator); err != nil {
			m.log.Errorf("Failed to restart coordinator: %v", err)
			return
		}

		// Wait until coordinator restarted
		if err := m.waitUntilUpgradeServerStarted(ctx); err != nil {
			return
		}

		// Wait until all coordinators respond
		if err := m.waitUntil(ctx, m.areCoordinatorsResponding, "Coordinator are not yet all responding: %v"); err != nil {
			return
		}
	}

	// We're done
	m.log.Info("Upgrading done.")
}

// waitUntilUpgradeServerStarted waits until the upgradeServerType field is empty.
func (m *upgradeManager) waitUntilUpgradeServerStarted(ctx context.Context) error {
	for {
		if m.upgradeServerType == "" {
			return nil
		}
		select {
		case <-ctx.Done():
			return maskAny(ctx.Err())
		case <-time.After(time.Millisecond * 100):
			// Try again
		}
	}
}

// waitUntil loops until the the given predicate returns nil or the given context is
// canceled.
// Returns nil when agency is completely healthy, an error otherwise.
func (m *upgradeManager) waitUntil(ctx context.Context, predicate func(ctx context.Context) error, errorLogTemplate string) error {
	for {
		err := predicate(ctx)
		if err == nil {
			return nil
		}
		m.log.Infof(errorLogTemplate, err)
		select {
		case <-ctx.Done():
			return maskAny(ctx.Err())
		case <-time.After(time.Second * 5):
			// Try again
		}
	}
}

// isAgencyHealth performs a check if the agency is healthy.
// Returns nil when agency is completely healthy, an error
// when the agency is not healthy or its health state could not
// be determined.
func (m *upgradeManager) isAgencyHealth(ctx context.Context) error {
	prepareReq := m.upgradeManagerContext.PrepareDatabaseServerRequestFunc()
	// Get cluster config
	clusterConfig, _, _ := m.upgradeManagerContext.ClusterConfig()
	// Build endpoint list
	endpoints, err := clusterConfig.GetAgentEndpoints()
	if err != nil {
		return maskAny(err)
	}
	// Build agency clients
	clients := make([]arangod.AgencyAPI, 0, len(endpoints))
	for _, ep := range endpoints {
		c, err := arangod.NewServerClient(ep, prepareReq, false)
		if err != nil {
			return maskAny(err)
		}
		clients = append(clients, c.Agency())
	}
	// Check health
	if err := arangod.AreAgentsHealthy(ctx, clients); err != nil {
		return maskAny(err)
	}
	return nil
}

// areDBServersResponding performs a check if all dbservers are responding.
func (m *upgradeManager) areDBServersResponding(ctx context.Context) error {
	prepareReq := m.upgradeManagerContext.PrepareDatabaseServerRequestFunc()
	// Get cluster config
	clusterConfig, _, _ := m.upgradeManagerContext.ClusterConfig()
	// Build endpoint list
	endpoints, err := clusterConfig.GetDBServerEndpoints()
	if err != nil {
		return maskAny(err)
	}
	// Check all
	for _, ep := range endpoints {
		c, err := arangod.NewServerClient(ep, prepareReq, false)
		if err != nil {
			return maskAny(err)
		}
		sa, err := c.Server()
		if err != nil {
			return maskAny(err)
		}
		if _, err := sa.ID(ctx); err != nil {
			return maskAny(err)
		}
	}
	return nil
}

// areCoordinatorsResponding performs a check if all coordinators are responding.
func (m *upgradeManager) areCoordinatorsResponding(ctx context.Context) error {
	prepareReq := m.upgradeManagerContext.PrepareDatabaseServerRequestFunc()
	// Get cluster config
	clusterConfig, _, _ := m.upgradeManagerContext.ClusterConfig()
	// Build endpoint list
	endpoints, err := clusterConfig.GetCoordinatorEndpoints()
	if err != nil {
		return maskAny(err)
	}
	// Check all
	for _, ep := range endpoints {
		c, err := arangod.NewServerClient(ep, prepareReq, false)
		if err != nil {
			return maskAny(err)
		}
		sa, err := c.Server()
		if err != nil {
			return maskAny(err)
		}
		if _, err := sa.ID(ctx); err != nil {
			return maskAny(err)
		}
	}
	return nil
}

// areSingleServersResponding performs a check if all single servers are responding.
func (m *upgradeManager) areSingleServersResponding(ctx context.Context) error {
	prepareReq := m.upgradeManagerContext.PrepareDatabaseServerRequestFunc()
	// Get cluster config
	clusterConfig, _, mode := m.upgradeManagerContext.ClusterConfig()
	// Build endpoint list
	endpoints, err := clusterConfig.GetSingleEndpoints(mode.IsSingleMode())
	if err != nil {
		return maskAny(err)
	}
	// Check all
	for _, ep := range endpoints {
		c, err := arangod.NewServerClient(ep, prepareReq, false)
		if err != nil {
			return maskAny(err)
		}
		sa, err := c.Server()
		if err != nil {
			return maskAny(err)
		}
		if _, err := sa.Version(ctx); err != nil {
			return maskAny(err)
		}
	}
	return nil
}
