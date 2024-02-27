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
	driver "github.com/arangodb/go-driver"

	"github.com/arangodb-helper/arangodb/pkg/features"
)

// DatabaseFeatures provides information about the features provided by the
// database in a given version.
type DatabaseFeatures struct {
	Version    driver.Version
	Enterprise bool
}

const (
	v32    driver.Version = "3.2.0"
	v33_20 driver.Version = "3.3.20"
	v33_22 driver.Version = "3.3.22"
	v34    driver.Version = "3.4.0"
	v34_2  driver.Version = "3.4.2"
	v312   driver.Version = "3.12.0"
)

// NewDatabaseFeatures returns a new DatabaseFeatures based on
// the given version.
func NewDatabaseFeatures(version driver.Version, enterprise bool) DatabaseFeatures {
	return DatabaseFeatures{
		Version:    version,
		Enterprise: enterprise,
	}
}

// HasStorageEngineOption returns true when `server.storage-engine`
// option is supported.
func (v DatabaseFeatures) HasStorageEngineOption() bool {
	return v.Version.CompareTo(v32) >= 0
}

// DefaultStorageEngine returns the default storage engine (mmfiles|rocksdb).
func (v DatabaseFeatures) DefaultStorageEngine() string {
	if v.Version.CompareTo(v34) >= 0 {
		return "rocksdb"
	}
	return "mmfiles"
}

// HasCopyInstallationFiles does server support copying installation files
func (v DatabaseFeatures) HasCopyInstallationFiles() bool {
	if v.Version.CompareTo(v34) >= 0 {
		return true
	}
	if v.Version.CompareTo(v33_20) >= 0 {
		return true
	}
	return false
}

// HasJWTSecretFileOption does the server support passing jwt secret by file
func (v DatabaseFeatures) HasJWTSecretFileOption() bool {
	if v.Version.CompareTo(v33_22) >= 0 && v.Version.CompareTo(v34) < 0 {
		return true
	}
	if v.Version.CompareTo(v34_2) >= 0 {
		return true
	}
	return false
}

func (v DatabaseFeatures) GetJWTFolderOption() bool {
	return features.JWTRotation().Enabled(features.Version{
		Version:    v.Version,
		Enterprise: v.Enterprise,
	})
}

// SupportsActiveFailover returns true if Active-Failover (resilient-single) mode is supported
func (v DatabaseFeatures) SupportsActiveFailover() bool {
	return v.Version.CompareTo(v312) < 0
}

func (v DatabaseFeatures) SupportsArangoSync() bool {
	return v.Version.CompareTo(v312) < 0
}
