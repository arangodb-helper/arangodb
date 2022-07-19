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

package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/dchest/uniuri"
	homedir "github.com/mitchellh/go-homedir"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	driver "github.com/arangodb/go-driver"

	_ "github.com/arangodb-helper/arangodb/client"
	"github.com/arangodb-helper/arangodb/pkg/definitions"
	"github.com/arangodb-helper/arangodb/pkg/features"
	"github.com/arangodb-helper/arangodb/pkg/logging"
	"github.com/arangodb-helper/arangodb/pkg/net"
	"github.com/arangodb-helper/arangodb/pkg/terminal"
	service "github.com/arangodb-helper/arangodb/service"
	"github.com/arangodb-helper/arangodb/service/options"
)

// Configuration data with defaults:

const (
	projectName                     = "arangodb"
	logFileName                     = projectName + ".log"
	defaultDockerGCDelay            = time.Minute * 10
	defaultDockerStarterImage       = "arangodb/arangodb-starter"
	defaultArangodPath              = "/usr/sbin/arangod"
	defaultArangoSyncPath           = "/usr/sbin/arangosync"
	defaultLogRotateFilesToKeep     = 5
	defaultLogRotateInterval        = time.Minute * 60 * 24
	defaultInstanceUpTimeoutLinux   = time.Second * 300
	defaultInstanceUpTimeoutWindows = time.Second * 900
)

var (
	defaultInstanceUpTimeout = defaultInstanceUpTimeoutLinux
	projectVersion           = "dev"
	projectBuild             = "dev"
	cmdMain                  = &cobra.Command{
		Use:   projectName,
		Short: "Start ArangoDB clusters & single servers with ease",
		Run:   cmdMainRun,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			// Load commandline options from envvars
			setFlagValuesFromEnv(cmd.Flags())
			// Show version if requested
			cmdShowVersionRun(cmd, args)
		},
	}
	cmdVersion = &cobra.Command{
		Use:   "version",
		Short: "Show ArangoDB version",
		Run:   cmdShowVersionRun,
	}
	log                 zerolog.Logger
	logService          logging.Service
	showVersion         bool
	id                  string
	advertisedEndpoint  string
	agencySize          int
	arangodPath         string
	arangodJSPath       string
	arangoSyncPath      string
	masterPort          int
	rrPath              string
	startAgent          []bool
	startDBserver       []bool
	startCoordinator    []bool
	startActiveFailover []bool
	startSyncMaster     []bool
	startSyncWorker     []bool
	startLocalSlaves    bool
	mode                string
	dataDir             string
	logDir              string // Custom log directory (default "")
	logOutput           struct {
		Color      bool
		Console    bool
		File       bool
		TimeFormat string
	}
	ownAddress               string
	bindAddress              string
	masterAddresses          []string
	verbose                  bool
	serverThreads            int
	serverStorageEngine      string
	allPortOffsetsUnique     bool
	jwtSecretFile            string
	sslKeyFile               string
	sslAutoKeyFile           bool
	sslAutoServerName        string
	sslAutoOrganization      string
	sslCAFile                string
	rocksDBEncryptionKeyFile string
	disableIPv6              bool
	logRotateFilesToKeep     int
	logRotateInterval        time.Duration
	dockerEndpoint           string
	dockerArangodImage       string
	dockerArangoSyncImage    string
	dockerImagePullPolicy    string
	dockerStarterImage       = defaultDockerStarterImage
	dockerUser               string
	dockerContainerName      string
	dockerGCDelay            time.Duration
	dockerNetHost            bool // Deprecated
	dockerNetworkMode        string
	dockerPrivileged         bool
	dockerTTY                bool
	debugCluster             bool
	enableSync               bool
	instanceUpTimeout        time.Duration
	syncMonitoringToken      string
	syncMasterKeyFile        string // TLS keyfile of local sync master
	syncMasterClientCAFile   string // CA Certificate used for client certificate verification
	syncMasterJWTSecretFile  string // File containing JWT secret used to access the Sync Master (from Sync Worker)
	syncMQType               string // MQ type used to Sync Master

	configuration *options.Configuration

	maskAny = errors.WithStack
)

