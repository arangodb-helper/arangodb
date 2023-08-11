//
// DISCLAIMER
//
// Copyright 2017-2022 ArangoDB GmbH, Cologne, Germany
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

package main

import (
	"fmt"
	"time"

	"github.com/arangodb-helper/arangodb/pkg/definitions"
	"github.com/arangodb-helper/arangodb/service/options"
)

type starterOptions struct {
	cluster struct {
		advertisedEndpoint  string
		agencySize          int
		startAgent          []bool
		startDBServer       []bool
		startCoordinator    []bool
		startActiveFailover []bool
	}
	server struct {
		useLocalBin    bool
		arangodPath    string
		arangodJSPath  string
		arangoSyncPath string
		rrPath         string
		threads        int
		storageEngine  string
	}
	log struct {
		dir               string // Custom log directory (default "")
		verbose           bool
		color             bool
		console           bool
		file              bool
		timeFormat        string
		rotateFilesToKeep int
		rotateInterval    time.Duration
	}
	starter struct {
		id                   string
		masterAddresses      []string
		masterPort           int
		ownAddress           string
		startLocalSlaves     bool
		mode                 string
		dataDir              string
		bindAddress          string
		allPortOffsetsUnique bool
		disableIPv6          bool
		debugCluster         bool
		enableSync           bool
		instanceUpTimeout    time.Duration
	}
	auth struct {
		jwtSecretFile string
	}
	ssl struct {
		keyFile          string
		autoKeyFile      bool
		autoServerName   string
		autoOrganization string
		caFile           string
	}
	rocksDB struct {
		encryptionKeyFile      string
		encryptionKeyGenerator string
	}
	docker struct {
		endpoint        string
		arangodImage    string
		arangoSyncImage string
		imagePullPolicy string
		user            string
		containerName   string
		gcDelay         time.Duration
		netHost         bool // Deprecated
		networkMode     string
		forceBindPorts  bool
		privileged      bool
		tty             bool
	}
	sync struct {
		startSyncMaster []bool
		startSyncWorker []bool
		monitoring      struct {
			token string
		}
		master struct {
			jwtSecretFile string // File containing JWT secret used to access the Sync Master (from Sync Worker)
		}
		mq struct {
			Type string // MQ type used to Sync Master
		}
		server struct {
			keyFile      string // TLS keyfile of local sync master
			clientCAFile string // CA Certificate used for client certificate verification
		}
	}
}

