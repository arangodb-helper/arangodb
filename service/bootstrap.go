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

// BootstrapConfig holds all configuration for a service that will
// not change through the lifetime of a cluster.
type BootstrapConfig struct {
	ID                       string      // Unique identifier of this peer
	Mode                     ServiceMode // Service mode cluster|single
	StartLocalSlaves         bool        // If set, start sufficient slave (Service's) locally.
	ServerStorageEngine      string      // mmfiles | rocksdb
	JwtSecret                string      // JWT secret used for arangod communication
	SslKeyFile               string      // Path containing an x509 certificate + private key to be used by the servers.
	SslCAFile                string      // Path containing an x509 CA certificate used to authenticate clients.
	RocksDBEncryptionKeyFile string      // Path containing encryption key for RocksDB encryption.
}

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
