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

package test

import (
	"testing"
)

func removeArangodProcesses(t *testing.T) {
	c := SpawnWithExpand(t, "sh -c 'PIDS=$(pidof -x arangod); if [ ! -z \"${PIDS}\" ]; then kill -9 ${PIDS}; fi'", false)
	defer c.Close()
	c.Wait()
}

func closeProcess(t *testing.T, s *SubProcess, name string) {
	s.Close()

	showProcessLogs(t, s, name)
}

func listArangodProcesses(t *testing.T, log Logger) {
	c := SpawnWithExpand(t, "pidof -x arangod", false)
	defer c.Close()
	c.Wait()

	logProcessOutput(log, c, "Processes: ")
}

func showProcessLogs(t *testing.T, s *SubProcess, name string) {
	if !t.Failed() {
		return
	}

	log := GetLogger(t)

	logProcessOutput(log, s, "Log of process: %s", name)
}
