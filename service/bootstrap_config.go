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
	"crypto/tls"
	"path"

	"github.com/arangodb-helper/arangodb/pkg/definitions"
)

// BootstrapConfig holds all configuration for a service that will
// not change through the lifetime of a cluster.
type BootstrapConfig struct {
	ID                            string      // Unique identifier of this peer
	Mode                          ServiceMode // Service mode cluster|single
	DataDir                       string
	AgencySize                    int    // Number of agents in the agency
	StartLocalSlaves              bool   // If set, start sufficient slave (Service's) locally.
	StartAgent                    *bool  // If not nil, sets if starter starts a agent, otherwise default handling applies
	StartDBserver                 *bool  // If not nil, sets if starter starts a dbserver, otherwise default handling applies
	StartCoordinator              *bool  // If not nil, sets if starter starts a coordinator, otherwise default handling applies
	ServerStorageEngine           string // mmfiles | rocksdb
	JwtSecret                     string // JWT secret used for arangod communication
	ArangosyncMonitoringToken     string // Bearer token used for arangosync authentication
	SslKeyFile                    string // Path containing an x509 certificate + private key to be used by the servers.
	SslCAFile                     string // Path containing an x509 CA certificate used to authenticate clients.
	RocksDBEncryptionKeyFile      string // Path containing encryption key for RocksDB encryption.
	RocksDBEncryptionKeyGenerator string // Path to program. The output of this program will be used as key for RocksDB encryption.
	DisableIPv6                   bool   // If set, no IPv6 notation will be used
	RecoveryAgentID               string `json:"-"` // ID of the agent. Only set during recovery
}

func (bsCfg *BootstrapConfig) JWTFolderDir() string {
	return path.Join(bsCfg.DataDir, definitions.ArangodJWTSecretFolderName)
}

func (bsCfg *BootstrapConfig) JWTFolderDirFile(f string) string {
	return path.Join(bsCfg.DataDir, definitions.ArangodJWTSecretFolderName, f)
}

// Initialize auto-configures some optional values
func (bsCfg *BootstrapConfig) Initialize() error {
	// Create unique ID
	if bsCfg.ID == "" {
		var err error
		bsCfg.ID, err = createUniqueID()
		if err != nil {
			return maskAny(err)
		}
	}
	return nil
}

// CreateTLSConfig creates a TLS config based on given bootstrap config
func (bsCfg *BootstrapConfig) CreateTLSConfig() (*tls.Config, error) {
	if bsCfg.SslKeyFile == "" {
		return nil, nil
	}
	cert, err := LoadKeyFile(bsCfg.SslKeyFile)
	if err != nil {
		return nil, maskAny(err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
	}, nil
}

// PeersNeeded returns the minimum number of peers needed for the given config.
func (bsCfg *BootstrapConfig) PeersNeeded() int {
	minServers := 1
	switch {
	case bsCfg.Mode.IsClusterMode():
		minServers = 3
	case bsCfg.Mode.IsSingleMode():
		minServers = 1
	}
	if minServers < bsCfg.AgencySize {
		minServers = bsCfg.AgencySize
	}
	return minServers
}

// LoadFromSetupConfig loads important values from setup config file
func (bsCfg *BootstrapConfig) LoadFromSetupConfig(cfg SetupConfigFile) {
	// Reload data from config
	bsCfg.ID = cfg.ID
	if cfg.Mode != "" {
		bsCfg.Mode = cfg.Mode
	}
	bsCfg.StartLocalSlaves = cfg.StartLocalSlaves
	if cfg.SslKeyFile != "" {
		bsCfg.SslKeyFile = cfg.SslKeyFile
	}
	if cfg.JwtSecret != "" {
		bsCfg.JwtSecret = cfg.JwtSecret
	}
	bsCfg.AgencySize = cfg.Peers.AgencySize
}