func init() {
	// Setup error functions in go-driver
	driver.WithStack = errors.WithStack
	driver.Cause = errors.Cause

	// Prepare initial logger
	log, _ = logging.NewRootLogger(logging.LoggerOutputOptions{
		Stderr: true,
	})

	defaultLogColor := true
	if !terminal.IsTerminal() {
		// We're not running on a terminal, avoid colorizing logs
		defaultLogColor = false
	}

	if runtime.GOOS == "windows" {
		defaultInstanceUpTimeout = defaultInstanceUpTimeoutWindows
	}

	// Prepare commandline parser
	cmdMain.AddCommand(cmdVersion)

	pf := cmdMain.PersistentFlags()
	f := cmdMain.Flags()

	pf.BoolVar(&showVersion, "version", false, "If set, show version and exit")

	f.StringSliceVar(&masterAddresses, "starter.join", nil, "join a cluster with master at given address")
	f.StringVar(&mode, "starter.mode", "cluster", "Set the mode of operation to use (cluster|single|activefailover)")
	f.BoolVar(&startLocalSlaves, "starter.local", false, "If set, local slaves will be started to create a machine local (test) cluster")
	f.StringVar(&ownAddress, "starter.address", "", "address under which this server is reachable, needed for running in docker or in single mode")
	f.StringVar(&bindAddress, "starter.host", "0.0.0.0", "address used to bind the starter to")
	f.StringVar(&id, "starter.id", "", "Unique identifier of this peer")
	f.IntVar(&masterPort, "starter.port", service.DefaultMasterPort, "Port to listen on for other arangodb's to join")
	f.BoolVar(&allPortOffsetsUnique, "starter.unique-port-offsets", false, "If set, all peers will get a unique port offset. If false (default) only portOffset+peerAddress pairs will be unique.")
	f.StringVar(&dataDir, "starter.data-dir", getEnvVar("DATA_DIR", "."), "directory to store all data the starter generates (and holds actual database directories)")
	f.BoolVar(&debugCluster, "starter.debug-cluster", getEnvVar("DEBUG_CLUSTER", "") != "", "If set, log more information to debug a cluster")
	f.BoolVar(&disableIPv6, "starter.disable-ipv6", !net.IsIPv6Supported(), "If set, no IPv6 notation will be used. Use this only when IPv6 address family is disabled")
	f.BoolVar(&enableSync, "starter.sync", false, "If set, the starter will also start arangosync instances")
	f.DurationVar(&instanceUpTimeout, "starter.instance-up-timeout", defaultInstanceUpTimeout, "Timeout to wait for an instance start")
	if err := features.JWTRotation().Register(f); err != nil {
		panic(err)
	}

	pf.BoolVar(&verbose, "log.verbose", false, "Turn on debug logging")
	pf.BoolVar(&logOutput.Console, "log.console", true, "Send log output to console")
	pf.BoolVar(&logOutput.File, "log.file", true, "Send log output to file")
	pf.BoolVar(&logOutput.Color, "log.color", defaultLogColor, "Colorize the log output")
	pf.StringVar(&logOutput.TimeFormat, "log.time-format", "local-datestring",
		"Time format to use in logs. Possible values: 'local-datestring' (default), 'utc-datestring'")
	pf.StringVar(&logDir, "log.dir", getEnvVar("LOG_DIR", ""), "Custom log file directory.")
	f.IntVar(&logRotateFilesToKeep, "log.rotate-files-to-keep", defaultLogRotateFilesToKeep, "Number of files to keep when rotating log files")
	f.DurationVar(&logRotateInterval, "log.rotate-interval", defaultLogRotateInterval, "Time between log rotations (0 disables log rotation)")
	f.StringVar(&advertisedEndpoint, "cluster.advertised-endpoint", "", "An external endpoint for the servers started by this Starter")
	f.IntVar(&agencySize, "cluster.agency-size", 3, "Number of agents in the cluster")
	f.BoolSliceVar(&startAgent, "cluster.start-agent", nil, "should an agent instance be started")
	f.BoolSliceVar(&startDBserver, "cluster.start-dbserver", nil, "should a dbserver instance be started")
	f.BoolSliceVar(&startCoordinator, "cluster.start-coordinator", nil, "should a coordinator instance be started")
	f.BoolSliceVar(&startActiveFailover, "cluster.start-single", nil, "should an active-failover single server instance be started")

	f.StringVar(&arangodPath, "server.arangod", defaultArangodPath, "Path of arangod")
	f.StringVar(&arangoSyncPath, "server.arangosync", defaultArangoSyncPath, "Path of arangosync")
	f.StringVar(&arangodJSPath, "server.js-dir", "/usr/share/arangodb3/js", "Path of arango JS folder")
	f.StringVar(&rrPath, "server.rr", "", "Path of rr")
	f.IntVar(&serverThreads, "server.threads", 0, "Adjust server.threads of each server")
	f.StringVar(&serverStorageEngine, "server.storage-engine", "", "Type of storage engine to use (mmfiles|rocksdb) (3.2 and up)")
	f.StringVar(&rocksDBEncryptionKeyFile, "rocksdb.encryption-keyfile", "", "Key file used for RocksDB encryption. (Enterprise Edition 3.2 and up)")

	f.StringVar(&dockerEndpoint, "docker.endpoint", "unix:///var/run/docker.sock", "Endpoint used to reach the docker daemon")
	f.StringVar(&dockerArangodImage, "docker.image", getEnvVar("DOCKER_IMAGE", ""), "name of the Docker image to use to launch arangod instances (leave empty to avoid using docker)")
	f.StringVar(&dockerArangoSyncImage, "docker.sync-image", getEnvVar("DOCKER_ARANGOSYNC_IMAGE", ""), "name of the Docker image to use to launch arangosync instances")
	f.StringVar(&dockerImagePullPolicy, "docker.imagePullPolicy", "", "pull docker image from docker hub (Always|IfNotPresent|Never)")
	f.StringVar(&dockerUser, "docker.user", "", "use the given name as user to run the Docker container")
	f.StringVar(&dockerContainerName, "docker.container", "", "name of the docker container that is running this process")
	f.DurationVar(&dockerGCDelay, "docker.gc-delay", defaultDockerGCDelay, "Delay before stopped containers are garbage collected")
	f.BoolVar(&dockerNetHost, "docker.net-host", false, "Run containers with --net=host")
	f.Lookup("docker.net-host").Deprecated = "use --docker.net-mode=host instead"
	f.StringVar(&dockerNetworkMode, "docker.net-mode", "", "Run containers with --net=<value>")
	f.BoolVar(&dockerPrivileged, "docker.privileged", false, "Run containers with --privileged")
	f.BoolVar(&dockerTTY, "docker.tty", true, "Run containers with TTY enabled")

	f.StringVar(&jwtSecretFile, "auth.jwt-secret", "", "name of a plain text file containing a JWT secret used for server authentication")

	f.StringVar(&sslKeyFile, "ssl.keyfile", "", "path of a PEM encoded file containing a server certificate + private key")
	f.StringVar(&sslCAFile, "ssl.cafile", "", "path of a PEM encoded file containing a CA certificate used for client authentication")
	f.BoolVar(&sslAutoKeyFile, "ssl.auto-key", false, "If set, a self-signed certificate will be created and used as --ssl.keyfile")
	f.StringVar(&sslAutoServerName, "ssl.auto-server-name", "", "Server name put into self-signed certificate. See --ssl.auto-key")
	f.StringVar(&sslAutoOrganization, "ssl.auto-organization", "ArangoDB", "Organization name put into self-signed certificate. See --ssl.auto-key")

	f.BoolSliceVar(&startSyncMaster, "sync.start-master", nil, "should an ArangoSync master instance be started (only relevant when starter.sync is enabled)")
	f.BoolSliceVar(&startSyncWorker, "sync.start-worker", nil, "should an ArangoSync worker instance be started (only relevant when starter.sync is enabled)")
	f.StringVar(&syncMonitoringToken, "sync.monitoring.token", "", "Bearer token used to access ArangoSync monitoring endpoints")
	f.StringVar(&syncMasterJWTSecretFile, "sync.master.jwt-secret", "", "File containing JWT secret used to access the Sync Master (from Sync Worker)")
	f.StringVar(&syncMQType, "sync.mq.type", "direct", "Type of message queue used by the Sync Master")
	f.StringVar(&syncMasterKeyFile, "sync.server.keyfile", "", "TLS keyfile of local sync master")
	f.StringVar(&syncMasterClientCAFile, "sync.server.client-cafile", "", "CA Certificate used for client certificate verification")

	cmdMain.Flags().SetNormalizeFunc(normalizeOptionNames)

	passthroughtPrefixesNew := options.ConfigurationPrefixes{
		// Old methods
		"all": {
			Usage: func(arg, key string) string {
				return fmt.Sprintf("Passed through to all server instances as --%s", key)
			},
			FieldSelector: func(p *options.Configuration, key string) *[]string {
				return p.ArgByProcessTypeAndName(definitions.ServerTypeAgent, key)
			},
			Deprecated: true,
		},
		"coordinators": {
			Usage: func(arg, key string) string {
				return fmt.Sprintf("Passed through to all coordinator instances as --%s", key)
			},
			FieldSelector: func(p *options.Configuration, key string) *[]string {
				return p.ArgByServerTypeAndName(definitions.ServerTypeCoordinator, key)
			},
			Deprecated: true,
		},
		"dbservers": {
			Usage: func(arg, key string) string {
				return fmt.Sprintf("Passed through to all dbserver instances as --%s", key)
			},
			FieldSelector: func(p *options.Configuration, key string) *[]string {
				return p.ArgByServerTypeAndName(definitions.ServerTypeDBServer, key)
			},
			Deprecated: true,
		},
		"agents": {
			Usage: func(arg, key string) string {
				return fmt.Sprintf("Passed through to all agent instances as --%s", key)
			},
			FieldSelector: func(p *options.Configuration, key string) *[]string {
				return p.ArgByServerTypeAndName(definitions.ServerTypeAgent, key)
			},
			Deprecated: true,
		},
		"sync": {
			Usage: func(arg, key string) string {
				return fmt.Sprintf("Passed through to all sync instances as --%s", key)
			},
			FieldSelector: func(p *options.Configuration, key string) *[]string {
				return p.ArgByProcessTypeAndName(definitions.ServerTypeSyncMaster, key)
			},
			Deprecated: true,
		},
		"syncmasters": {
			Usage: func(arg, key string) string {
				return fmt.Sprintf("Passed through to all sync master instances as --%s", key)
			},
			FieldSelector: func(p *options.Configuration, key string) *[]string {
				return p.ArgByServerTypeAndName(definitions.ServerTypeSyncMaster, key)
			},
			Deprecated: true,
		},
		"syncworkers": {
			Usage: func(arg, key string) string {
				return fmt.Sprintf("Passed through to all sync master instances as --%s", key)
			},
			FieldSelector: func(p *options.Configuration, key string) *[]string {
				return p.ArgByServerTypeAndName(definitions.ServerTypeSyncWorker, key)
			},
			Deprecated: true,
		},
		// New methods for args
		"args.all": {
			Usage: func(arg, key string) string {
				return fmt.Sprintf("Passed through to all server instances as --%s", key)
			},
			FieldSelector: func(p *options.Configuration, key string) *[]string {
				return p.ArgByProcessTypeAndName(definitions.ServerTypeAgent, key)
			},
		},
		"args.coordinators": {
			Usage: func(arg, key string) string {
				return fmt.Sprintf("Passed through to all coordinator instances as --%s", key)
			},
			FieldSelector: func(p *options.Configuration, key string) *[]string {
				return p.ArgByServerTypeAndName(definitions.ServerTypeCoordinator, key)
			},
		},
		"args.dbservers": {
			Usage: func(arg, key string) string {
				return fmt.Sprintf("Passed through to all dbserver instances as --%s", key)
			},
			FieldSelector: func(p *options.Configuration, key string) *[]string {
				return p.ArgByServerTypeAndName(definitions.ServerTypeDBServer, key)
			},
		},
		"args.agents": {
			Usage: func(arg, key string) string {
				return fmt.Sprintf("Passed through to all agent instances as --%s", key)
			},
			FieldSelector: func(p *options.Configuration, key string) *[]string {
				return p.ArgByServerTypeAndName(definitions.ServerTypeAgent, key)
			},
		},
		"args.sync": {
			Usage: func(arg, key string) string {
				return fmt.Sprintf("Passed through to all sync instances as --%s", key)
			},
			FieldSelector: func(p *options.Configuration, key string) *[]string {
				return p.ArgByProcessTypeAndName(definitions.ServerTypeSyncMaster, key)
			},
		},
		"args.syncmasters": {
			Usage: func(arg, key string) string {
				return fmt.Sprintf("Passed through to all sync master instances as --%s", key)
			},
			FieldSelector: func(p *options.Configuration, key string) *[]string {
				return p.ArgByServerTypeAndName(definitions.ServerTypeSyncMaster, key)
			},
		},
		"args.syncworkers": {
			Usage: func(arg, key string) string {
				return fmt.Sprintf("Passed through to all sync master instances as --%s", key)
			},
			FieldSelector: func(p *options.Configuration, key string) *[]string {
				return p.ArgByServerTypeAndName(definitions.ServerTypeSyncWorker, key)
			},
		},
		// New methods for args
		"envs.all": {
			Usage: func(arg, key string) string {
				return fmt.Sprintf("Env passed to all server instances as %s", key)
			},
			FieldSelector: func(p *options.Configuration, key string) *[]string {
				return p.EnvByProcessTypeAndName(definitions.ServerTypeAgent, key)
			},
		},
		"envs.coordinators": {
			Usage: func(arg, key string) string {
				return fmt.Sprintf("Env passed to all coordinator instances as %s", key)
			},
			FieldSelector: func(p *options.Configuration, key string) *[]string {
				return p.EnvByServerTypeAndName(definitions.ServerTypeCoordinator, key)
			},
		},
		"envs.dbservers": {
			Usage: func(arg, key string) string {
				return fmt.Sprintf("Env passed to all dbserver instances as %s", key)
			},
			FieldSelector: func(p *options.Configuration, key string) *[]string {
				return p.EnvByServerTypeAndName(definitions.ServerTypeDBServer, key)
			},
		},
		"envs.agents": {
			Usage: func(arg, key string) string {
				return fmt.Sprintf("Env passed to all agent instances as %s", key)
			},
			FieldSelector: func(p *options.Configuration, key string) *[]string {
				return p.EnvByServerTypeAndName(definitions.ServerTypeAgent, key)
			},
		},
		"envs.sync": {
			Usage: func(arg, key string) string {
				return fmt.Sprintf("Env passed to all sync instances as %s", key)
			},
			FieldSelector: func(p *options.Configuration, key string) *[]string {
				return p.EnvByProcessTypeAndName(definitions.ServerTypeSyncMaster, key)
			},
		},
		"envs.syncmasters": {
			Usage: func(arg, key string) string {
				return fmt.Sprintf("Env passed to all sync master instances as %s", key)
			},
			FieldSelector: func(p *options.Configuration, key string) *[]string {
				return p.EnvByServerTypeAndName(definitions.ServerTypeSyncMaster, key)
			},
		},
		"envs.syncworkers": {
			Usage: func(arg, key string) string {
				return fmt.Sprintf("Env passed to all sync master instances as %s", key)
			},
			FieldSelector: func(p *options.Configuration, key string) *[]string {
				return p.EnvByServerTypeAndName(definitions.ServerTypeSyncWorker, key)
			},
		},
	}

	config, flags, err := passthroughtPrefixesNew.Parse(os.Args...)
	if err != nil {
		log.Fatal().Err(err).Msgf("Unable to parse arguments")
	}

	for _, flag := range flags {
		if f.Lookup(flag.CleanKey) != nil {
			// Do not override predefined flags, which match prefixed config parser (like `sync.server.keyfile`)
			continue
		}

		f.StringSliceVar(flag.Value, flag.CleanKey, nil, flag.Usage)
		if flag.Deprecated {
			f.MarkDeprecated(flag.CleanKey, "Deprecated")
		}
	}

	configuration = config

	cmdStart.Flags().AddFlagSet(f)
	cmdStop.Flags().AddFlagSet(f)
}

