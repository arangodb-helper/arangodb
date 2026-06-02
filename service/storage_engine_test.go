//
// DISCLAIMER
//
// Copyright 2026 ArangoDB GmbH, Cologne, Germany
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
	"testing"

	driver "github.com/arangodb/go-driver/v2/arangodb"
	"github.com/stretchr/testify/require"
)

func TestRelaunchStorageEngineUsesClusterConfigValue(t *testing.T) {
	s := newTestServiceWithDatabaseVersion("4.0.0")

	engine := s.relaunchStorageEngine(ClusterConfig{ServerStorageEngine: "rocksdb"})
	t.Logf("engine: %s", engine)
	require.Equal(t, "rocksdb", engine)
}

func TestRelaunchStorageEngineDefaultsWhenClusterConfigValueIsMissing(t *testing.T) {
	s := newTestServiceWithDatabaseVersion("4.0.0")

	engine := s.relaunchStorageEngine(ClusterConfig{})
	t.Logf("engine: %s", engine)
	require.Equal(t, "rocksdb", engine)
}

func newTestServiceWithDatabaseVersion(version string) *Service {
	return &Service{
		databaseFeatures: NewDatabaseFeatures(driver.Version(version), false, true),
	}
}
