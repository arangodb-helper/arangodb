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
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"github.com/ryanuber/columnize"

	"github.com/arangodb-helper/arangodb/agency"
	driver "github.com/arangodb/go-driver/v2/arangodb"
	driver_http "github.com/arangodb/go-driver/v2/connection"
	upgraderules "github.com/arangodb/go-upgrade-rules"

	"github.com/arangodb-helper/arangodb/client"
	"github.com/arangodb-helper/arangodb/pkg/definitions"
)

// UpgradeManager is the API of a service used to control the upgrade process from 1 database version to the next.
type UpgradeManager interface {
	// StartDatabaseUpgrade is called to start the upgrade process
	StartDatabaseUpgrade(ctx context.Context, forceMinorUpgrade bool) error

	// RetryDatabaseUpgrade resets a failure mark in the existing upgrade plan
	// such that the starters will retry the upgrade once more.
	RetryDatabaseUpgrade(ctx context.Context) error

	// AbortDatabaseUpgrade removes the existing upgrade plan.
	// Note that Starters working on an entry of the upgrade
	// will finish that entry.
	// If there is no plan, a NotFoundError will be returned.
	AbortDatabaseUpgrade(ctx context.Context) error

	// Status returns the status of any upgrade plan
	Status(context.Context) (client.UpgradeStatus, error)

	// IsServerUpgradeInProgress returns true when the upgrade manager is busy upgrading the server of given type.
	IsServerUpgradeInProgress(serverType definitions.ServerType) bool

	// ServerDatabaseAutoUpgrade returns true if the server of given type must be started with --database.auto-upgrade
	ServerDatabaseAutoUpgrade(serverType definitions.ServerType, lastExitCode int) bool

	// ServerDatabaseAutoUpgradeStarter is called when a server of given type has been started with --database.auto-upgrade
	ServerDatabaseAutoUpgradeStarter(serverType definitions.ServerType)

	// RunWatchUpgradePlan keeps watching the upgrade plan until the given context is canceled.
	RunWatchUpgradePlan(context.Context)
}

// UpgradeManagerContext holds methods used by the upgrade manager to control its context.
type UpgradeManagerContext interface {
	ClientBuilder
	// ClusterConfig returns the current cluster configuration and the current peer
	ClusterConfig() (ClusterConfig, *Peer, ServiceMode)
	// RestartServer triggers a restart of the server of the given type.
	RestartServer(serverType definitions.ServerType) error
	// IsRunningMaster returns if the starter is the running master.
	IsRunningMaster() (isRunningMaster, isRunning bool, masterURL string)
	// TestInstance checks the `up` status of an arangod server instance.
	TestInstance(ctx context.Context, serverType definitions.ServerType, address string, port int,
		statusChanged chan StatusItem) (up, correctRole bool, version, role, mode string, statusTrail []int, cancelled bool)
}

// NewUpgradeManager creates a new upgrade manager.
func NewUpgradeManager(log zerolog.Logger, upgradeManagerContext UpgradeManagerContext) UpgradeManager {
	return &upgradeManager{
		log:                   log,
		upgradeManagerContext: upgradeManagerContext,
	}
}

var (
	upgradeManagerLockKey     = []string{"arangodb-helper", "arangodb", "upgrade-manager"}
	upgradePlanKey            = []string{"arangodb-helper", "arangodb", "upgrade-plan"}
	upgradePlanRevisionKey    = []string{"arangodb-helper", "arangodb", "upgrade-plan", "revision"}
	superVisionMaintenanceKey = []string{"arango", "Supervision", "Maintenance"}
	superVisionStateKey       = []string{"arango", "Supervision", "State"}
)

const (
	upgradeManagerLockTTL       = time.Minute * 5
	superVisionMaintenanceTTL   = time.Hour
	superVisionStateMaintenance = "Maintenance"
)

// UpgradePlan is the JSON structure that describes a plan to upgrade
// a deployment to a new version.
type UpgradePlan struct {
	Revision        int                `json:"revision"` // Must match with upgradePlanRevisionKey
	CreatedAt       time.Time          `json:"created_at"`
	LastModifiedAt  time.Time          `json:"last_modified_at"`
	Entries         []UpgradePlanEntry `json:"entries"`
	FinishedEntries []UpgradePlanEntry `json:"finished_entries"`
	Finished        bool               `json:"finished"`
	FromVersions    []driver.Version   `json:"from_versions"`
	ToVersion       driver.Version     `json:"to_version"`
}

// IsEmpty returns true when the given plan has not been initialized.
func (p UpgradePlan) IsEmpty() bool {
	return p.CreatedAt.IsZero()
}

// IsReady returns true when all entries have finished.
func (p UpgradePlan) IsReady() bool {
	return len(p.Entries) == 0
}

// IsFailed returns true when one of the entries has failures.
func (p UpgradePlan) IsFailed() bool {
	for _, e := range p.Entries {
		if e.Failures > 0 {
			return true
		}
	}
	return false
}

// ResetFailures resets all Failures field to 0.
func (p *UpgradePlan) ResetFailures() {
	for _, e := range p.Entries {
		e.Failures = 0
		e.Reason = ""
	}
}

// Equals returns true if other plan is the same
func (p UpgradePlan) Equals(other UpgradePlan) bool {
	return reflect.DeepEqual(p, other)
}

// UpgradeEntryType is a strongly typed upgrade plan item
type UpgradeEntryType string

const (
	UpgradeEntryTypeAgent       = "agent"
	UpgradeEntryTypeDBServer    = "dbserver"
	UpgradeEntryTypeCoordinator = "coordinator"
	UpgradeEntryTypeSingle      = "single"
)

// UpgradePlanEntry is the JSON structure that describes a single entry
// in an upgrade plan.
type UpgradePlanEntry struct {
	PeerID        string           `json:"peer_id"`
	Type          UpgradeEntryType `json:"type"`
	Failures      int              `json:"failures,omitempty"`
	Reason        string           `json:"reason,omitempty"`
	WithoutResign bool             `json:"withoutResign,omitempty"`
}

