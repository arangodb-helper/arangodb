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
// Author Adam Janikowski
//

package definitions

const (
	PortOffsetUnknown      = 0 // Coordinator/single server
	PortOffsetCoordinator  = 1 // Coordinator/single server
	PortOffsetDBServer     = 2
	PortOffsetAgent        = 3
	PortOffsetSyncMaster   = 4
	PortOffsetSyncWorker   = 5
	PortOffsetIncrementOld = 5  // {our http server, agent, coordinator, dbserver, reserved}
	PortOffsetIncrementNew = 10 // {our http server, agent, coordinator, dbserver, syncmaster, syncworker, reserved...}
)

const (
	MinRecentFailuresForLog = 2   // Number of recent failures needed before a log file is shown.
	MaxRecentFailures       = 100 // Maximum number of recent failures before the starter gives up.
)

const (
	ArangodConfFileName        = "arangod.conf"
	ArangodJWTSecretFileName   = "arangod.jwtsecret"
	ArangodJWTSecretFolderName = "jwt"
	ArangodJWTSecretActive     = "-"
)
