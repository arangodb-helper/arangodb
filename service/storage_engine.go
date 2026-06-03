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

func (s *Service) relaunchStorageEngine(clusterConfig ClusterConfig) string {
	if clusterConfig.ServerStorageEngine != "" {
		return clusterConfig.ServerStorageEngine
	}
	return s.DatabaseFeatures().DefaultStorageEngine()
}