// CreateStatusServer creates a UpgradeStatusServer for the given entry.
// When the entry does not involve a specific server, nil is returned.
func (e UpgradePlanEntry) CreateStatusServer(upgradeManagerContext UpgradeManagerContext) (*client.UpgradeStatusServer, error) {
	config, _, _ := upgradeManagerContext.ClusterConfig()
	var serverType definitions.ServerType
	switch e.Type {
	case UpgradeEntryTypeAgent:
		serverType = definitions.ServerTypeAgent
	case UpgradeEntryTypeDBServer:
		serverType = definitions.ServerTypeDBServer
	case UpgradeEntryTypeCoordinator:
		serverType = definitions.ServerTypeCoordinator
	case UpgradeEntryTypeSingle:
		serverType = definitions.ServerTypeSingle
	default:
		return nil, maskAny(fmt.Errorf("unknown entry type '%s'", e.Type))
	}
	peer, found := config.PeerByID(e.PeerID)
	if !found {
		return nil, maskAny(fmt.Errorf("unknown entry peer ID '%s'", e.PeerID))
	}
	port := peer.Port + peer.PortOffset + serverType.PortOffset()
	return &client.UpgradeStatusServer{
		Type:    client.ServerType(serverType),
		Address: peer.Address,
		Port:    port,
	}, nil
}

// upgradeManager is a helper used to control the upgrade process from 1 database version to the next.
type upgradeManager struct {
	mutex                 sync.Mutex
	agencyAPIMu           sync.Mutex // protects agencyAPI (createAgencyAPI is called from multiple goroutines)
	log                   zerolog.Logger
	upgradeManagerContext UpgradeManagerContext
	upgradeServerType     definitions.ServerType
	updateNeeded          bool
	agencyAPI             agency.Agency
}

