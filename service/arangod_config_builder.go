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
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rs/zerolog"

	"github.com/arangodb-helper/arangodb/pkg/definitions"
	"github.com/arangodb-helper/arangodb/service/options"
)

var (
	urlFixer = strings.NewReplacer(
		"http://", "tcp://",
		"https://", "ssl://",
	)
)

// fixupEndpointURLSchemeForArangod changes endpoint URL schemes used by Starter to ones used by arangodb.
// E.g. "http://localhost:8529" -> "tcp://localhost:8529"
func fixupEndpointURLSchemeForArangod(u string) string {
	return urlFixer.Replace(u)
}

// createArangodConf creates an arangod.conf file in the given host directory if it does not yet exists.
// The arangod.conf file contains all settings that are considered static for the lifetime of the server.
func createArangodConf(log zerolog.Logger, bsCfg BootstrapConfig, myHostDir, myContainerDir, myPort string, serverType definitions.ServerType, features DatabaseFeatures) ([]Volume, configFile, error) {
	hostConfFileName := filepath.Join(myHostDir, definitions.ArangodConfFileName)
	containerConfFileName := filepath.Join(myContainerDir, definitions.ArangodConfFileName)
	volumes := addVolume(nil, hostConfFileName, containerConfFileName, true)

	if _, err := os.Stat(hostConfFileName); err == nil {
		// Arangod.conf already exists
		// Read config file
		if cfg, err := readConfigFile(hostConfFileName); err != nil {
			return nil, nil, maskAny(err)
		} else {
			return volumes, cfg, nil
		}
	}

	// Arangod.conf does not exist. Create it.
	logLevel := "INFO"
	listenAddr := "[::]"
	if bsCfg.DisableIPv6 {
		listenAddr = "0.0.0.0"
	}
	scheme := NewURLSchemes(bsCfg.SslKeyFile != "").Arangod
	serverSection := &configSection{
		Name: "server",
		Settings: map[string]string{
			"endpoint":       fmt.Sprintf("%s://%s:%s", scheme, listenAddr, myPort),
			"authentication": "false",
		},
	}
	if bsCfg.JwtSecret != "" {
		serverSection.Settings["authentication"] = "true"
		// otherwise pass the file name by argument
		if !features.HasJWTSecretFileOption() {
			serverSection.Settings["jwt-secret"] = bsCfg.JwtSecret
		}
	}
	if features.HasStorageEngineOption() {
		serverSection.Settings["storage-engine"] = bsCfg.ServerStorageEngine
	}
	config := configFile{
		serverSection,
		&configSection{
			Name: "log",
			Settings: map[string]string{
				"level": logLevel,
			},
		},
	}
	if bsCfg.SslKeyFile != "" {
		sslSection := &configSection{
			Name: "ssl",
			Settings: map[string]string{
				"keyfile": bsCfg.SslKeyFile,
			},
		}
		if bsCfg.SslCAFile != "" {
			sslSection.Settings["cafile"] = bsCfg.SslCAFile
		}
		config = append(config, sslSection)
	}
	if bsCfg.RocksDBEncryptionKeyFile != "" {
		rocksdbSection := &configSection{
			Name: "rocksdb",
			Settings: map[string]string{
				"encryption-keyfile": bsCfg.RocksDBEncryptionKeyFile,
			},
		}
		config = append(config, rocksdbSection)
	}

	out, err := os.Create(hostConfFileName)
	if err != nil {
		log.Fatal().Err(err).Msgf("Could not create configuration file %s", hostConfFileName)
		return nil, nil, maskAny(err)
	}
	defer out.Close()
	if _, err := config.WriteTo(out); err != nil {
		log.Fatal().Err(err).Msg("Cannot create config file")
		return nil, nil, maskAny(err)
	}

	return volumes, config, nil
}

