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

//
// Arangod options are configured in the following places:
// - arangod.conf:
//     This holds all settings that are considered static for the lifetime of the cluster.
//     Using new/different settings on the Starter will not change these settings.
// - arangod commandline:
//     This holds all settings that can change over the lifetime of the cluster.
//     These settings mostly involve the cluster layout.
//     Using new/different settings on the Starter will change these settings.
//     Passthrough options are always added here.
//

import (
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"path"
	"path/filepath"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"

	"github.com/arangodb-helper/arangodb/pkg/definitions"
	"github.com/arangodb-helper/arangodb/service/options"
)

// createArangoClusterSecretFile creates an arangod.jwtsecret file in the given host directory if it does not yet exists.
// The arangod.jwtsecret file contains the JWT secret used to authenticate with the local cluster.
func createArangoClusterSecretFile(log zerolog.Logger, bsCfg BootstrapConfig, myHostDir, myContainerDir string, serverType definitions.ServerType, features DatabaseFeatures) ([]Volume, string, error) {

	// Is there a secret set?
	if bsCfg.JwtSecret != "" {
		if features.GetJWTFolderOption() {
			// Yes there is a secret
			hostSecretFolderName := filepath.Join(myHostDir, definitions.ArangodJWTSecretFolderName)
			containerSecretFolderName := filepath.Join(myContainerDir, definitions.ArangodJWTSecretFolderName)
			volumes := addVolume(nil, hostSecretFolderName, containerSecretFolderName, true)

			files, err := ioutil.ReadDir(hostSecretFolderName)
			if err != nil {
				return nil, "", err
			}

			files = FilterFiles(files, FilterOnlyFiles)

			if len(files) != 0 {
				return volumes, containerSecretFolderName, nil
			}

			if files, err := ioutil.ReadDir(hostSecretFolderName); err != nil {
				return nil, "", err
			} else {
				// Cleanup old files
				for _, f := range files {
					if err := os.Remove(filepath.Join(hostSecretFolderName, f.Name())); err != nil {
						return nil, "", err
					}
				}
			}

			files, err = ioutil.ReadDir(bsCfg.JWTFolderDir())
			if err != nil {
				return nil, "", err
			}

			files = FilterFiles(files, FilterOnlyFiles)

			if len(files) == 0 {
				return nil, "", errors.Errorf("Unable to find any usable file")
			}

			for _, f := range files {
				h, err := ioutil.ReadFile(filepath.Join(bsCfg.JWTFolderDir(), f.Name()))
				if err != nil {
					return nil, "", err
				}

				if err := ioutil.WriteFile(filepath.Join(hostSecretFolderName, Sha256sum(h)), h, 0600); err != nil {
					return nil, "", err
				}
			}
			h, err := ioutil.ReadFile(filepath.Join(bsCfg.JWTFolderDir(), files[0].Name()))
			if err != nil {
				return nil, "", err
			}

			if err := ioutil.WriteFile(filepath.Join(hostSecretFolderName, definitions.ArangodJWTSecretActive), h, 0600); err != nil {
				return nil, "", err
			}

			return volumes, containerSecretFolderName, nil
		} else {

			// Yes there is a secret
			hostSecretFileName := filepath.Join(myHostDir, definitions.ArangodJWTSecretFileName)
			containerSecretFileName := filepath.Join(myContainerDir, definitions.ArangodJWTSecretFileName)
			volumes := addVolume(nil, hostSecretFileName, containerSecretFileName, true)

			if _, err := os.Stat(hostSecretFileName); err == nil {
				// Arangod.jwtsecret already exists
				return volumes, containerSecretFileName, nil
			}

			// Create arangod.jwtsecret file now
			if err := ioutil.WriteFile(hostSecretFileName, []byte(bsCfg.JwtSecret), 0600); err != nil {
				return nil, "", maskAny(err)
			}
			return volumes, containerSecretFileName, nil
		}
	}
	return nil, "", nil
}

// createArangoSyncArgs returns the command line arguments needed to run an arangosync server of given type.
func createArangoSyncArgs(log zerolog.Logger, config Config, clusterConfig ClusterConfig, myContainerDir, myContainerLogFile string,
	myPeerID, myAddress, myPort string, serverType definitions.ServerType, clusterJWTSecretFile string, features DatabaseFeatures) ([]string, error) {

	opts := make([]optionPair, 0, 32)
	executable := config.ArangoSyncPath
	args := []string{
		executable,
	}

	opts = append(opts,
		optionPair{"--log.file", myContainerLogFile},
		optionPair{"--server.endpoint", "https://" + net.JoinHostPort(myAddress, myPort)},
		optionPair{"--server.port", myPort},
		optionPair{"--monitoring.token", config.SyncMonitoringToken},
		optionPair{"--master.jwt-secret", config.SyncMasterJWTSecretFile},
		optionPair{"--pid-file", getLockFilePath(myContainerDir)},
	)
	if config.DebugCluster {
		opts = append(opts,
			optionPair{"--log.level", "debug"})
	}
	switch serverType {
	case definitions.ServerTypeSyncMaster:
		args = append(args, "run", "master")
		opts = append(opts,
			optionPair{"--server.keyfile", config.SyncMasterKeyFile},
			optionPair{"--server.client-cafile", config.SyncMasterClientCAFile},
			optionPair{"--mq.type", config.SyncMQType},
		)
		if clusterJWTSecretFile != "" {
			jwtSecretPath := clusterJWTSecretFile
			if features.GetJWTFolderOption() {
				jwtSecretPath = path.Join(clusterJWTSecretFile, definitions.ArangodJWTSecretActive)
			}
			opts = append(opts, optionPair{"--cluster.jwt-secret", jwtSecretPath})
		}
		if clusterEPs, err := clusterConfig.GetCoordinatorEndpoints(); err == nil {
			if len(clusterEPs) == 0 {
				return nil, maskAny(fmt.Errorf("No cluster coordinators found"))
			}
			for _, ep := range clusterEPs {
				opts = append(opts,
					optionPair{"--cluster.endpoint", ep})
			}
		} else {
			log.Error().Err(err).Msg("Cannot find coordinator endpoints")
			return nil, maskAny(err)
		}
	case definitions.ServerTypeSyncWorker:
		args = append(args, "run", "worker")
		if syncMasterEPs, err := clusterConfig.GetSyncMasterEndpoints(); err == nil {
			if len(syncMasterEPs) == 0 {
				return nil, maskAny(fmt.Errorf("No sync masters found"))
			}
			for _, ep := range syncMasterEPs {
				opts = append(opts, optionPair{"--master.endpoint", ep})
			}
		} else {
			log.Error().Err(err).Msg("Cannot find sync master endpoints")
			return nil, maskAny(err)
		}
	}

	passArgs := config.Configuration.ArgsForServerType(serverType)

	for _, opt := range opts {
		if _, ok := passArgs[opt.Key]; ok {
			log.Warn().Msgf("Pass through option %s conflicts with automatically generated option with value '%s'", opt.Key, opt.Value)
		} else {
			args = append(args, opt.Key, opt.Value)
		}
	}
	for key, values := range passArgs {
		if len(values) == 0 {
			continue
		}

		// Append all values
		for _, value := range values {
			args = append(args, options.FormattedOptionName(key), value)
		}
	}

	return args, nil
}