// StartDatabaseUpgrade is called to start the upgrade process
func (m *upgradeManager) StartDatabaseUpgrade(ctx context.Context, forceMinorUpgrade bool) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// Check the versions of all starters
	if err := m.checkStarterVersions(ctx); err != nil {
		return maskAny(err)
	}

	// Fetch (binary) database versions of all starters
	binaryDBVersions, err := m.fetchBinaryDatabaseVersions(ctx)
	if err != nil {
		return maskAny(err)
	}
	if len(binaryDBVersions) > 1 {
		return maskAny(client.NewBadRequestError(fmt.Sprintf("Found multiple database versions (%v). Make sure all machines have the same version", binaryDBVersions)))
	}
	if len(binaryDBVersions) == 0 {
		return maskAny(client.NewBadRequestError("Found no database versions. This is likely a bug"))
	}
	toVersion := binaryDBVersions[0]

	// Fetch (running) database versions of all starters
	runningDBVersions, err := m.fetchRunningDatabaseVersions(ctx)
	if err != nil {
		return maskAny(err)
	}

	// Check if we can upgrade from running to binary versions
	specialUpgradeFrom346 := false
	specialUpgradeFrom3614 := false
	rules := upgraderules.CheckUpgradeRules
	if forceMinorUpgrade {
		rules = upgraderules.CheckSoftUpgradeRules
	}

	for _, from := range runningDBVersions {
		if err := rules(from, toVersion); err != nil {
			return maskAny(errors.Wrap(err, "Found incompatible upgrade versions"))
		}
		if from.CompareTo("3.4.6") == 0 {
			specialUpgradeFrom346 = true
		}
		if IsSpecialUpgradeFrom3614(from) {
			specialUpgradeFrom3614 = true
		}
	}

	// Fetch mode
	config, myPeer, mode := m.upgradeManagerContext.ClusterConfig()

	if mode.IsSingleMode() {
		// Run upgrade without agency (i.e., SingleServer)

		// Create a new context to be independent of ctx
		timeoutContext, _ := context.WithTimeout(context.Background(), time.Minute*5)
		go m.runSingleServerUpgradeProcess(timeoutContext, myPeer, mode)
		return nil
	}

	// Check cluster health
	if mode.IsClusterMode() {
		if err := m.isClusterHealthy(ctx); err != nil {
			return maskAny(errors.Wrap(err, "Cannot upgrade unhealthy cluster"))
		}
	}

	// Run upgrade with agency.
	// Create an agency lock, so we know we're the only one to create a plan.
	m.log.Debug().Msg("Creating agency API")
	api, err := m.createAgencyAPI()
	if err != nil {
		return maskAny(errors.Wrap(err, "Cannot upgrade: cannot connect to agency"))
	}

	// Use peer ID for lock holder identification (myPeer already declared above)
	m.log.Debug().Msg("Creating lock")
	lock, err := agency.NewLock(api, upgradeManagerLockKey, myPeer.ID, upgradeManagerLockTTL)
	if err != nil {
		return maskAny(err)
	}

	// Claim the upgrade lock
	m.log.Debug().Msg("Acquiring lock")
	lockAcquired := false
	if err := lock.Acquire(ctx); err != nil {
		if !strings.Contains(err.Error(), "lock already held") {
			m.log.Debug().Err(err).Msg("Lock acquisition failed")
			return maskAny(err)
		}

		const (
			lockRetryWindow = 20 * time.Second
			lockRetryDelay  = 300 * time.Millisecond
		)
		deadline := time.Now().Add(lockRetryWindow)
		for {
			if ctx.Err() != nil {
				return ctx.Err()
			}

			plan, planErr := m.readUpgradePlan(ctx)
			if planErr == nil && !plan.IsReady() {
				m.log.Debug().Msg("Upgrade plan is already in progress")
				return nil
			}
			if planErr != nil && !agency.IsKeyNotFound(planErr) {
				return maskAny(planErr)
			}

			if acquireErr := lock.Acquire(ctx); acquireErr == nil {
				lockAcquired = true
				break
			} else if !strings.Contains(acquireErr.Error(), "lock already held") {
				m.log.Debug().Err(acquireErr).Msg("Lock acquisition failed")
				return maskAny(acquireErr)
			}

			if time.Now().After(deadline) {
				// Some environments can leave this lock key contended/stale while no
				// plan exists. Continue idempotently and let plan checks/writes decide.
				m.log.Warn().Err(err).Msg("Proceeding without upgrade lock after contention window")
				break
			}

			time.Sleep(lockRetryDelay)
		}
	} else {
		lockAcquired = true
	}

	// Close agency lock when we're done
	if lockAcquired {
		defer func() {
			m.log.Debug().Msg("Releasing lock")
			releaseCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := lock.Release(releaseCtx); err != nil {
				m.log.Warn().Err(err).Msg("Failed to release lock")
			}
		}()
	}

	m.log.Debug().Msg("Reading upgrade plan...")
	// Check existing plan
	plan, err := m.readUpgradePlan(ctx)
	if err != nil && !agency.IsKeyNotFound(err) {
		// Failed to read upgrade plan
		m.log.Error().Msg("Failed to read upgrade plan")
		return errors.Wrap(err, "Failed to read upgrade plan")
	}

	m.log.Debug().Msg("Checking if plan is ready...")
	// Check plan status
	if !plan.IsReady() {
		m.log.Debug().Msg("Current upgrade plan has not finished yet.")
		return maskAny(client.NewBadRequestError("Current upgrade plan has not finished yet"))
	}

	// Special measure for upgrades from 3.4.6:
	if specialUpgradeFrom346 {
		// Write 1000 dummy values into agency to advance the log:
		for i := 0; i < 1000; i++ {
			err := api.WriteKey(ctx, []string{"/arangodb-helper/dummy"}, 17, 0)
			if err != nil {
				m.log.Error().Msg("Could not append log entries to agency.")
				return maskAny(err)
			}
		}
		m.log.Debug().Msg("Have written 1000 log entries into agency.")

		// wait for the compaction to be created
		time.Sleep(3 * time.Second)

		// Repair each agent's persistent snapshots:
		for _, p := range config.AllPeers {
			if p.HasAgent() {
				cli, err := p.CreateAgentAPI(m.upgradeManagerContext)
				if err != nil {
					m.log.Error().Msgf("Could not create client for agent of peer %s", p.ID)
					return maskAny(err)
				}
				db, err := cli.GetDatabase(ctx, "_system", nil)
				if err != nil {
					m.log.Error().Msgf("Could not find _system database for agent of peer %s", p.ID)
					return maskAny(err)
				}
				_, err = db.Query(ctx, "FOR x IN compact LET old = x.readDB LET new = (FOR i IN 0..LENGTH(old)-1 RETURN i == 1 ? {} : old[i]) UPDATE x._key WITH {readDB: new} IN compact", nil)
				if err != nil {
					m.log.Error().Msgf("Could not repair agent log compaction for agent of peer %s", p.ID)
				}
				m.log.Debug().Msgf("Finished repair of log compaction for agent of peer %s", p.ID)
			}
		}

		m.log.Info().Msg("Applied special upgrade procedure for 3.4.6")
	}

	// Create upgrade plan
	m.log.Debug().Msg("Creating upgrade plan")
	plan = UpgradePlan{
		CreatedAt:      time.Now(),
		LastModifiedAt: time.Now(),
		FromVersions:   runningDBVersions,
		ToVersion:      toVersion,
	}
	// First add all agents
	for _, p := range config.AllPeers {
		if p.HasAgent() {
			plan.Entries = append(plan.Entries, UpgradePlanEntry{
				Type:   UpgradeEntryTypeAgent,
				PeerID: p.ID,
			})
		}
	}
	// If cluster...
	if mode.IsClusterMode() {
		// Add all dbservers
		for _, p := range config.AllPeers {
			if p.HasDBServer() {
				plan.Entries = append(plan.Entries, UpgradePlanEntry{
					Type:          UpgradeEntryTypeDBServer,
					PeerID:        p.ID,
					WithoutResign: specialUpgradeFrom3614,
				})
			}
		}
		// Add all coordinators
		for _, p := range config.AllPeers {
			if p.HasCoordinator() {
				plan.Entries = append(plan.Entries, UpgradePlanEntry{
					Type:   UpgradeEntryTypeCoordinator,
					PeerID: p.ID,
				})
			}
		}
	}

	// Save plan
	m.log.Debug().Msg("Writing upgrade plan")
	overwrite := true
	if _, err := m.writeUpgradePlan(ctx, plan, overwrite); agency.IsPreconditionFailed(err) {
		m.log.Error().Msg("Failed to write upgrade plan because it was outdated or removed.")
		return errors.Wrap(err, "Failed to write upgrade plan because is was outdated or removed")
	} else if err != nil {
		m.log.Error().Msgf("Failed to write upgrade plan %v.", err)
		return errors.Wrap(err, "Failed to write upgrade plan")
	}

	// Inform user
	m.log.Info().Msgf("Created plan to upgrade from %v to %v", runningDBVersions, binaryDBVersions)

	// We're done
	return nil
}

// RetryDatabaseUpgrade resets a failure mark in the existing upgrade plan
// such that the starters will retry the upgrade once more.
func (m *upgradeManager) RetryDatabaseUpgrade(ctx context.Context) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// Fetch mode
	_, _, mode := m.upgradeManagerContext.ClusterConfig()

	if !mode.HasAgency() {
		// Without an agency there is not upgrade plan to retry
		return maskAny(client.NewBadRequestError("Retry needs an agency"))
	}

	// Check cluster health
	if mode.IsClusterMode() {
		if err := m.isClusterHealthy(ctx); err != nil {
			return maskAny(errors.Wrap(err, "Found unhealthy cluster"))
		}
	}

	// Note that in contrast to StartDatabaseUpgrade we do not use an agency lock
	// here. The reason for that is that we expect to have a plan and use
	// the revision condition to ensure a "safe" update.

	// Retry upgrade with agency.
	plan, err := m.readUpgradePlan(ctx)
	if agency.IsKeyNotFound(err) {
		// There is no upgrade plan
		return maskAny(client.NewBadRequestError("There is no upgrade plan"))
	}
	if err != nil {
		// Failed to read upgrade plan
		return errors.Wrap(err, "Failed to read upgrade plan")
	}

	// Check failure status
	if !plan.IsFailed() {
		return maskAny(client.NewBadRequestError("The upgrade plan has not failed"))
	}

	// Reset failures and write plan
	plan.ResetFailures()
	overwrite := false
	if _, err := m.writeUpgradePlan(ctx, plan, overwrite); agency.IsPreconditionFailed(err) {
		return errors.Wrap(err, "Failed to write upgrade plan because is was outdated or removed")
	} else if err != nil {
		return errors.Wrap(err, "Failed to write upgrade plan")
	}

	// Inform user
	m.log.Info().Msg("Reset failures in upgrade plan so it can be retried")

	return nil
}

