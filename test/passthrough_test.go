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
	"os"
	"regexp"
	"testing"
	"time"
)

// TestPassthroughConflict runs `arangodb --starter.local --all.ssl.keyfile=foo`
func TestPassthroughConflict(t *testing.T) {
	needTestMode(t, testModeProcess)
	dataDir := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDir)

	child := Spawn(t, "${STARTER} --starter.local --all.ssl.keyfile=foo")
	defer child.Close()

	expr := regexp.MustCompile("is essential to the starters behavior and cannot be overwritten")
	if err := child.ExpectTimeout(time.Second*15, expr); err != nil {
		t.Errorf("Expected errors message, got %#v", err)
	}
}
