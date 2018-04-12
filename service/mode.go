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

type ServiceMode string

const (
	ServiceModeCluster_        = ServiceMode("cluster")
	ServiceModeSingle_         = ServiceMode("single")
	ServiceModeActiveFailover_ = ServiceMode("activefailover")
)

// IsClusterMode returns true when the service is running in cluster mode.
func (m ServiceMode) IsClusterMode() bool {
	return m == "cluster"
}

// IsSingleMode returns true when the service is running in single server mode.
func (m ServiceMode) IsSingleMode() bool {
	return m == "single"
}

// IsActiveFailoverMode returns true when the service is running in activefailover server mode.
func (m ServiceMode) IsActiveFailoverMode() bool {
	return m == "activefailover" || m == "resilientsingle" /* keep as alias */
}

// SupportsArangoSync returns true when the given mode support running arangosync on it.
func (m ServiceMode) SupportsArangoSync() bool {
	return m.IsClusterMode()
}

// SupportsRecovery returns true when the given mode support recovering from permanent failed machines.
func (m ServiceMode) SupportsRecovery() bool {
	return m == "" || m.IsClusterMode() || m.IsActiveFailoverMode()
}