// AbortDatabaseUpgrade removes the existing upgrade plan.
// Note that Starters working on an entry of the upgrade
// will finish that entry.
// If there is no plan, a NotFoundError will be returned.
func (m *upgradeManager) AbortDatabaseUpgrade(ctx context.Context) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// Fetch mode
	_, _, mode := m.upgradeManagerContext.ClusterConfig()

	if !mode.HasAgency() {
		// Without an agency there is not upgrade plan to abort
		return maskAny(client.NewBadRequestError("Abort needs an agency"))
	}

	// Run upgrade with agency.
	// Create an agency lock, so we know we're the only one to create a plan.
	m.log.Debug().Msg("Creating agency API")
	api, err := m.createAgencyAPI()
	if err != nil {
		return maskAny(err)
	}

	// Get peer ID for lock holder identification
	_, myPeer, _ := m.upgradeManagerContext.ClusterConfig()
	m.log.Debug().Msg("Creating lock")
	lock, err := agency.NewLock(api, upgradeManagerLockKey, myPeer.ID, upgradeManagerLockTTL)
	if err != nil {
		return maskAny(err)
	}

	// Claim the upgrade lock
	m.log.Debug().Msg("Acquiring lock")
	if err := lock.Acquire(ctx); err != nil {
		m.log.Debug().Err(err).Msg("Lock acquisition failed")
		return maskAny(err)
	}

	// Close agency lock when we're done
	defer func() {
		m.log.Debug().Msg("Releasing lock")
		releaseCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := lock.Release(releaseCtx); err != nil {
			m.log.Warn().Err(err).Msg("Failed to release lock")
		}
	}()

	// Check plan
	if _, err := m.readUpgradePlan(ctx); agency.IsKeyNotFound(err) {
		// There is no plan
		return maskAny(client.NewNotFoundError("There is no upgrade plan"))
	}

	// Remove plan
	m.log.Debug().Msg("Removing upgrade plan")
	if err := m.removeUpgradePlan(ctx); err != nil {
		return errors.Wrap(err, "Failed to remove upgrade plan")
	}

	// Inform user
	m.log.Info().Msgf("Removed upgrade plan")

	// We're done
	return nil
}

// Status returns the current status of the upgrade process.
func (m *upgradeManager) Status(ctx context.Context) (client.UpgradeStatus, error) {
	_, _, mode := m.upgradeManagerContext.ClusterConfig()
	if !mode.HasAgency() {
		return client.UpgradeStatus{}, maskAny(client.NewPreconditionFailedError("Mode does not support agency based upgrades"))
	}

	plan, err := m.readUpgradePlan(ctx)
	if agency.IsKeyNotFound(err) {
		// No plan, return not found error
		return client.UpgradeStatus{}, maskAny(client.NewNotFoundError("There is no upgrade plan"))
	} else if err != nil {
		return client.UpgradeStatus{}, maskAny(err)
	}
	result := client.UpgradeStatus{
		Ready:        plan.IsReady(),
		Failed:       plan.IsFailed(),
		FromVersions: plan.FromVersions,
		ToVersion:    plan.ToVersion,
	}
	for _, entry := range plan.Entries {
		if entry.Failures > 0 && result.Reason == "" {
			result.Reason = entry.Reason
		}
		statusServer, err := entry.CreateStatusServer(m.upgradeManagerContext)
		if err != nil {
			return client.UpgradeStatus{}, maskAny(err)
		}
		if statusServer != nil {
			result.ServersRemaining = append(result.ServersRemaining, *statusServer)
		}
	}
	for _, entry := range plan.FinishedEntries {
		statusServer, err := entry.CreateStatusServer(m.upgradeManagerContext)
		if err != nil {
			return client.UpgradeStatus{}, maskAny(err)
		}
		if statusServer != nil {
			result.ServersUpgraded = append(result.ServersUpgraded, *statusServer)
		}
	}
	return result, nil
}

// checkStarterVersions ensures that all starters have the same version.
func (m *upgradeManager) checkStarterVersions(ctx context.Context) error {
	config, _, _ := m.upgradeManagerContext.ClusterConfig()
	endpoints, err := config.GetPeerEndpoints()
	if err != nil {
		return maskAny(err)
	}
	versions := make(map[string]struct{})
	for _, ep := range endpoints {
		m.log.Debug().Str("endpoint", ep).Msg("Checking Starter version")
		epURL, err := url.Parse(ep)
		if err != nil {
			return maskAny(err)
		}
		c, err := client.NewArangoStarterClient(*epURL)
		if err != nil {
			return maskAny(err)
		}
		info, err := c.Version(ctx)
		if err != nil {
			return maskAny(err)
		}
		version := info.Version + " / " + info.Build
		versions[version] = struct{}{}
	}
	if len(versions) > 1 {
		list := make([]string, 0, len(versions))
		for v := range versions {
			list = append(list, v)
		}
		return maskAny(fmt.Errorf("Found multiple Starter versions: %s", strings.Join(list, ", ")))
	}
	return nil
}

// fetchBinaryDatabaseVersions asks all starters for the version of the arangod binary.
// It returns all distinct versions.
func (m *upgradeManager) fetchBinaryDatabaseVersions(ctx context.Context) ([]driver.Version, error) {
	config, _, _ := m.upgradeManagerContext.ClusterConfig()
	endpoints, err := config.GetPeerEndpoints()
	if err != nil {
		return nil, maskAny(err)
	}
	versionMap := make(map[driver.Version]struct{})
	var versionList []driver.Version
	for _, ep := range endpoints {
		m.log.Debug().Str("endpoint", ep).Msg("Checking Database version")
		epURL, err := url.Parse(ep)
		if err != nil {
			return nil, maskAny(err)
		}
		c, err := client.NewArangoStarterClient(*epURL)
		if err != nil {
			return nil, maskAny(err)
		}
		version, err := c.DatabaseVersion(ctx)
		if err != nil {
			return nil, maskAny(err)
		}
		if _, found := versionMap[version]; !found {
			versionMap[version] = struct{}{}
			versionList = append(versionList, version)
		}
	}
	return versionList, nil
}