// setFlagValuesFromEnv sets defaults from environment variables
func setFlagValuesFromEnv(fs *pflag.FlagSet) {
	envKeyReplacer := strings.NewReplacer(".", "_", "-", "_")
	fs.VisitAll(func(f *pflag.Flag) {
		if !f.Changed {
			envKey := "ARANGODB_" + strings.ToUpper(envKeyReplacer.Replace(f.Name))
			if value := os.Getenv(envKey); value != "" {
				fs.Set(f.Name, value)
			}
		}
	})
}

var (
	obsoleteOptionNameMap = map[string]string{
		"id":                  "starter.id",
		"masterPort":          "starter.port",
		"local":               "starter.local",
		"mode":                "starter.mode",
		"ownAddress":          "starter.address",
		"join":                "starter.join",
		"uniquePortOffsets":   "starter.unique-port-offsets",
		"startCoordinator":    "cluster.start-coordinator",
		"startDBserver":       "cluster.start-dbserver",
		"agencySize":          "cluster.agency-size",
		"dataDir":             "starter.data-dir",
		"data.dir":            "starter.data-dir",
		"verbose":             "log.verbose",
		"arangod":             "server.arangod",
		"jsDir":               "server.js-dir",
		"rr":                  "server.rr",
		"dockerEndpoint":      "docker.endpoint",
		"docker":              "docker.image",
		"dockerContainer":     "docker.container",
		"dockerUser":          "docker.user",
		"dockerGCDelay":       "docker.gc-delay",
		"dockerNetHost":       "docker.net-host",
		"dockerNetworkMode":   "docker.net-mode",
		"dockerPrivileged":    "docker.privileged",
		"jwtSecretFile":       "auth.jwt-secret",
		"sslKeyPath":          "ssl.keyfile",
		"sslCAFile":           "ssl.cafile",
		"sslAutoKeyFile":      "ssl.auto-key",
		"sslAutoServerName":   "ssl.auto-server-name",
		"sslAutoOrganization": "ssl.auto-organization",
	}
)

