//
// DISCLAIMER
//
// Copyright 2021 ArangoDB GmbH, Cologne, Germany
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
// Author Adam Janikowski
//

package options

type forbidden []string

func (f forbidden) IsForbidden(key string) bool {
	for _, a := range f {
		if a == key {
			return true
		}
	}

	return false
}

var (
	// forbiddenOptions holds a list of options that are not allowed to be overridden.
	forbiddenOptions = forbidden{
		// Arangod
		"agency.activate",
		"agency.endpoint",
		"agency.my-address",
		"agency.size",
		"agency.supervision",
		"cluster.agency-endpoint",
		"cluster.my-address",
		"cluster.my-role",
		"database.directory",
		"javascript.startup-directory",
		"javascript.app-path",
		"log.file",
		"rocksdb.encryption-keyfile",
		"server.endpoint",
		"server.authentication",
		"server.jwt-secret",
		"server.storage-engine",
		"ssl.cafile",
		"ssl.keyfile",
		// ArangoSync
		"cluster.endpoint",
		"master.endpoint",
		"server.endpoint",
	}
)