// createArangodArgs returns the command line arguments needed to run an arangod server of given type.
func createArangodArgs(log zerolog.Logger, config Config, clusterConfig ClusterConfig, myContainerDir, myContainerLogFile string,
	myPeerID, myAddress, myPort string, serverType definitions.ServerType, arangodConfig configFile, agentRecoveryID string, databaseAutoUpgrade bool, clusterJWTSecretFile string,
	features DatabaseFeatures) []string {
	containerConfFileName := filepath.Join(myContainerDir, definitions.ArangodConfFileName)

	args := make([]string, 0, 40)
	opts := make([]optionPair, 0, 32)
	executable := config.ArangodPath
	jsStartup := config.ArangodJSPath
	if config.RrPath != "" {
		args = append(args, config.RrPath)
	}
	args = append(args,
		executable,
		"-c", slasher(containerConfFileName),
	)

	opts = append(opts,
		optionPair{"--database.directory", slasher(filepath.Join(myContainerDir, "data"))},
		optionPair{"--javascript.startup-directory", slasher(jsStartup)},
		optionPair{"--javascript.app-path", slasher(filepath.Join(myContainerDir, "apps"))},
		optionPair{"--log.file", slasher(myContainerLogFile)},
		optionPair{"--log.force-direct", "false"},
	)
	if clusterJWTSecretFile != "" {
		if !features.GetJWTFolderOption() {
			opts = append(opts,
				optionPair{"--server.jwt-secret-keyfile", clusterJWTSecretFile},
			)
		} else {
			opts = append(opts,
				optionPair{"--server.jwt-secret-folder", clusterJWTSecretFile},
			)
		}
	}
	if !config.RunningInDocker && features.HasCopyInstallationFiles() {
		opts = append(opts, optionPair{"--javascript.copy-installation", "true"})
	}

	if databaseAutoUpgrade {
		opts = append(opts,
			optionPair{"--database.auto-upgrade", "true"})
	}
	if config.ServerThreads != 0 {
		opts = append(opts,
			optionPair{"--server.threads", strconv.Itoa(config.ServerThreads)})
	}
	if config.DebugCluster {
		opts = append(opts,
			optionPair{"--log.level", "startup=trace"})
	}
	scheme := NewURLSchemes(clusterConfig.IsSecure()).Arangod
	myTCPURL := scheme + "://" + net.JoinHostPort(myAddress, myPort)
	switch serverType {
	case definitions.ServerTypeAgent:
		opts = append(opts,
			optionPair{"--agency.activate", "true"},
			optionPair{"--agency.my-address", myTCPURL},
			optionPair{"--agency.size", strconv.Itoa(clusterConfig.AgencySize)},
			optionPair{"--agency.supervision", "true"},
			optionPair{"--foxx.queues", "false"},
			optionPair{"--server.statistics", "false"},
		)
		for _, p := range clusterConfig.AllAgents() {
			if p.ID != myPeerID {
				opts = append(opts,
					optionPair{"--agency.endpoint", fmt.Sprintf("%s://%s", scheme, net.JoinHostPort(p.Address, strconv.Itoa(p.Port+p.PortOffset+definitions.PortOffsetAgent)))},
				)
			}
		}
		if agentRecoveryID != "" {
			opts = append(opts,
				optionPair{"--agency.disaster-recovery-id", agentRecoveryID},
			)
		}
	case definitions.ServerTypeDBServer:
		opts = append(opts,
			optionPair{"--cluster.my-address", myTCPURL},
			optionPair{"--cluster.my-role", "PRIMARY"},
			optionPair{"--foxx.queues", "false"},
			optionPair{"--server.statistics", "true"},
		)
	case definitions.ServerTypeCoordinator:
		opts = append(opts,
			optionPair{"--cluster.my-address", myTCPURL},
			optionPair{"--cluster.my-role", "COORDINATOR"},
			optionPair{"--foxx.queues", "true"},
			optionPair{"--server.statistics", "true"},
		)
	case definitions.ServerTypeSingle:
		opts = append(opts,
			optionPair{"--foxx.queues", "true"},
			optionPair{"--server.statistics", "true"},
		)
	case definitions.ServerTypeResilientSingle:
		opts = append(opts,
			optionPair{"--foxx.queues", "true"},
			optionPair{"--server.statistics", "true"},
			optionPair{"--replication.automatic-failover", "true"},
			optionPair{"--cluster.my-address", myTCPURL},
			optionPair{"--cluster.my-role", "SINGLE"},
		)
	}
	if serverType == definitions.ServerTypeCoordinator || serverType == definitions.ServerTypeResilientSingle {
		if config.AdvertisedEndpoint != "" {
			opts = append(opts,
				optionPair{"--cluster.my-advertised-endpoint", fixupEndpointURLSchemeForArangod(config.AdvertisedEndpoint)},
			)
		}
	}
	if serverType != definitions.ServerTypeAgent && serverType != definitions.ServerTypeSingle {
		for _, p := range clusterConfig.AllAgents() {
			opts = append(opts,
				optionPair{"--cluster.agency-endpoint",
					fmt.Sprintf("%s://%s", scheme, net.JoinHostPort(p.Address, strconv.Itoa(p.Port+p.PortOffset+definitions.PortOffsetAgent)))},
			)
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

		// Look for overrides of configuration sections
		if section := arangodConfig.FindSection(options.SectionName(key)); section != nil {
			if confValue, found := section.Settings[options.SectionName(key)]; found {
				log.Warn().Msgf("Pass through option %s overrides generated configuration option with value '%s'", key, confValue)
			}
		}
		// Append all values
		for _, value := range values {
			args = append(args, options.FormattedOptionName(key), value)
		}
	}
	return args
}