// normalizeOptionNames provides support for obsolete option names.
func normalizeOptionNames(f *pflag.FlagSet, name string) pflag.NormalizedName {
	if newName, found := obsoleteOptionNameMap[name]; found {
		name = newName
	}
	return pflag.NormalizedName(name)
}

// handleSignal listens for termination signals and stops this process onup termination.
func handleSignal(sigChannel chan os.Signal, cancel context.CancelFunc, rotateLogFiles func(context.Context)) {
	signalCount := 0
	for s := range sigChannel {
		if s == syscall.SIGHUP {
			rotateLogFiles(context.Background())
		} else {
			signalCount++
			fmt.Println("Received signal:", s)
			if signalCount > 1 {
				os.Exit(1)
			}
			cancel()
		}
	}
}

// For Windows we need to change backslashes to slashes, strangely enough:
func slasher(s string) string {
	return strings.Replace(s, "\\", "/", -1)
}

// findExecutable uses a platform dependent approach to find an executable
// with given process name.
func findExecutable(processName, defaultPath string) (executablePath string, isBuild bool) {
	var pathList = make([]string, 0, 10)
	pathList = append(pathList, "build/bin/"+processName)
	switch runtime.GOOS {
	case "windows":
		// Look in the default installation location:
		foundPaths := make([]string, 0, 20)
		basePath := "C:/Program Files"
		d, e := os.Open(basePath)
		if e == nil {
			l, e := d.Readdir(1024)
			if e == nil {
				for _, n := range l {
					if n.IsDir() {
						name := n.Name()
						if strings.HasPrefix(name, "ArangoDB3 ") ||
							strings.HasPrefix(name, "ArangoDB3e ") {
							foundPaths = append(foundPaths, basePath+"/"+name+
								"/usr/bin/"+processName+".exe")
						}
					}
				}
			} else {
				log.Error().Msgf("Could not read directory %s to look for executable.", basePath)
			}
			d.Close()
		} else {
			log.Error().Msgf("Could not open directory %s to look for executable.", basePath)
		}
		sort.Sort(sort.Reverse(sort.StringSlice(foundPaths)))
		pathList = append(pathList, foundPaths...)
	case "darwin":
		pathList = append(pathList,
			"/Applications/ArangoDB3-CLI.app/Contents/MacOS/usr/sbin/"+processName,
			"/usr/local/opt/arangodb/sbin/"+processName,
		)
	case "linux":
		pathList = append(pathList,
			"/usr/sbin/"+processName,
			"/usr/local/sbin/"+processName,
		)
	}
	// Add local folder to search path
	if exePath, err := os.Executable(); err == nil {
		folder := filepath.Dir(exePath)
		pathList = append(pathList, filepath.Join(folder, processName+filepath.Ext(exePath)))
	}

	// Search to search path for the first path that exists.
	for _, p := range pathList {
		if _, e := os.Stat(filepath.Clean(filepath.FromSlash(p))); e == nil || !os.IsNotExist(e) {
			executablePath, _ = filepath.Abs(filepath.FromSlash(p))
			isBuild = p == "build/bin/arangod"
			return
		}
	}

	return defaultPath, false
}

