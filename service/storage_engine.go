//
// DISCLAIMER
//
// Copyright 2018-2024 ArangoDB GmbH, Cologne, Germany
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
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/arangodb-helper/arangodb/pkg/definitions"
)

// validateStorageEngine checks if the given storage engine is a valid one.
// Empty is still allowed.
func (s *Service) validateStorageEngine(storageEngine string, features DatabaseFeatures) error {
	switch storageEngine {
	case "":
		// Not set yet. We'll choose one later
		return nil
	case "mmfiles":
		// Always OK
		return nil
	case "rocksdb":
		if !features.HasStorageEngineOption() {
			return maskAny(fmt.Errorf("RocksDB storage engine is not support for this database version"))
		}
		return nil
	default:
		return maskAny(fmt.Errorf("Unknown storage engine '%s'", storageEngine))
	}
}

// readActualStorageEngine reads the actually used storage engine from
// the database directory.
func (s *Service) readActualStorageEngine() (string, error) {
	features := s.DatabaseFeatures()
	if !features.HasStorageEngineOption() {
		// ENGINE file does not exist
		return features.DefaultStorageEngine(), nil
	}

	_, peer, mode := s.ClusterConfig()
	var serverType definitions.ServerType
	if mode.IsClusterMode() {
		// Read engine from dbserver data directory
		if peer.HasDBServer() {
			serverType = definitions.ServerTypeDBServer
		} else if peer.HasAgent() {
			serverType = definitions.ServerTypeAgent
		} else {
			// In case of Coordinator return default storage engine
			return features.DefaultStorageEngine(), nil
		}
	} else {
		// Read engine from single server data directory
		serverType = definitions.ServerTypeSingle
	}
	// Get directory
	dataDir, err := s.serverHostDir(serverType)
	if err != nil {
		return "", maskAny(err)
	}
	// Read ENGINE file
	engine, err := os.ReadFile(filepath.Join(dataDir, "data", "ENGINE"))
	if err != nil {
		return "", maskAny(err)
	}
	storageEngine := strings.ToLower(strings.TrimSpace(string(engine)))
	return storageEngine, nil
}