// fetchRunningDatabaseVersions asks all arangod servers in the deployment for their version.
// It returns all distinct versions.
func (m *upgradeManager) fetchRunningDatabaseVersions(ctx context.Context) ([]driver.Version, error) {
	config, _, mode := m.upgradeManagerContext.ClusterConfig()
	versionMap := make(map[driver.Version]struct{})
	var versionList []driver.Version

	collect := func(endpointsGetter func() ([]string, error), connectionType ConnectionType) error {
		endpoints, err := endpointsGetter()
		if err != nil {
			return maskAny(err)
		}
		for _, ep := range endpoints {
			m.log.Debug().Str("endpoint", ep).Msg("Checking Running Database version")
			c, err := m.upgradeManagerContext.CreateClient([]string{ep}, connectionType, definitions.ServerTypeUnknown)
			if err != nil {
				return maskAny(err)
			}
			v, err := c.Version(ctx)
			if err != nil {
				return maskAny(err)
			}
			if _, found := versionMap[v.Version]; !found {
				versionMap[v.Version] = struct{}{}
				versionList = append(versionList, v.Version)
			}
		}
		return nil
	}

	if mode.HasAgency() {
		if err := collect(config.GetAgentEndpoints, ConnectionTypeAgency); err != nil {
			return nil, maskAny(err)
		}
	}
	if mode.IsClusterMode() {
		if err := collect(config.GetCoordinatorEndpoints, ConnectionTypeDatabase); err != nil {
			return nil, maskAny(err)
		}
		if err := collect(config.GetDBServerEndpoints, ConnectionTypeDatabase); err != nil {
			return nil, maskAny(err)
		}
	}
	if mode.IsSingleMode() {
		if err := collect(config.GetSingleEndpoints, ConnectionTypeDatabase); err != nil {
			return nil, maskAny(err)
		}
	}

	return versionList, nil
}

// Errorf is a wrapper for log.Error()... used by the agency lock
func (m *upgradeManager) Errorf(msg string, args ...interface{}) {
	m.log.Error().Msgf(msg, args...)
}

// IsServerUpgradeInProgress returns true when the upgrade manager is busy upgrading the server of given type.
func (m *upgradeManager) IsServerUpgradeInProgress(serverType definitions.ServerType) bool {
	return m.upgradeServerType == serverType
}

// ServerDatabaseAutoUpgrade returns true if the server of given type must be started with --database.auto-upgrade
func (m *upgradeManager) ServerDatabaseAutoUpgrade(serverType definitions.ServerType, lastExitCode int) bool {
	return m.updateNeeded && m.upgradeServerType == serverType || lastExitCode == definitions.ArangoDExitUpgradeRequired
}

// ServerDatabaseAutoUpgradeStarter is called when a server of given type has been be started with --database.auto-upgrade
func (m *upgradeManager) ServerDatabaseAutoUpgradeStarter(serverType definitions.ServerType) {
	if m.upgradeServerType == serverType {
		m.updateNeeded = false
	}
}

// Create a client for the agency. The client is cached so that the
// underlying RoundRobinEndpoints state persists across calls, allowing
// automatic rotation to healthy agents when one is unreachable.
// Safe for concurrent use from RunWatchUpgradePlan and upgrade endpoints.
func (m *upgradeManager) createAgencyAPI() (agency.Agency, error) {
	m.agencyAPIMu.Lock()
	defer m.agencyAPIMu.Unlock()
	if m.agencyAPI != nil {
		return m.agencyAPI, nil
	}
	// Get cluster config
	clusterConfig, _, _ := m.upgradeManagerContext.ClusterConfig()
	// Create client
	a, err := clusterConfig.CreateAgencyAPI(m.upgradeManagerContext)
	if err != nil {
		return nil, maskAny(err)
	}
	m.agencyAPI = a
	return a, nil
}

// readUpgradePlan reads the current upgrade plan from the agency.
func (m *upgradeManager) readUpgradePlan(ctx context.Context) (UpgradePlan, error) {
	var plan UpgradePlan
	api, err := m.createAgencyAPI()
	if err != nil {
		return UpgradePlan{}, maskAny(err)
	}
	if err := api.ReadKey(ctx, upgradePlanKey, &plan); err != nil {
		return UpgradePlan{}, maskAny(err)
	}
	return plan, nil
}

// writeUpgradePlan writes the given upgrade plan to the agency.
// Unless overwrite is true, the revision currently in the agency must match
// the revision in the given plan. The revision is increased just before writing.
func (m *upgradeManager) writeUpgradePlan(ctx context.Context, plan UpgradePlan, overwrite bool) (UpgradePlan, error) {
	api, err := m.createAgencyAPI()
	if err != nil {
		return UpgradePlan{}, maskAny(err)
	}
	oldRevision := plan.Revision
	plan.Revision++
	plan.LastModifiedAt = time.Now()
	var condition agency.WriteCondition
	if !overwrite {
		// Use optimistic concurrency control: ensure revision hasn't changed
		condition = agency.IfEqualTo(upgradePlanRevisionKey, oldRevision)
	}
	// When overwrite is true, condition is nil and will be skipped by buildPreconditionsMap
	if err := api.WriteKey(ctx, upgradePlanKey, plan, 0, condition); err != nil {
		return UpgradePlan{}, maskAny(err)
	}
	return plan, nil
}

// removeUpgradePlan removes the current upgrade plan from the agency.
func (m *upgradeManager) removeUpgradePlan(ctx context.Context) error {
	api, err := m.createAgencyAPI()
	if err != nil {
		return maskAny(err)
	}
	if err := api.RemoveKey(ctx, upgradePlanKey); err != nil {
		return maskAny(err)
	}
	return nil
}