// findJSDir returns the JS directory to match the given executable path.
func findJSDir(executablePath string, isBuild bool) (jsPath string) {
	if isBuild {
		jsPath, _ = filepath.Abs("js")
	} else {
		relPath := filepath.Join(filepath.Dir(executablePath), "../share/arangodb3/js")
		jsPath, _ = filepath.Abs(relPath)
	}
	return
}

func main() {
	// Find executable and jsdir default in a platform dependent way:
	var isBuild bool
	arangodPath, isBuild = findExecutable("arangod", defaultArangodPath)
	arangodJSPath = findJSDir(arangodPath, isBuild)
	arangoSyncPath, _ = findExecutable("arangosync", defaultArangoSyncPath)

	if err := cmdMain.Execute(); err != nil {
		os.Exit(1)
	}
}

// Cobra run function using the usage of the given command
func cmdShowUsage(cmd *cobra.Command, args []string) {
	log.Info().Msgf("%s version %s, build %s", projectName, projectVersion, projectBuild)
	cmd.Usage()
}

func cmdShowVersionRun(cmd *cobra.Command, args []string) {
	if cmd.Use == "version" || showVersion {
		fmt.Printf("Version %s, build %s, Go %s\n", projectVersion, projectBuild, runtime.Version())
		os.Exit(0)
	}
}

