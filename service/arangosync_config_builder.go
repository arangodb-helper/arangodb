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
	"path/filepath"
	"strings"

	logging "github.com/op/go-logging"
)

// createArangoSyncArgs returns the command line arguments needed to run an arangosync server of given type.
func createArangoSyncArgs(log *logging.Logger, config Config, clusterConfig ClusterConfig, myContainerDir string,
	myPeerID, myAddress, myPort string, serverType ServerType) []string {

	options := make([]optionPair, 0, 32)
	executable := config.ArangoSyncPath
	args := []string{
		executable,
	}

	options = append(options,
		optionPair{"--log.file", slasher(filepath.Join(myContainerDir, logFileName))},
	)
	if config.DebugCluster {
		options = append(options,
			optionPair{"--log.level", "debug"})
	}
	//	scheme := NewURLSchemes(clusterConfig.IsSecure()).Arangod
	//	myTCPURL := scheme + "://" + net.JoinHostPort(myAddress, myPort)
	switch serverType {
	case ServerTypeSyncWorker:
		// TODO
	}

	for _, opt := range options {
		ptValues := config.passthroughOptionValuesForServerType(strings.TrimPrefix(opt.Key, "--"), serverType)
		if len(ptValues) > 0 {
			log.Warningf("Pass through option %s conflicts with automatically generated option with value '%s'", opt.Key, opt.Value)
		} else {
			args = append(args, opt.Key, opt.Value)
		}
	}
	for _, ptOpt := range config.PassthroughOptions {
		values := ptOpt.valueForServerType(serverType)
		if len(values) == 0 {
			continue
		}
		// Append all values
		for _, value := range values {
			args = append(args, ptOpt.FormattedOptionName(), value)
		}
	}
	return args
}
