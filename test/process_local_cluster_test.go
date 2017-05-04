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

// +build localprocess

package test

import (
	"fmt"
	"os"
	"testing"
)

// TestProcessLocalCluster runs `arangodb --local`
func TestProcessLocalCluster(t *testing.T) {
	dataDir := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDir)
	child, err := Spawn("${STARTER} --local")
	if err != nil {
		t.Fatalf("Failed to launch arangodb: %s", describe(err))
	}
	defer child.Close()

	fmt.Println("Waiting for cluster ready")
	if ok := WaitUntilStarterReady(t, child); ok {
		testCluster(t, "http://localhost:4000")
		testCluster(t, "http://localhost:4005")
		testCluster(t, "http://localhost:4010")
	}

	fmt.Println("Waiting for termination")
	StopStarter(t, child)
}