func cmdMainRun(cmd *cobra.Command, args []string) {
	// Setup log level
	consoleOnly := false
	configureLogging(consoleOnly)

	log.Info().Msgf("Starting %s version %s, build %s", projectName, projectVersion, projectBuild)

	if len(args) > 0 {
		log.Fatal().Msgf("Expected no arguments, got %q", args)
	}

	// Create service
	svc, bsCfg := mustPrepareService(true)

	// Interrupt signal:
	sigChannel := make(chan os.Signal)
	rootCtx, cancel := context.WithCancel(context.Background())
	signal.Notify(sigChannel, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	go handleSignal(sigChannel, cancel, svc.RotateLogFiles)

	// Read RECOVERY file if it exists and perform recovery.
	bsCfg, err := svc.PerformRecovery(rootCtx, bsCfg)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to recover")
	}

	// Read setup.json (if exists)
	bsCfg, peers, relaunch, _ := service.ReadSetupConfig(log, dataDir, bsCfg)

	// Run the service
	if err := svc.Run(rootCtx, bsCfg, peers, relaunch); err != nil {
		log.Fatal().Err(err).Msg("Failed to run service")
	}
}

// configureLogging configures the log object according to command line arguments.
func configureLogging(consoleOnly bool) {
	logOpts := logging.LoggerOutputOptions{
		Stderr:     logOutput.Console,
		Color:      logOutput.Color,
		TimeFormat: logging.TimeFormatLocal,
	}

	if logOutput.TimeFormat == "utc-datestring" {
		logOpts.TimeFormat = logging.TimeFormatUTC
	}

	if logOutput.File && !consoleOnly {
		if logDir != "" {
			logOpts.LogFile = filepath.Join(logDir, logFileName)
		} else {
			logOpts.LogFile = filepath.Join(dataDir, logFileName)
		}
		logFileDir := filepath.Dir(logOpts.LogFile)
		if err := os.MkdirAll(logFileDir, 0755); err != nil {
			log.Fatal().Err(err).Str("directory", logFileDir).Msg("Failed to create log directory")
		}
	}
	defaultLevel := "INFO"
	if verbose {
		defaultLevel = "DEBUG"
	}
	var err error
	logService, err = logging.NewService(defaultLevel, logOpts)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to configure logging service")
	}
	log = logService.MustGetLogger(projectName)
}

