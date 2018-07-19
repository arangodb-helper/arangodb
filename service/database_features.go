//
// DISCLAIMER
//
// Copyright 2018 ArangoDB GmbH, Cologne, Germany
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

import driver "github.com/arangodb/go-driver"

// DatabaseFeatures provides information about the features provided by the
// database in a given version.
type DatabaseFeatures driver.Version

const (
	v32 driver.Version = "3.2.0"
	v34 driver.Version = "3.4.0"
)

// NewDatabaseFeatures returns a new DatabaseFeatures based on
// the given version.
func NewDatabaseFeatures(version driver.Version) DatabaseFeatures {
	return DatabaseFeatures(version)
}

// HasStorageEngineOption returns true when `server.storage-engine`
// option is supported.
func (v DatabaseFeatures) HasStorageEngineOption() bool {
	return driver.Version(v).CompareTo(v32) >= 0
}

// DefaultStorageEngine returns the default storage engine (mmfiles|rocksdb).
func (v DatabaseFeatures) DefaultStorageEngine() string {
	if driver.Version(v).CompareTo(v34) >= 0 {
		return "rocksdb"
	}
	return "mmfiles"
}