func preparePassthroughPrefixes() options.ConfigurationPrefixes {
	return options.ConfigurationPrefixes{
		// Old methods
		"all": {
			Usage: func(key string) string {
				return fmt.Sprintf("Passed through to all server instances as --%s", key)
			},
			FieldSelector: func(p *options.Configuration, key string) *[]string {
				return p.ArgByProcessTypeAndName(definitions.ServerTypeAgent, key)
			},
			DeprecatedHintFormat: "use --args.all.%s instead",
		},
		"coordinators": {
			Usage: func(key string) string {
				return fmt.Sprintf("Passed through to all coordinator instances as --%s", key)
			},
			FieldSelector: func(p *options.Configuration, key string) *[]string {
				return p.ArgByServerTypeAndName(definitions.ServerTypeCoordinator, key)
			},
			DeprecatedHintFormat: "use --args.coordinators.%s instead",
		},
		"dbservers": {
			Usage: func(key string) string {
				return fmt.Sprintf("Passed through to all dbserver instances as --%s", key)
			},
			FieldSelector: func(p *options.Configuration, key string) *[]string {
				return p.ArgByServerTypeAndName(definitions.ServerTypeDBServer, key)
			},
			DeprecatedHintFormat: "use --args.dbservers.%s instead",
		},
		"agents": {
			Usage: func(key string) string {
				return fmt.Sprintf("Passed through to all agent instances as --%s", key)
			},
			FieldSelector: func(p *options.Configuration, key string) *[]string {
				return p.ArgByServerTypeAndName(definitions.ServerTypeAgent, key)
			},
			DeprecatedHintFormat: "use --args.agents.%s instead",
		},
		"sync": {
			Usage: func(key string) string {
				return fmt.Sprintf("Passed through to all sync instances as --%s", key)
			},
			FieldSelector: func(p *options.Configuration, key string) *[]string {
				return p.ArgByProcessTypeAndName(definitions.ServerTypeSyncMaster, key)
			},
			DeprecatedHintFormat: "use --args.sync.%s instead",
		},
		"syncmasters": {
			Usage: func(key string) string {
				return fmt.Sprintf("Passed through to all sync master instances as --%s", key)
			},
			FieldSelector: func(p *options.Configuration, key string) *[]string {
				return p.ArgByServerTypeAndName(definitions.ServerTypeSyncMaster, key)
			},
			DeprecatedHintFormat: "use --args.syncmasters.%s instead",
		},
		"syncworkers": {
			Usage: func(key string) string {
				return fmt.Sprintf("Passed through to all sync workers instances as --%s", key)
			},
			FieldSelector: func(p *options.Configuration, key string) *[]string {
				return p.ArgByServerTypeAndName(definitions.ServerTypeSyncWorker, key)
			},
			DeprecatedHintFormat: "use --args.syncworkers.%s instead",
		},
		// New methods for args
		"args.all": {
			Usage: func(key string) string {
				return fmt.Sprintf("Passed through to all server instances as --%s", key)
			},
			FieldSelector: func(p *options.Configuration, key string) *[]string {
				return p.ArgByProcessTypeAndName(definitions.ServerTypeAgent, key)
			},
		},
		"args.coordinators": {
			Usage: func(key string) string {
				return fmt.Sprintf("Passed through to all coordinator instances as --%s", key)
			},
			FieldSelector: func(p *options.Configuration, key string) *[]string {
				return p.ArgByServerTypeAndName(definitions.ServerTypeCoordinator, key)
			},
		},
		"args.dbservers": {
			Usage: func(key string) string {
				return fmt.Sprintf("Passed through to all dbserver instances as --%s", key)
			},
			FieldSelector: func(p *options.Configuration, key string) *[]string {
				return p.ArgByServerTypeAndName(definitions.ServerTypeDBServer, key)
			},
		},
		"args.agents": {
			Usage: func(key string) string {
				return fmt.Sprintf("Passed through to all agent instances as --%s", key)
			},
			FieldSelector: func(p *options.Configuration, key string) *[]string {
				return p.ArgByServerTypeAndName(definitions.ServerTypeAgent, key)
			},
		},
		"args.sync": {
			Usage: func(key string) string {
				return fmt.Sprintf("Passed through to all sync instances as --%s", key)
			},
			FieldSelector: func(p *options.Configuration, key string) *[]string {
				return p.ArgByProcessTypeAndName(definitions.ServerTypeSyncMaster, key)
			},
		},
		"args.syncmasters": {
			Usage: func(key string) string {
				return fmt.Sprintf("Passed through to all sync master instances as --%s", key)
			},
			FieldSelector: func(p *options.Configuration, key string) *[]string {
				return p.ArgByServerTypeAndName(definitions.ServerTypeSyncMaster, key)
			},
		},
		"args.syncworkers": {
			Usage: func(key string) string {
				return fmt.Sprintf("Passed through to all sync workers instances as --%s", key)
			},
			FieldSelector: func(p *options.Configuration, key string) *[]string {
				return p.ArgByServerTypeAndName(definitions.ServerTypeSyncWorker, key)
			},
		},
		// New methods for envs
		"envs.all": {
			Usage: func(key string) string {
				return fmt.Sprintf("Env passed to all server instances as %s", key)
			},
			FieldSelector: func(p *options.Configuration, key string) *[]string {
				return p.EnvByProcessTypeAndName(definitions.ServerTypeAgent, key)
			},
		},
		"envs.coordinators": {
			Usage: func(key string) string {
				return fmt.Sprintf("Env passed to all coordinator instances as %s", key)
			},
			FieldSelector: func(p *options.Configuration, key string) *[]string {
				return p.EnvByServerTypeAndName(definitions.ServerTypeCoordinator, key)
			},
		},
		"envs.dbservers": {
			Usage: func(key string) string {
				return fmt.Sprintf("Env passed to all dbserver instances as %s", key)
			},
			FieldSelector: func(p *options.Configuration, key string) *[]string {
				return p.EnvByServerTypeAndName(definitions.ServerTypeDBServer, key)
			},
		},
		"envs.agents": {
			Usage: func(key string) string {
				return fmt.Sprintf("Env passed to all agent instances as %s", key)
			},
			FieldSelector: func(p *options.Configuration, key string) *[]string {
				return p.EnvByServerTypeAndName(definitions.ServerTypeAgent, key)
			},
		},
		"envs.sync": {
			Usage: func(key string) string {
				return fmt.Sprintf("Env passed to all sync instances as %s", key)
			},
			FieldSelector: func(p *options.Configuration, key string) *[]string {
				return p.EnvByProcessTypeAndName(definitions.ServerTypeSyncMaster, key)
			},
		},
		"envs.syncmasters": {
			Usage: func(key string) string {
				return fmt.Sprintf("Env passed to all sync master instances as %s", key)
			},
			FieldSelector: func(p *options.Configuration, key string) *[]string {
				return p.EnvByServerTypeAndName(definitions.ServerTypeSyncMaster, key)
			},
		},
		"envs.syncworkers": {
			Usage: func(key string) string {
				return fmt.Sprintf("Env passed to all sync master instances as %s", key)
			},
			FieldSelector: func(p *options.Configuration, key string) *[]string {
				return p.EnvByServerTypeAndName(definitions.ServerTypeSyncWorker, key)
			},
		},
	}
}
