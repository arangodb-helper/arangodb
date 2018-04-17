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
	"fmt"
	"strings"
	"sync"
	"time"

	driver "github.com/arangodb/go-driver"
	"github.com/arangodb/go-driver/agency"
	logging "github.com/op/go-logging"
	"github.com/ryanuber/columnize"
)

// UpgradeManager is the API of a service used to control the upgrade process from 1 database version to the next.
type UpgradeManager interface {
	// StartDatabaseUpgrade is called to start the upgrade process
	StartDatabaseUpgrade() error

	// IsServerUpgradeInProgress returns true when the upgrade manager is busy upgrading the server of given type.
	IsServerUpgradeInProgress(serverType ServerType) bool

	// ServerDatabaseAutoUpgrade returns true if the server of given type must be started with --database.auto-upgrade
	ServerDatabaseAutoUpgrade(serverType ServerType) bool

	// ServerDatabaseAutoUpgradeStarter is called when a server of given type has been be started with --database.auto-upgrade
	ServerDatabaseAutoUpgradeStarter(serverType ServerType)
}

// UpgradeManagerContext holds methods used by the upgrade manager to control its context.
type UpgradeManagerContext interface {
	// ClusterConfig returns the current cluster configuration and the current peer
	ClusterConfig() (ClusterConfig, *Peer, ServiceMode)
	// CreateClient creates a go-driver client with authentication for the given endpoints.
	CreateClient(endpoints []string, followRedirect bool) (driver.Client, error)
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
	upgradeManagerLockKey     = []string{"arangodb-helper", "arangodb", "upgrade-manager"}
	superVisionMaintenanceKey = []string{"arango", "Supervision", "Maintenance"}
	superVisionStateKey       = []string{"arango", "Supervision", "State"}
)

const (
	upgradeManagerLockTTL       = time.Minute * 5
	superVisionMaintenanceTTL   = time.Hour
	superVisionStateMaintenance = "Maintenance"
	superVisionStateNormal      = "Normal"
)

// upgradeManager is a helper used to control the upgrade process from 1 database version to the next.
type upgradeManager struct {
	mutex                 sync.Mutex
	log                   *logging.Logger
	upgradeManagerContext UpgradeManagerContext
	upgradeServerType     ServerType
	updateNeeded          bool
}

// StartDatabaseUpgrade is called to start the upgrade process
func (m *upgradeManager) StartDatabaseUpgrade() error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// Fetch mode
	_, myPeer, mode := m.upgradeManagerContext.ClusterConfig()

	// Create lock (if needed)
	ctx := context.Background()
	var lock agency.Lock
	if mode.HasAgency() {
		m.log.Debug("Creating agency API")
		api, err := m.createAgencyAPI()
		if err != nil {
			return maskAny(err)
		}
		m.log.Debug("Creating lock")
		lock, err = agency.NewLock(m.log, api, upgradeManagerLockKey, "", upgradeManagerLockTTL)
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
	go m.runUpgradeProcess(ctx, myPeer, mode, lock)

	return nil
}

// IsServerUpgradeInProgress returns true when the upgrade manager is busy upgrading the server of given type.
func (m *upgradeManager) IsServerUpgradeInProgress(serverType ServerType) bool {
	return m.upgradeServerType == serverType
}

// serverDatabaseAutoUpgrade returns true if the server of given type must be started with --database.auto-upgrade
func (m *upgradeManager) ServerDatabaseAutoUpgrade(serverType ServerType) bool {
	return m.updateNeeded && m.upgradeServerType == serverType
}

// serverDatabaseAutoUpgradeStarter is called when a server of given type has been be started with --database.auto-upgrade
func (m *upgradeManager) ServerDatabaseAutoUpgradeStarter(serverType ServerType) {
	if m.upgradeServerType == serverType {
		m.updateNeeded = false
	}
}