// waitForPlanChanges returns unbuffered channel on which updated UpgradePlan will be delivered.
// Closing channel means that there will be no more changes.
func (m *upgradeManager) waitForPlanChanges(ctx context.Context) chan *UpgradePlan {
	ch := make(chan *UpgradePlan)

	go func() {
		defer close(ch)
		var oldPlan UpgradePlan
		for {
			delay := time.Second * 3
			plan, err := m.readUpgradePlan(ctx)
			if agency.IsKeyNotFound(err) || plan.IsEmpty() {
				// Just try later
			} else if err != nil {
				// Failed to read plan
				m.log.Info().Err(err).Msg("Failed to read upgrade plan")
			} else if !oldPlan.Equals(plan) {
				ch <- &plan
				oldPlan = plan
				delay = time.Second
			}

			timer := time.NewTimer(delay)
			select {
			case <-timer.C:
				// Continue
			case <-ctx.Done():
				// Context canceled
				if !timer.Stop() {
					<-timer.C
				}
				return
			}
		}
	}()

	return ch
}

// RunWatchUpgradePlan keeps watching the upgrade plan in the agency.
// Once it detects that this starter has to act, it does.
func (m *upgradeManager) RunWatchUpgradePlan(ctx context.Context) {
	_, _, mode := m.upgradeManagerContext.ClusterConfig()
	if !mode.HasAgency() {
		// Nothing to do here without an agency
		return
	}

	planChanges := m.waitForPlanChanges(ctx)
	for {
		var newPlan *UpgradePlan
		select {
		case newPlan = <-planChanges:
			// Plan changed
		case <-ctx.Done():
			// Context canceled
			m.log.Info().Msg("Stopping watching for plan changes: context canceled")
			return
		}
		if newPlan == nil {
			// channel was closed
			m.log.Info().Msg("Stopping watching for plan changes")
			return
		}

		plan := *newPlan
		if plan.IsReady() {
			// Plan entries have been processed
			if !plan.Finished {
				// Let's show the user that we're done
				if err := m.finishUpgradePlan(ctx, plan); err != nil {
					m.log.Error().Err(err).Msg("Failed to finish upgrade plan")
				}
			}
		} else if plan.IsFailed() {
			// Plan already failed; no action needed
		} else if len(plan.Entries) > 0 {
			// Let's inspect the first entry
			if err := m.processUpgradePlan(ctx, plan); err != nil {
				m.log.Error().Err(err).Msg("Failed to process upgrade plan entry")
			}
		}
	}
}

