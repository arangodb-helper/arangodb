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

package definitions

import "fmt"

// ProcessType specifies the types of process (the starter can work with).
type ProcessType string

const (
	ProcessTypeArangod    ProcessType = "arangod"
	ProcessTypeArangoSync ProcessType = "arangosync"
)

// String returns a string representation of the given ProcessType.
func (s ProcessType) String() string {
	return string(s)
}

// CommandFileName returns the name of a file containing the full command for processes
// of this type.
func (s ProcessType) CommandFileName() string {
	switch s {
	case ProcessTypeArangod:
		return "arangod_command.txt"
	case ProcessTypeArangoSync:
		return "arangosync_command.txt"
	default:
		return ""
	}
}

// LogFileName returns the name of the log file used by this process
func (s ProcessType) LogFileName(suffix string) string {
	switch s {
	case ProcessTypeArangod:
		return fmt.Sprintf("arangod%s.log", suffix)
	case ProcessTypeArangoSync:
		return fmt.Sprintf("arangosync%s.log", suffix)
	default:
		return ""
	}
}