// mustPrepareService creates a new Service for the configured arguments,
// creating & checking settings where needed.
func mustPrepareService(generateAutoKeyFile bool) (*service.Service, service.BootstrapConfig) {
	// Auto detect docker container ID (if needed)
	runningInDocker := false
	if isRunningInDocker() {
		runningInDocker = true
		info, err := findDockerContainerInfo(dockerEndpoint)
		if err != nil {
			if dockerContainerName == "" {
				showDockerContainerNameMissingHelp()
			}
		} else {
			if dockerContainerName == "" {
				dockerContainerName = info.Name
			}
			if info.ImageName != "" {
				dockerStarterImage = info.ImageName
			}
		}
	}

	// Some plausibility checks:
	if agencySize%2 == 0 || agencySize <= 0 {
		showClusterAgencySizeInvalidHelp()
	}
	if agencySize == 1 && ownAddress == "" {
		showClusterAgencySize1WithoutAddressHelp()
	}
	if dockerArangodImage != "" && rrPath != "" {
		showDockerImageWithRRIsNotAllowedHelp()
	}
	if dockerNetHost {
		if dockerNetworkMode == "" {
			dockerNetworkMode = "host"
		} else if dockerNetworkMode != "host" {
			showDockerNetHostAndNotModeNotBothAllowedHelp()
		}
	}
	imagePullPolicy, err := service.ParseImagePullPolicy(dockerImagePullPolicy, dockerArangodImage)
	if err != nil {
		log.Fatal().Err(err).Msgf("Unsupport image pull policy '%s'", dockerImagePullPolicy)
	}

	// Sanity checking URL scheme on advertised endpoints
	if _, err := url.Parse(advertisedEndpoint); err != nil {
		log.Fatal().Err(err).Msgf("Advertised cluster endpoint %s does not meet URL standards", advertisedEndpoint)
	}

	// Expand home-dis (~) in paths
	arangodPath = mustExpand(arangodPath)
	arangodJSPath = mustExpand(arangodJSPath)
	arangoSyncPath = mustExpand(arangoSyncPath)
	rrPath = mustExpand(rrPath)
	dataDir = mustExpand(dataDir)
	jwtSecretFile = mustExpand(jwtSecretFile)
	sslKeyFile = mustExpand(sslKeyFile)
	sslCAFile = mustExpand(sslCAFile)
	rocksDBEncryptionKeyFile = mustExpand(rocksDBEncryptionKeyFile)

	// Check database executable
	if !runningInDocker {
		if _, err := os.Stat(arangodPath); os.IsNotExist(err) {
			showArangodExecutableNotFoundHelp(arangodPath)
		}
		log.Debug().Msgf("Using %s as default arangod executable.", arangodPath)
		log.Debug().Msgf("Using %s as default JS dir.", arangodJSPath)
	}

	// Sort out work directory:
	if len(dataDir) == 0 {
		dataDir = "."
	}
	dataDir, _ = filepath.Abs(dataDir)
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		log.Fatal().Err(err).Msgf("Cannot create data directory %s, giving up.", dataDir)
	}

	// Make custom log directory absolute
	if logDir != "" {
		logDir, _ = filepath.Abs(logDir)
		if err := os.MkdirAll(logDir, 0755); err != nil {
			log.Fatal().Err(err).Msgf("Cannot create custom log directory %s, giving up.", logDir)
		}
	}

	var jwtSecret string
	if jwtSecretFile != "" {
		content, err := ioutil.ReadFile(jwtSecretFile)
		if err != nil {
			log.Fatal().Err(err).Msgf("Failed to read JWT secret file '%s'", jwtSecretFile)
		}
		jwtSecret = strings.TrimSpace(string(content))
	}

	// Auto create key file (if needed)
	if sslAutoKeyFile && generateAutoKeyFile {
		if sslKeyFile != "" {
			showSslAutoKeyAndKeyFileNotBothAllowedHelp()
		}
		hosts := []string{"arangod.server"}
		if sslAutoServerName != "" {
			hosts = []string{sslAutoServerName}
		}
		if ownAddress != "" {
			hosts = append(hosts, ownAddress)
		}
		keyFile, err := service.CreateCertificate(service.CreateCertificateOptions{
			Hosts:        hosts,
			Organization: sslAutoOrganization,
		}, dataDir)
		if err != nil {
			log.Fatal().Err(err).Msg("Failed to create keyfile")
		}
		sslKeyFile = keyFile
		log.Info().Msgf("Using self-signed certificate: %s", sslKeyFile)
	}

	// Check sync settings
	if enableSync {
		// Check mode
		if !service.ServiceMode(mode).SupportsArangoSync() {
			showArangoSyncNotAllowedWithModeHelp(mode)
		}
		if !runningInDocker {
			// Check arangosync executable
			if _, err := os.Stat(arangoSyncPath); os.IsNotExist(err) {
				showArangoSyncExecutableNotFoundHelp(arangoSyncPath)
			}
			log.Debug().Msgf("Using %s as default arangosync executable.", arangoSyncPath)
		} else {
			// Check arangosync docker image
			if dockerArangoSyncImage == "" {
				// Default to arangod docker image
				dockerArangoSyncImage = dockerArangodImage
			}
		}
		if startMaster := optionalBool(startSyncMaster, true); startMaster {
			if syncMasterKeyFile == "" {
				showSyncMasterServerKeyfileMissingHelp()
			}
			if syncMasterClientCAFile == "" {
				showSyncMasterClientCAFileMissingHelp()
			}
		}
		/*		if startWorker := optionalBool(startSyncWorker, true); startWorker {
				}*/
		if syncMasterJWTSecretFile == "" {
			if jwtSecretFile != "" {
				// Use cluster JWT secret
				syncMasterJWTSecretFile = jwtSecretFile
			} else {
				showSyncMasterJWTSecretMissingHelp()
			}
		}
		if syncMonitoringToken == "" {
			syncMonitoringToken = uniuri.New()
		}
	} else {
		startSyncMaster = []bool{false}
		startSyncWorker = []bool{false}
	}

	// Create service
	bsCfg := service.BootstrapConfig{
		ID:                       id,
		Mode:                     service.ServiceMode(mode),
		DataDir:                  dataDir,
		AgencySize:               agencySize,
		StartLocalSlaves:         startLocalSlaves,
		StartAgent:               mustGetOptionalBoolRef("cluster.start-agent", startAgent),
		StartDBserver:            mustGetOptionalBoolRef("cluster.start-dbserver", startDBserver),
		StartCoordinator:         mustGetOptionalBoolRef("cluster.start-coordinator", startCoordinator),
		StartResilientSingle:     mustGetOptionalBoolRef("cluster.start-single", startActiveFailover),
		StartSyncMaster:          mustGetOptionalBoolRef("sync.start-master", startSyncMaster),
		StartSyncWorker:          mustGetOptionalBoolRef("sync.start-worker", startSyncWorker),
		ServerStorageEngine:      serverStorageEngine,
		JwtSecret:                jwtSecret,
		SslKeyFile:               sslKeyFile,
		SslCAFile:                sslCAFile,
		RocksDBEncryptionKeyFile: rocksDBEncryptionKeyFile,
		DisableIPv6:              disableIPv6,
	}
	bsCfg.Initialize()
	serviceConfig := service.Config{
		ArangodPath:             arangodPath,
		ArangoSyncPath:          arangoSyncPath,
		ArangodJSPath:           arangodJSPath,
		AdvertisedEndpoint:      advertisedEndpoint,
		MasterPort:              masterPort,
		RrPath:                  rrPath,
		DataDir:                 dataDir,
		LogDir:                  logDir,
		OwnAddress:              ownAddress,
		BindAddress:             bindAddress,
		MasterAddresses:         masterAddresses,
		Verbose:                 verbose,
		ServerThreads:           serverThreads,
		AllPortOffsetsUnique:    allPortOffsetsUnique,
		LogRotateFilesToKeep:    logRotateFilesToKeep,
		LogRotateInterval:       logRotateInterval,
		InstanceUpTimeout:       instanceUpTimeout,
		RunningInDocker:         isRunningInDocker(),
		DockerContainerName:     dockerContainerName,
		DockerEndpoint:          dockerEndpoint,
		DockerArangodImage:      dockerArangodImage,
		DockerArangoSyncImage:   dockerArangoSyncImage,
		DockerImagePullPolicy:   imagePullPolicy,
		DockerStarterImage:      dockerStarterImage,
		DockerUser:              dockerUser,
		DockerGCDelay:           dockerGCDelay,
		DockerNetworkMode:       dockerNetworkMode,
		DockerPrivileged:        dockerPrivileged,
		DockerTTY:               dockerTTY,
		ProjectVersion:          projectVersion,
		ProjectBuild:            projectBuild,
		DebugCluster:            debugCluster,
		SyncEnabled:             enableSync,
		SyncMonitoringToken:     syncMonitoringToken,
		SyncMasterKeyFile:       syncMasterKeyFile,
		SyncMasterClientCAFile:  syncMasterClientCAFile,
		SyncMasterJWTSecretFile: syncMasterJWTSecretFile,
		SyncMQType:              syncMQType,
		Configuration:           configuration,
	}
	service := service.NewService(context.Background(), log, logService, serviceConfig, bsCfg, false)

	return service, bsCfg
}

// getEnvVar returns the value of the environment variable with given key of the given default
// value of no such variable exist or is empty.
func getEnvVar(key, defaultValue string) string {
	value := os.Getenv(key)
	if value != "" {
		return value
	}
	return defaultValue
}

// mustExpand performs a homedir.Expand and fails on errors.
func mustExpand(s string) string {
	result, err := homedir.Expand(s)
	if err != nil {
		log.Fatal().Err(err).Msgf("Cannot expand '%s'", s)
	}
	return result
}

// mustGetOptionalBoolRef returns a reference to a boolean based on given
// slice with either 0 or 1 elements.
// 0 elements -> nil
// 1 elements -> &element[0]
func mustGetOptionalBoolRef(flagName string, v []bool) *bool {
	switch len(v) {
	case 0:
		return nil
	case 1:
		x := v[0]
		return &x
	default:
		log.Fatal().Msgf("Expected 0 or 1 %s options, got %d", flagName, len(v))
		return nil
	}
}

// optionalBool returns either the first element of the given slice
// or the given default value if the slice is empty.
func optionalBool(v []bool, defaultValue bool) bool {
	if len(v) == 0 {
		return defaultValue
	}
	return v[0]
}