// processUpgradePlan inspects the first entry of the given plan and acts upon
// it when needed.
func (m *upgradeManager) processUpgradePlan(ctx context.Context, plan UpgradePlan) error {
	_, myPeer, mode := m.upgradeManagerContext.ClusterConfig()
	_, isRunning, _ := m.upgradeManagerContext.IsRunningMaster()
	if !isRunning {
		return maskAny(fmt.Errorf("not in running phase"))
	}

	// recordFailure increments the failure count in the first entry and
	// stored the modified plan.
	// It then returns the original error.
	recordFailure := func(err error) error {
		m.log.Error().Err(err).
			Str("type", string(plan.Entries[0].Type)).
			Msg("Upgrade plan entry failed")
		plan.Entries[0].Failures++
		plan.Entries[0].Reason = err.Error()
		overwrite := false
		if _, err := m.writeUpgradePlan(ctx, plan, overwrite); err != nil {
			m.log.Error().Err(err).Msg("Failed to write updated plan (recording failure)")
		}
		return maskAny(err)
	}

	firstEntry := plan.Entries[0]
	// For server entries, we only respond when the peer is ours
	if firstEntry.PeerID != myPeer.ID {
		return nil
	}
	// Prepare cleanup
	defer func() {
		m.upgradeServerType = ""
		m.updateNeeded = false
	}()

	switch firstEntry.Type {
	case UpgradeEntryTypeAgent:
		// Restart the agency in auto-upgrade mode
		m.log.Info().Msg("Upgrading agent")
		m.upgradeServerType = definitions.ServerTypeAgent
		m.updateNeeded = true
		if err := m.upgradeManagerContext.RestartServer(definitions.ServerTypeAgent); err != nil {
			return recordFailure(errors.Wrap(err, "Failed to restart agent"))
		}

		// Wait until agency restarted
		if err := m.waitUntilUpgradeServerStarted(ctx); err != nil {
			return recordFailure(errors.Wrap(err, "Agent restart in upgrade mode did not succeed"))
		}

		// Wait until agency happy again
		if err := m.waitUntil(ctx, m.isAgencyHealth, "Agency is not yet healthy: %v"); err != nil {
			return recordFailure(errors.Wrap(err, "Agency is not healthy in time"))
		}

		// Wait until cluster healthy
		if mode.IsClusterMode() {
			if err := m.waitUntil(ctx, m.isClusterHealthy, "Cluster is not yet healthy: %v"); err != nil {
				return recordFailure(errors.Wrap(err, "Cluster is not healthy in time"))
			}
		}
		m.log.Info().Msg("Finished upgrading agent")
	case UpgradeEntryTypeDBServer:
		// Restart the dbserver in auto-upgrade mode
		m.log.Info().Msg("Upgrading dbserver")
		m.upgradeServerType = definitions.ServerTypeDBServer
		m.updateNeeded = true
		upgrade := func() error {
			if firstEntry.WithoutResign {
				m.log.Info().Msg("Upgrading without resign")
			}
			if err := m.upgradeManagerContext.RestartServer(definitions.ServerTypeDBServerNoResign); err != nil {
				return recordFailure(errors.Wrap(err, "Failed to restart dbserver"))
			}

			if err := m.withMaintenance(ctx, recordFailure)(func() error {
				// Wait until dbserver restarted
				if err := m.waitUntilUpgradeServerStarted(ctx); err != nil {
					return recordFailure(errors.Wrap(err, "DBServer restart in upgrade mode did not succeed"))
				}

				// Wait until all dbservers respond
				if err := m.waitUntil(ctx, m.areDBServersResponding, "DBServers are not yet all responding: %v"); err != nil {
					return recordFailure(errors.Wrap(err, "Not all DBServers are responding in time"))
				}

				return nil
			}); err != nil {
				return err
			}

			// Wait until cluster healthy
			if err := m.waitUntil(ctx, m.isClusterHealthy, "Cluster is not yet healthy: %v"); err != nil {
				return recordFailure(errors.Wrap(err, "Cluster is not healthy in time"))
			}

			return nil
		}
		if err := upgrade(); err != nil {
			return maskAny(err)
		}
		m.log.Info().Msg("Finished upgrading dbserver")
	case UpgradeEntryTypeCoordinator:
		// Restart the coordinator in auto-upgrade mode
		m.log.Info().Msg("Upgrading coordinator")
		m.upgradeServerType = definitions.ServerTypeCoordinator
		m.updateNeeded = true
		if err := m.upgradeManagerContext.RestartServer(definitions.ServerTypeCoordinator); err != nil {
			return recordFailure(errors.Wrap(err, "Failed to restart coordinator"))
		}

		// Wait until coordinator restarted
		if err := m.waitUntilUpgradeServerStarted(ctx); err != nil {
			return recordFailure(errors.Wrap(err, "Coordinator restart in upgrade mode did not succeed"))
		}

		// Wait until all coordinators respond
		if err := m.waitUntil(ctx, m.areCoordinatorsResponding, "Coordinator are not yet all responding: %v"); err != nil {
			return recordFailure(errors.Wrap(err, "Not all Coordinators are responding in time"))
		}

		// Wait until cluster healthy
		if err := m.waitUntil(ctx, m.isClusterHealthy, "Cluster is not yet healthy: %v"); err != nil {
			return recordFailure(errors.Wrap(err, "Cluster is not healthy in time"))
		}
		m.log.Info().Msg("Finished upgrading coordinator")
	case UpgradeEntryTypeSingle:
		// Restart the single server in auto-upgrade mode
		m.log.Info().Msg("Upgrading single server")
		m.upgradeServerType = definitions.ServerTypeSingle
		m.updateNeeded = true
		upgrade := func() error {
			if err := m.upgradeManagerContext.RestartServer(definitions.ServerTypeSingle); err != nil {
				return recordFailure(errors.Wrap(err, "Failed to restart single server"))
			}

			if err := m.withMaintenance(ctx, recordFailure)(func() error {
				// Wait until single server restarted
				if err := m.waitUntilUpgradeServerStarted(ctx); err != nil {
					return recordFailure(errors.Wrap(err, "Single server restart in upgrade mode did not succeed"))
				}

				// Wait until all single servers respond
				if err := m.waitUntil(ctx, m.areSingleServersResponding, "Single server is not yet responding: %v"); err != nil {
					return recordFailure(errors.Wrap(err, "Not all single servers are responding in time"))
				}

				return nil
			}); err != nil {
				return err
			}

			return nil
		}
		if err := upgrade(); err != nil {
			return maskAny(err)
		}
		m.log.Info().Msg("Finished upgrading single server")
	default:
		return maskAny(fmt.Errorf("unsupported upgrade plan entry type '%s'", firstEntry.Type))
	}

	// Move first entry to finished entries
	plan.Entries = plan.Entries[1:]
	plan.FinishedEntries = append(plan.FinishedEntries, firstEntry)

	// Save plan
	overwrite := false
	if _, err := m.writeUpgradePlan(ctx, plan, overwrite); err != nil {
		return maskAny(err)
	}
	return nil
}

// withMaintenance wraps upgrade action with maintenance steps
func (m *upgradeManager) withMaintenance(ctx context.Context, recordFailure func(err error) error) func(func() error) error {
	return func(f func() error) error {
		m.log.Info().Msg("Disabling supervision")
		if err := m.disableSupervision(ctx); err != nil {
			return recordFailure(errors.Wrap(err, "Failed to disable supervision"))
		}

		m.log.Info().Msg("Disabled supervision")
		defer func() {
			m.log.Info().Msg("Enabling supervision")
			if err := m.enableSupervision(ctx); err != nil {
				recordFailure(errors.Wrap(err, "Failed to enable supervision"))
			}
		}()

		return f()
	}
}

// finishUpgradePlan is called at the end of the upgrade process.
// It shows the user that everything is ready & what versions we have now.
func (m *upgradeManager) finishUpgradePlan(ctx context.Context, plan UpgradePlan) error {
	isRunningAsMaster, isRunning, _ := m.upgradeManagerContext.IsRunningMaster()
	if !isRunning {
		return maskAny(fmt.Errorf("Not in running phase"))
	} else if !isRunningAsMaster {
		return nil
	}
	if _, err := m.ShowArangodServerVersions(ctx); err != nil {
		return maskAny(err)
	}

	// Save plan
	overwrite := false
	plan.Finished = true
	if _, err := m.writeUpgradePlan(ctx, plan, overwrite); err != nil {
		return maskAny(err)
	}

	// Inform user that we're done
	m.log.Info().Msg("Upgrade plan has finished successfully")

	return nil
}

