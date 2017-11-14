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

import "fmt"

// ServerType specifies the types of database servers.
type ServerType string

const (
	ServerTypeCoordinator     = "coordinator"
	ServerTypeDBServer        = "dbserver"
	ServerTypeAgent           = "agent"
	ServerTypeSingle          = "single"
	ServerTypeResilientSingle = "resilientsingle"
	ServerTypeSyncMaster      = "syncmaster"
	ServerTypeSyncWorker      = "syncworker"
)

// String returns a string representation of the given ServerType.
func (s ServerType) String() string {
	return string(s)
}

// PortOffset returns the offset from a peer base port for the given type of server.
func (s ServerType) PortOffset() int {
	switch s {
	case ServerTypeCoordinator, ServerTypeSingle, ServerTypeResilientSingle:
		return _portOffsetCoordinator
	case ServerTypeDBServer:
		return _portOffsetDBServer
	case ServerTypeAgent:
		return _portOffsetAgent
	case ServerTypeSyncMaster:
		return _portOffsetSyncMaster
	case ServerTypeSyncWorker:
		return _portOffsetSyncWorker
	default:
		panic(fmt.Sprintf("Unknown ServerType: %s", string(s)))
	}
}

// ProcessType returns the type of process needed to run a server of given type.
func (s ServerType) ProcessType() ProcessType {
	switch s {
	case ServerTypeSyncMaster, ServerTypeSyncWorker:
		return ProcessTypeArangoSync
	default:
		return ProcessTypeArangod
	}
}

// ExpectedServerRole returns the expected `role` & `mode` value resulting from a request to `_admin/server/role`.
func (s ServerType) ExpectedServerRole() (string, string) {
	switch s {
	case ServerTypeCoordinator:
		return "COORDINATOR", ""
	case ServerTypeSingle:
		return "SINGLE", ""
	case ServerTypeResilientSingle:
		return "SINGLE", "resilient"
	case ServerTypeDBServer:
		return "PRIMARY", ""
	case ServerTypeAgent:
		return "AGENT", ""
	case ServerTypeSyncMaster, ServerTypeSyncWorker:
		return "", ""
	default:
		panic(fmt.Sprintf("Unknown ServerType: %s", string(s)))
	}
}