// Create a client for the agency
func (m *upgradeManager) createAgencyAPI() (agency.Agency, error) {
	// Get cluster config
	clusterConfig, _, _ := m.upgradeManagerContext.ClusterConfig()
	// Create client
	a, err := clusterConfig.CreateAgencyAPI(m.upgradeManagerContext.CreateClient)
	if err != nil {
		return nil, maskAny(err)
	}
	return a, nil
}

// runUpgradeProcess runs the entire upgrade process until it is finished.
func (m *upgradeManager) runUpgradeProcess(ctx context.Context, myPeer *Peer, mode ServiceMode, lock agency.Lock) {
	// Unlock when we're done
	defer func() {
		m.upgradeServerType = ""
		m.updateNeeded = false
		if lock != nil {
			lock.Unlock(context.Background())
		}
	}()

	maintenanceModeSupported := false
	if mode.HasAgency() {
		// First wait for the agency to be health
		if err := m.waitUntil(ctx, m.isAgencyHealth, "Agency is not yet healthy: %v"); err != nil {
			return
		}

		// Look for support of maintenance mode
		var err error
		maintenanceModeSupported, err = m.isSuperVisionMaintenanceSupported(ctx)
		if err != nil {
			m.log.Errorf("Failed to check for support of maintenance mode: %v", err)
			return
		}

		// If maintenance mode is supported, disable supervision now
		if maintenanceModeSupported {
			m.log.Info("Disabling agency supervision")
			if err := m.disableSupervision(ctx); err != nil {
				m.log.Errorf("Failed to disabled supervision in the agency: %v", err)
				return
			}
		} else {
			m.log.Info("Agency supervision maintenance mode not supported on this version")
		}

		if myPeer.HasAgent() {
			// Restart the agency in auto-upgrade mode
			m.log.Info("Upgrading agent")
			m.upgradeServerType = ServerTypeAgent
			m.updateNeeded = true
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
	}

	if mode.IsSingleMode() {
		// Restart the single server in auto-upgrade mode
		m.log.Info("Upgrading single server")
		m.upgradeServerType = ServerTypeSingle
		m.updateNeeded = true
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
	} else if mode.IsActiveFailoverMode() {
		if myPeer.HasResilientSingle() {
			// Restart the single server in auto-upgrade mode
			m.log.Info("Upgrading single server")
			m.upgradeServerType = ServerTypeResilientSingle
			m.updateNeeded = true
			if err := m.upgradeManagerContext.RestartServer(ServerTypeResilientSingle); err != nil {
				m.log.Errorf("Failed to restart resilient single server: %v", err)
				return
			}

			// Wait until single server restarted
			if err := m.waitUntilUpgradeServerStarted(ctx); err != nil {
				return
			}

			// Wait until all single servers respond
			if err := m.waitUntil(ctx, m.areSingleServersResponding, "Resilient single server is not yet responding: %v"); err != nil {
				return
			}
		}
	} else if mode.IsClusterMode() {
		if myPeer.HasDBServer() {
			// Restart the dbserver in auto-upgrade mode
			m.log.Info("Upgrading dbserver")
			m.upgradeServerType = ServerTypeDBServer
			m.updateNeeded = true
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
		}

		if myPeer.HasCoordinator() {
			// Restart the coordinator in auto-upgrade mode
			m.log.Info("Upgrading coordinator")
			m.upgradeServerType = ServerTypeCoordinator
			m.updateNeeded = true
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
	}

	// If maintenance mode is supported, re-enable supervision now
	if maintenanceModeSupported {
		m.log.Info("Re-enabling agency supervision")
		if err := m.enableSupervision(ctx); err != nil {
			m.log.Errorf("Failed to enable supervision in the agency: %v", err)
			return
		}
	}

	// We're done
	allSameVersion, err := m.ShowArangodServerVersions(ctx)
	if err != nil {
		m.log.Errorf("Failed to show server versions: %v", err)
	} else if allSameVersion {
		m.log.Info("Upgrading done.")
	} else {
		m.log.Info("Upgrading of all servers controlled by this starter done, you can continue with the next starter now.")
	}
}

// waitUntilUpgradeServerStarted waits until the updateNeeded is false.
func (m *upgradeManager) waitUntilUpgradeServerStarted(ctx context.Context) error {
	for {
		if !m.updateNeeded {
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
	// Get cluster config
	clusterConfig, _, _ := m.upgradeManagerContext.ClusterConfig()
	// Build endpoint list
	endpoints, err := clusterConfig.GetAgentEndpoints()
	if err != nil {
		return maskAny(err)
	}
	// Build agency clients
	clients := make([]driver.Connection, 0, len(endpoints))
	for _, ep := range endpoints {
		c, err := m.upgradeManagerContext.CreateClient([]string{ep}, false)
		if err != nil {
			return maskAny(err)
		}
		clients = append(clients, c.Connection())
	}
	// Check health
	if err := agency.AreAgentsHealthy(ctx, clients); err != nil {
		return maskAny(err)
	}
	return nil
}

// areDBServersResponding performs a check if all dbservers are responding.
func (m *upgradeManager) areDBServersResponding(ctx context.Context) error {
	// Get cluster config
	clusterConfig, _, _ := m.upgradeManagerContext.ClusterConfig()
	// Build endpoint list
	endpoints, err := clusterConfig.GetDBServerEndpoints()
	if err != nil {
		return maskAny(err)
	}
	// Check all
	for _, ep := range endpoints {
		c, err := m.upgradeManagerContext.CreateClient([]string{ep}, false)
		if err != nil {
			return maskAny(err)
		}
		if _, err := c.ServerID(ctx); err != nil {
			return maskAny(err)
		}
	}
	return nil
}

// areCoordinatorsResponding performs a check if all coordinators are responding.
func (m *upgradeManager) areCoordinatorsResponding(ctx context.Context) error {
	// Get cluster config
	clusterConfig, _, _ := m.upgradeManagerContext.ClusterConfig()
	// Build endpoint list
	endpoints, err := clusterConfig.GetCoordinatorEndpoints()
	if err != nil {
		return maskAny(err)
	}
	// Check all
	for _, ep := range endpoints {
		c, err := m.upgradeManagerContext.CreateClient([]string{ep}, false)
		if err != nil {
			return maskAny(err)
		}
		if _, err := c.ServerID(ctx); err != nil {
			return maskAny(err)
		}
	}
	return nil
}

// areSingleServersResponding performs a check if all single servers are responding.
func (m *upgradeManager) areSingleServersResponding(ctx context.Context) error {
	// Get cluster config
	clusterConfig, _, mode := m.upgradeManagerContext.ClusterConfig()
	// Build endpoint list
	endpoints, err := clusterConfig.GetSingleEndpoints(mode.IsSingleMode())
	if err != nil {
		return maskAny(err)
	}
	// Check all
	for _, ep := range endpoints {
		c, err := m.upgradeManagerContext.CreateClient([]string{ep}, false)
		if err != nil {
			return maskAny(err)
		}
		if _, err := c.Version(ctx); err != nil {
			return maskAny(err)
		}
	}
	return nil
}

// isSuperVisionMaintenanceSupported checks all agents for their version number.
// If it is to low to support supervision maintenance mode, false is returned.
func (m *upgradeManager) isSuperVisionMaintenanceSupported(ctx context.Context) (bool, error) {
	// Get cluster config
	clusterConfig, _, _ := m.upgradeManagerContext.ClusterConfig()
	// Build endpoint list
	endpoints, err := clusterConfig.GetAgentEndpoints()
	if err != nil {
		return false, maskAny(err)
	}
	// Check agents
	for _, ep := range endpoints {
		c, err := m.upgradeManagerContext.CreateClient([]string{ep}, false)
		if err != nil {
			return false, maskAny(err)
		}
		info, err := c.Version(ctx)
		if err != nil {
			return false, maskAny(err)
		}
		version := driver.Version(info.Version)
		if version.Major() < 3 {
			return false, nil
		}
		if version.Major() == 3 {
			sub, _ := version.SubInt()
			switch version.Minor() {
			case 0, 1:
				return false, nil
			case 2:
				if sub < 14 {
					return false, nil
				}
			case 3:
				if sub < 8 {
					return false, nil
				}
			}
		}
	}
	return true, nil
}

// disableSupervision blocks supervision of the agency and waits for the agency to acknowledge.
func (m *upgradeManager) disableSupervision(ctx context.Context) error {
	api, err := m.createAgencyAPI()
	if err != nil {
		return maskAny(err)
	}
	// Set maintenance mode
	if err := api.WriteKey(ctx, superVisionMaintenanceKey, struct{}{}, superVisionMaintenanceTTL); err != nil {
		return maskAny(err)
	}
	// Wait for agency to acknowledge
	for {
		var value interface{}
		err := api.ReadKey(ctx, superVisionStateKey, &value)
		if err != nil {
			m.log.Warningf("Failed to read supervision state: %v", err)
		} else if valueStr, ok := value.(string); !ok {
			m.log.Warningf("Supervision state is not a string but: %v", value)
		} else if valueStr != superVisionStateMaintenance {
			m.log.Warningf("Supervision state is not yet '%s' but '%s'", superVisionStateMaintenance, valueStr)
		} else {
			return nil
		}
		select {
		case <-ctx.Done():
			return maskAny(ctx.Err())
		case <-time.After(time.Second):
			// Try again
		}
	}
}

// enableSupervision enabled supervision of the agency.
func (m *upgradeManager) enableSupervision(ctx context.Context) error {
	api, err := m.createAgencyAPI()
	if err != nil {
		return maskAny(err)
	}
	// Remove maintenance mode
	if err := api.RemoveKey(ctx, superVisionMaintenanceKey); err != nil {
		return maskAny(err)
	}
	return nil
}

// ShowServerVersions queries the versions of all Arangod servers in the cluster and shows them.
// Returns true when all servers are the same, false otherwise.
func (m *upgradeManager) ShowArangodServerVersions(ctx context.Context) (bool, error) {
	// Get cluster config
	clusterConfig, _, mode := m.upgradeManagerContext.ClusterConfig()
	versions := make(map[string]struct{})
	rows := []string{}

	showGroup := func(serverType ServerType, endpoints []string) {
		groupVersions := make([]string, len(endpoints))
		for i, ep := range endpoints {
			c, err := m.upgradeManagerContext.CreateClient([]string{ep}, false)
			if err != nil {
				groupVersions[i] = "?"
				continue
			}
			if info, err := c.Version(ctx); err != nil {
				groupVersions[i] = "?"
				continue
			} else {
				groupVersions[i] = string(info.Version)
			}
		}
		for i, v := range groupVersions {
			versions[v] = struct{}{}
			rows = append(rows, fmt.Sprintf("%s %d | %s", serverType, i+1, v))
		}
	}

	if mode.HasAgency() {
		endpoints, err := clusterConfig.GetAgentEndpoints()
		if err != nil {
			return false, maskAny(err)
		}
		showGroup(ServerTypeAgent, endpoints)
	}
	if mode.IsSingleMode() {
		endpoints, err := clusterConfig.GetSingleEndpoints(true)
		if err != nil {
			return false, maskAny(err)
		}
		showGroup(ServerTypeSingle, endpoints)
	}
	if mode.IsActiveFailoverMode() {
		endpoints, err := clusterConfig.GetSingleEndpoints(false)
		if err != nil {
			return false, maskAny(err)
		}
		showGroup(ServerTypeResilientSingle, endpoints)
	}
	if mode.IsClusterMode() {
		endpoints, err := clusterConfig.GetDBServerEndpoints()
		if err != nil {
			return false, maskAny(err)
		}
		showGroup(ServerTypeDBServer, endpoints)
		endpoints, err = clusterConfig.GetCoordinatorEndpoints()
		if err != nil {
			return false, maskAny(err)
		}
		showGroup(ServerTypeCoordinator, endpoints)
	}

	m.log.Info("Server versions:")
	rows = strings.Split(columnize.SimpleFormat(rows), "\n")
	for _, r := range rows {
		m.log.Info(r)
	}

	return len(versions) == 1, nil
}