// runSingleServerUpgradeProcess runs the entire upgrade process of a single server until it is finished.
func (m *upgradeManager) runSingleServerUpgradeProcess(ctx context.Context, myPeer *Peer, mode ServiceMode) {
	// Cleanup when we're done
	defer func() {
		m.upgradeServerType = ""
		m.updateNeeded = false
	}()

	if !mode.IsSingleMode() {
		m.log.Info().Msg("Not in Single Server Mode, aborting.")
		return
	}
	// Restart the single server in auto-upgrade mode
	m.log.Info().Msg("Upgrading single server")
	m.upgradeServerType = definitions.ServerTypeSingle
	m.updateNeeded = true
	if err := m.upgradeManagerContext.RestartServer(definitions.ServerTypeSingle); err != nil {
		m.log.Error().Err(err).Msg("Failed to restart single server")
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

	// We're done
	allSameVersion, err := m.ShowArangodServerVersions(ctx)
	if err != nil {
		m.log.Error().Err(err).Msg("Failed to show server versions")
	} else if allSameVersion {
		m.log.Info().Msg("Upgrading done.")
	} else {
		m.log.Info().Msg("Upgrading of all servers controlled by this starter done, you can continue with the next starter now.")
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
		m.log.Info().Msgf(errorLogTemplate, err)
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
	clients := make([]driver_http.Connection, 0, len(endpoints))
	for _, ep := range endpoints {
		c, err := m.upgradeManagerContext.CreateClient([]string{ep}, ConnectionTypeAgency, definitions.ServerTypeUnknown)
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

// isClusterHealthy performs a check on the cluster health status.
// If any of the servers is reported as not GOOD, an error is returned.
func (m *upgradeManager) isClusterHealthy(ctx context.Context) error {
	// Get cluster config
	clusterConfig, _, _ := m.upgradeManagerContext.ClusterConfig()
	// Use CreateClusterAPI to get cluster client
	clusterClient, err := clusterConfig.CreateClusterAPI(ctx, m.upgradeManagerContext)
	if err != nil {
		return maskAny(err)
	}
	// Check health using go-driver v2 API
	health, err := clusterClient.Health(ctx)
	if err != nil {
		return maskAny(err)
	}
	for id, sh := range health.Health {
		if sh.Role == driver.ServerRoleAgent && sh.Status == "" {
			continue
		}
		if sh.Status != driver.ServerStatusGood {
			return maskAny(fmt.Errorf("Server '%s' has a '%s' status", id, sh.Status))
		}
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
		c, err := m.upgradeManagerContext.CreateClient([]string{ep}, ConnectionTypeDatabase, definitions.ServerTypeUnknown)
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
		c, err := m.upgradeManagerContext.CreateClient([]string{ep}, ConnectionTypeDatabase, definitions.ServerTypeUnknown)
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
	clusterConfig, _, _ := m.upgradeManagerContext.ClusterConfig()
	// Build endpoint list
	endpoints, err := clusterConfig.GetSingleEndpoints()
	if err != nil {
		return maskAny(err)
	}
	// Check all
	for _, ep := range endpoints {
		c, err := m.upgradeManagerContext.CreateClient([]string{ep}, ConnectionTypeDatabase, definitions.ServerTypeUnknown)
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
		c, err := m.upgradeManagerContext.CreateClient([]string{ep}, ConnectionTypeAgency, definitions.ServerTypeUnknown)
		if err != nil {
			return false, maskAny(err)
		}
		info, err := c.Version(ctx)
		if err != nil {
			return false, maskAny(err)
		}
		version := info.Version
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
	supported, err := m.isSuperVisionMaintenanceSupported(ctx)
	if err != nil {
		return maskAny(err)
	}
	if !supported {
		m.log.Info().Msg("Supervision maintenance is not supported on this version")
		return nil
	}
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
			m.log.Warn().Err(err).Msg("Failed to read supervision state")
		} else if valueStr, ok := getMaintenanceMode(value); !ok {
			m.log.Warn().Msgf("Supervision state is not a string but: %v", value)
		} else if valueStr != superVisionStateMaintenance {
			m.log.Warn().Msgf("Supervision state is not yet '%s' but '%s'", superVisionStateMaintenance, valueStr)
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

func getMaintenanceMode(value interface{}) (string, bool) {
	if s, ok := value.(string); ok {
		return s, true
	}
	if m, ok := value.(map[string]interface{}); ok {
		if mode, ok := m["Mode"]; ok {
			return getMaintenanceMode(mode)
		} else if mode, ok := m["mode"]; ok {
			return getMaintenanceMode(mode)
		}
	}
	return "", false
}

// enableSupervision enabled supervision of the agency.
func (m *upgradeManager) enableSupervision(ctx context.Context) error {
	supported, err := m.isSuperVisionMaintenanceSupported(ctx)
	if err != nil {
		return maskAny(err)
	}
	if !supported {
		m.log.Info().Msg("Supervision maintenance is not supported on this version")
		return nil
	}

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

// ShowArangodServerVersions queries the versions of all Arangod servers in the cluster and shows them.
// Returns true when all servers are the same, false otherwise.
func (m *upgradeManager) ShowArangodServerVersions(ctx context.Context) (bool, error) {
	// Get cluster config
	clusterConfig, _, mode := m.upgradeManagerContext.ClusterConfig()
	versions := make(map[string]struct{})
	rows := []string{}

	showGroup := func(serverType definitions.ServerType, endpoints []string) {
		groupVersions := make([]string, len(endpoints))
		for i, ep := range endpoints {
			c, err := m.upgradeManagerContext.CreateClient([]string{ep}, ConnectionTypeDatabase, definitions.ServerTypeUnknown)
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
		showGroup(definitions.ServerTypeAgent, endpoints)
	}
	if mode.IsSingleMode() {
		endpoints, err := clusterConfig.GetSingleEndpoints()
		if err != nil {
			return false, maskAny(err)
		}
		showGroup(definitions.ServerTypeSingle, endpoints)
	}
	if mode.IsClusterMode() {
		endpoints, err := clusterConfig.GetDBServerEndpoints()
		if err != nil {
			return false, maskAny(err)
		}
		showGroup(definitions.ServerTypeDBServer, endpoints)
		endpoints, err = clusterConfig.GetCoordinatorEndpoints()
		if err != nil {
			return false, maskAny(err)
		}
		showGroup(definitions.ServerTypeCoordinator, endpoints)
	}

	m.log.Info().Msg("Server versions:")
	rows = strings.Split(columnize.SimpleFormat(rows), "\n")
	for _, r := range rows {
		m.log.Info().Msg(r)
	}

	return len(versions) == 1, nil
}

// IsSpecialUpgradeFrom3614 determines if special case for upgrade is required
func IsSpecialUpgradeFrom3614(v driver.Version) bool {
	return (v.CompareTo("3.6.0") >= 0 && v.CompareTo("3.6.14") <= 0) ||
		(v.CompareTo("3.7.0") >= 0 && v.CompareTo("3.7.12") <= 0)
}
