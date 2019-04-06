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
	"path/filepath"
	"strings"

	"github.com/rs/zerolog"
)

// createArangoClusterSecretFile creates an arangod.jwtsecret file in the given host directory if it does not yet exists.
// The arangod.jwtsecret file contains the JWT secret used to authenticate with the local cluster.
func createArangoClusterSecretFile(log zerolog.Logger, bsCfg BootstrapConfig, myHostDir, myContainerDir string, serverType ServerType) ([]Volume, string, error) {
	// Is there a secret set?
	if bsCfg.JwtSecret == "" {
		return nil, "", nil
	}

	// Yes there is a secret
	hostSecretFileName := filepath.Join(myHostDir, arangodJWTSecretFileName)
	containerSecretFileName := filepath.Join(myContainerDir, arangodJWTSecretFileName)
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

// createArangoSyncArgs returns the command line arguments needed to run an arangosync server of given type.
func createArangoSyncArgs(log zerolog.Logger, config Config, clusterConfig ClusterConfig, myContainerDir, myContainerLogFile string,
	myPeerID, myAddress, myPort string, serverType ServerType, clusterJWTSecretFile string) ([]string, error) {

	options := make([]optionPair, 0, 32)
	executable := config.ArangoSyncPath
	args := []string{
		executable,
	}

	options = append(options,
		optionPair{"--log.file", myContainerLogFile},
		optionPair{"--server.endpoint", "https://" + net.JoinHostPort(myAddress, myPort)},
		optionPair{"--server.port", myPort},
		optionPair{"--monitoring.token", config.SyncMonitoringToken},
		optionPair{"--master.jwt-secret", config.SyncMasterJWTSecretFile},
	)
	if config.DebugCluster {
		options = append(options,
			optionPair{"--log.level", "debug"})
	}
	switch serverType {
	case ServerTypeSyncMaster:
		args = append(args, "run", "master")
		options = append(options,
			optionPair{"--server.keyfile", config.SyncMasterKeyFile},
			optionPair{"--server.client-cafile", config.SyncMasterClientCAFile},
			optionPair{"--mq.type", config.SyncMQType},
		)
		if clusterJWTSecretFile != "" {
			options = append(options,
				optionPair{"--cluster.jwt-secret", clusterJWTSecretFile},
			)
		}
		if clusterEPs, err := clusterConfig.GetCoordinatorEndpoints(); err == nil {
			if len(clusterEPs) == 0 {
				return nil, maskAny(fmt.Errorf("No cluster coordinators found"))
			}
			for _, ep := range clusterEPs {
				options = append(options,
					optionPair{"--cluster.endpoint", ep})
			}
		} else {
			log.Error().Err(err).Msg("Cannot find coordinator endpoints")
			return nil, maskAny(err)
		}
	case ServerTypeSyncWorker:
		args = append(args, "run", "worker")
		if syncMasterEPs, err := clusterConfig.GetSyncMasterEndpoints(); err == nil {
			if len(syncMasterEPs) == 0 {
				return nil, maskAny(fmt.Errorf("No sync masters found"))
			}
			for _, ep := range syncMasterEPs {
				options = append(options,
					optionPair{"--master.endpoint", ep})
			}
		} else {
			log.Error().Err(err).Msg("Cannot find sync master endpoints")
			return nil, maskAny(err)
		}
	}

	for _, opt := range options {
		ptValues := config.passthroughOptionValuesForServerType(strings.TrimPrefix(opt.Key, "--"), serverType)
		if len(ptValues) > 0 {
			log.Warn().Msgf("Pass through option %s conflicts with automatically generated option with value '%s'", opt.Key, opt.Value)
		} else {
			args = append(args, opt.Key+"="+opt.Value)
		}
	}
	for _, ptOpt := range config.PassthroughOptions {
		values := ptOpt.valueForServerType(serverType)
		if len(values) == 0 {
			continue
		}
		// Append all values
		for _, value := range values {
			if isOptionSuitableForArangoSyncServer(serverType, ptOpt.Name) {
				args = append(args, ptOpt.FormattedOptionName()+"="+value)
			}
		}
	}
	return args, nil
}

func isOptionSuitableForArangoSyncServer(serverType ServerType, optionName string) bool {
	switch serverType {
	case ServerTypeSyncMaster:
	case ServerTypeSyncWorker:
		if strings.HasPrefix(optionName, "mq.") || strings.HasPrefix(optionName, "cluster.") {
			return false
		}
	}
	return true
}
