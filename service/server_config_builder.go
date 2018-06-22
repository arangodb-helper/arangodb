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
	"io/ioutil"
	"os"
	"runtime"
	"strings"

	"github.com/rs/zerolog"
)

type optionPair struct {
	Key   string
	Value string
}

// collectServerConfigVolumes collects all files from the given config file for which a volume needs to be mapped.
func collectServerConfigVolumes(serverType ServerType, config configFile) []Volume {
	var result []Volume

	addVolumeForSetting := func(sectionName, key string) {
		if section := config.FindSection(sectionName); section != nil {
			if path, ok := section.Settings[key]; ok {
				result = addVolume(result, path, path, true)
			}
		}
	}

	switch serverType.ProcessType() {
	case ProcessTypeArangod:
		addVolumeForSetting("ssl", "keyfile")
		addVolumeForSetting("ssl", "cafile")
		addVolumeForSetting("rocksdb", "encryption-keyfile")
	case ProcessTypeArangoSync:
		// TODO
	}

	return result
}

// createServerArgs returns the command line arguments needed to run an arangod/arangosync server of given type.
func createServerArgs(log zerolog.Logger, config Config, clusterConfig ClusterConfig, myContainerDir, myContainerLogFile string,
	myPeerID, myAddress, myPort string, serverType ServerType, arangodConfig configFile,
	clusterJWTSecretFile, agentRecoveryID string, databaseAutoUpgrade bool) ([]string, error) {
	switch serverType.ProcessType() {
	case ProcessTypeArangod:
		return createArangodArgs(log, config, clusterConfig, myContainerDir, myContainerLogFile, myPeerID, myAddress, myPort, serverType, arangodConfig, agentRecoveryID, databaseAutoUpgrade), nil
	case ProcessTypeArangoSync:
		return createArangoSyncArgs(log, config, clusterConfig, myContainerDir, myContainerLogFile, myPeerID, myAddress, myPort, serverType, clusterJWTSecretFile)
	default:
		return nil, nil
	}
}

// writeCommand writes the command used to start a server in a file with given path.
func writeCommand(log zerolog.Logger, filename string, executable string, args []string) {
	content := strings.Join(args, " \\\n") + "\n"
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		if err := ioutil.WriteFile(filename, []byte(content), 0755); err != nil {
			log.Error().Err(err).Msgf("Failed to write command to %s", filename)
		}
	}
}

// addVolume extends the list of volumes with given host+container pair if running on linux.
func addVolume(configVolumes []Volume, hostPath, containerPath string, readOnly bool) []Volume {
	if runtime.GOOS == "linux" {
		return []Volume{
			Volume{
				HostPath:      hostPath,
				ContainerPath: containerPath,
				ReadOnly:      readOnly,
			},
		}
	}
	return configVolumes
}
