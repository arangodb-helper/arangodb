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
	"os"
	"path/filepath"
	"testing"

	driver "github.com/arangodb/go-driver"
	"github.com/stretchr/testify/require"

	"github.com/arangodb-helper/arangodb/pkg/definitions"
)

func TestReadActualStorageEngineReadsEngineFile(t *testing.T) {
	s := newTestSingleService(t, "3.12.10")
	hostDir, err := s.serverHostDir(definitions.ServerTypeSingle)
	require.NoError(t, err)
	engineDir := filepath.Join(hostDir, "data")
	require.NoError(t, os.MkdirAll(engineDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(engineDir, "ENGINE"), []byte("rocksdb\n"), 0644))

	engine, err := s.readActualStorageEngine()

	require.NoError(t, err)
	require.Equal(t, "rocksdb", engine)
}

func TestReadActualStorageEngineDefaultsWhenEngineFileIsMissingForNewerVersion(t *testing.T) {
	s := newTestSingleService(t, "3.12.10")
	hostDir, err := s.serverHostDir(definitions.ServerTypeSingle)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(hostDir, "data"), 0755))

	engine, err := s.readActualStorageEngine()

	require.NoError(t, err)
	require.Equal(t, "rocksdb", engine)
}

func TestReadActualStorageEngineRequiresDataDirForNewerVersion(t *testing.T) {
	s := newTestSingleService(t, "3.12.10")

	_, err := s.readActualStorageEngine()

	require.Error(t, err)
}

func TestReadActualStorageEngineRequiresEngineFileForOlderVersion(t *testing.T) {
	s := newTestSingleService(t, "3.12.9")
	hostDir, err := s.serverHostDir(definitions.ServerTypeSingle)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(hostDir, "data"), 0755))

	_, err = s.readActualStorageEngine()

	require.Error(t, err)
}

func newTestSingleService(t *testing.T, version string) *Service {
	t.Helper()

	const id = "starter"
	dataDir := t.TempDir()
	s := &Service{
		cfg:              Config{DataDir: dataDir},
		id:               id,
		mode:             ServiceModeSingle,
		databaseFeatures: NewDatabaseFeatures(driver.Version(version), false, true),
	}
	s.runtimeClusterManager.myPeers = ClusterConfig{
		AllPeers: []Peer{
			newPeer(id, "127.0.0.1", 8528, 0, dataDir, preparePeerServers(ServiceModeSingle, BootstrapConfig{}, nil), false),
		},
	}
	return s
}
