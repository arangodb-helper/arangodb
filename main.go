//
// DISCLAIMER
//
// Copyright 2017-2024 ArangoDB GmbH, Cologne, Germany
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
	"github.com/arangodb-helper/arangodb/pkg/docker"
	"github.com/arangodb-helper/arangodb/pkg/features"
	"github.com/arangodb-helper/arangodb/pkg/logging"
	"github.com/arangodb-helper/arangodb/pkg/net"
	"github.com/arangodb-helper/arangodb/pkg/terminal"
	service "github.com/arangodb-helper/arangodb/service"
	"github.com/arangodb-helper/arangodb/service/options"
)

//go:generate go run internal/generate-exit-codes/main.go

// Configuration data with defaults:

const (
	projectName                     = "arangodb"
	logFileName                     = projectName + ".log"
	defaultDockerGCDelay            = time.Minute * 10
	defaultDockerStarterImage       = "arangodb/arangodb-starter"
	defaultArangodPath              = "/usr/sbin/arangod"
	defaultArangoSyncPath           = "/usr/sbin/arangosync"
	defaultConfigFilePath           = "arangodb-starter.conf"
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
	log        zerolog.Logger
	logService logging.Service

	showVersion    bool
	configFilePath string

	opts                starterOptions
	passthroughOpts     *options.Configuration
	passthroughPrefixes = preparePassthroughPrefixes()
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
	pf.BoolVar(&showVersion, "version", false, "If set, show version and exit")
	pf.StringVarP(&configFilePath, "configuration", "c", defaultConfigFilePath, "Configuration file path")

	pf.BoolVar(&opts.log.verbose, "log.verbose", false, "Turn on debug logging")
	pf.BoolVar(&opts.log.console, "log.console", true, "Send log output to console")
	pf.BoolVar(&opts.log.file, "log.file", true, "Send log output to file")
	pf.BoolVar(&opts.log.color, "log.color", defaultLogColor, "Colorize the log output")
	pf.StringVar(&opts.log.timeFormat, "log.time-format", "local-datestring",
		"Time format to use in logs. Possible values: 'local-datestring' (default), 'utc-datestring'")
	pf.StringVar(&opts.log.dir, "log.dir", getEnvVar("LOG_DIR", ""), "Custom log file directory.")

	f := cmdMain.Flags()
	f.StringSliceVar(&opts.starter.masterAddresses, "starter.join", nil, "join a cluster with master at given address")
	f.StringVar(&opts.starter.mode, "starter.mode", "cluster", "Set the mode of operation to use (cluster|single|activefailover). Note that 'activefailover' is deprecated and will be removed in future releases")
	f.BoolVar(&opts.starter.startLocalSlaves, "starter.local", false, "If set, local slaves will be started to create a machine local (test) cluster")
	f.StringVar(&opts.starter.ownAddress, "starter.address", "", "address under which this server is reachable, needed for running in docker or in single mode")
	f.StringVar(&opts.starter.bindAddress, "starter.host", "0.0.0.0", "address used to bind the starter to")
	f.StringVar(&opts.starter.id, "starter.id", "", "Unique identifier of this peer")
	f.IntVar(&opts.starter.masterPort, "starter.port", service.DefaultMasterPort, "Port to listen on for other arangodb's to join")
	f.BoolVar(&opts.starter.allPortOffsetsUnique, "starter.unique-port-offsets", false, "If set, all peers will get a unique port offset. If false (default) only portOffset+peerAddress pairs will be unique.")
	f.StringVar(&opts.starter.dataDir, "starter.data-dir", getEnvVar("DATA_DIR", "."), "directory to store all data the starter generates (and holds actual database directories)")
	f.BoolVar(&opts.starter.debugCluster, "starter.debug-cluster", getEnvVar("DEBUG_CLUSTER", "") != "", "If set, log more information to debug a cluster")
	f.BoolVar(&opts.starter.disableIPv6, "starter.disable-ipv6", !net.IsIPv6Supported(), "If set, no IPv6 notation will be used. Use this only when IPv6 address family is disabled")
	f.BoolVar(&opts.starter.enableSync, "starter.sync", false, "If set, the starter will also start arangosync instances")
	f.DurationVar(&opts.starter.instanceUpTimeout, "starter.instance-up-timeout", defaultInstanceUpTimeout, "Timeout to wait for an instance start")
	if err := features.JWTRotation().RegisterDeprecated(f); err != nil {
		panic(err)
	}

	f.IntVar(&opts.log.rotateFilesToKeep, "log.rotate-files-to-keep", defaultLogRotateFilesToKeep, "Number of files to keep when rotating log files")
	f.DurationVar(&opts.log.rotateInterval, "log.rotate-interval", defaultLogRotateInterval, "Time between log rotations (0 disables log rotation)")

	f.StringVar(&opts.cluster.advertisedEndpoint, "cluster.advertised-endpoint", "", "An external endpoint for the servers started by this Starter")
	f.IntVar(&opts.cluster.agencySize, "cluster.agency-size", 3, "Number of agents in the cluster")
	f.BoolSliceVar(&opts.cluster.startAgent, "cluster.start-agent", nil, "should an agent instance be started")
	f.BoolSliceVar(&opts.cluster.startDBServer, "cluster.start-dbserver", nil, "should a dbserver instance be started")
	f.BoolSliceVar(&opts.cluster.startCoordinator, "cluster.start-coordinator", nil, "should a coordinator instance be started")
	f.BoolSliceVar(&opts.cluster.startActiveFailover, "cluster.start-single", nil, "should an active-failover single server instance be started")
	f.MarkDeprecated("cluster.start-single", "Active-Failover (resilient-single) mode is deprecated and will be removed in coming releases")

	f.BoolVar(&opts.server.useLocalBin, "server.use-local-bin", false, "If true, starter will try searching for binaries in local directory first")
	f.StringVar(&opts.server.arangodPath, "server.arangod", defaultArangodPath, "Path of arangod")
	f.StringVar(&opts.server.arangoSyncPath, "server.arangosync", defaultArangoSyncPath, "Path of arangosync")
	f.StringVar(&opts.server.arangodJSPath, "server.js-dir", "/usr/share/arangodb3/js", "Path of arango JS folder")
	f.StringVar(&opts.server.rrPath, "server.rr", "", "Path of rr")
	f.IntVar(&opts.server.threads, "server.threads", 0, "Adjust server.threads of each server")
	f.StringVar(&opts.server.storageEngine, "server.storage-engine", "", "Type of storage engine to use (mmfiles|rocksdb) (3.2 and up)")

	f.StringVar(&opts.rocksDB.encryptionKeyFile, "rocksdb.encryption-keyfile", "", "Key file used for RocksDB encryption. (Enterprise Edition 3.2 and up)")
	f.StringVar(&opts.rocksDB.encryptionKeyGenerator, "rocksdb.encryption-key-generator", "", "Path to program. The output of this program will be used as key for RocksDB encryption. (Enterprise Edition)")

	f.StringVar(&opts.docker.endpoint, "docker.endpoint", "unix:///var/run/docker.sock", "Endpoint used to reach the docker daemon")
	f.StringVar(&opts.docker.arangodImage, "docker.image", getEnvVar("DOCKER_IMAGE", ""), "name of the Docker image to use to launch arangod instances (leave empty to avoid using docker)")
	f.StringVar(&opts.docker.arangoSyncImage, "docker.sync-image", getEnvVar("DOCKER_ARANGOSYNC_IMAGE", ""), "name of the Docker image to use to launch arangosync instances")
	f.StringVar(&opts.docker.imagePullPolicy, "docker.imagePullPolicy", "", "pull docker image from docker hub (Always|IfNotPresent|Never)")
	f.StringVar(&opts.docker.user, "docker.user", "", "use the given name as user to run the Docker container")
	f.StringVar(&opts.docker.containerName, "docker.container", "", "name of the docker container that is running this process")
	f.DurationVar(&opts.docker.gcDelay, "docker.gc-delay", defaultDockerGCDelay, "Delay before stopped containers are garbage collected")
	f.BoolVar(&opts.docker.netHost, "docker.net-host", false, "Run containers with --net=host")
	f.Lookup("docker.net-host").Deprecated = "use --docker.net-mode=host instead"
	f.StringVar(&opts.docker.networkMode, "docker.net-mode", "", "Run containers with --net=<value>")
	f.BoolVar(&opts.docker.privileged, "docker.privileged", false, "Run containers with --privileged")
	f.BoolVar(&opts.docker.tty, "docker.tty", true, "Run containers with TTY enabled")

	f.StringVar(&opts.auth.jwtSecretFile, "auth.jwt-secret", "", "name of a plain text file containing a JWT secret used for server authentication")

	f.StringVar(&opts.ssl.keyFile, "ssl.keyfile", "", "path of a PEM encoded file containing a server certificate + private key")
	f.StringVar(&opts.ssl.caFile, "ssl.cafile", "", "path of a PEM encoded file containing a CA certificate used for client authentication")
	f.BoolVar(&opts.ssl.autoKeyFile, "ssl.auto-key", false, "If set, a self-signed certificate will be created and used as --ssl.keyfile")
	f.StringVar(&opts.ssl.autoServerName, "ssl.auto-server-name", "", "Server name put into self-signed certificate. See --ssl.auto-key")
	f.StringVar(&opts.ssl.autoOrganization, "ssl.auto-organization", "ArangoDB", "Organization name put into self-signed certificate. See --ssl.auto-key")

	f.BoolSliceVar(&opts.sync.startSyncMaster, "sync.start-master", nil, "should an ArangoSync master instance be started (only relevant when starter.sync is enabled)")
	f.BoolSliceVar(&opts.sync.startSyncWorker, "sync.start-worker", nil, "should an ArangoSync worker instance be started (only relevant when starter.sync is enabled)")
	f.StringVar(&opts.sync.monitoring.token, "sync.monitoring.token", "", "Bearer token used to access ArangoSync monitoring endpoints")
	f.StringVar(&opts.sync.master.jwtSecretFile, "sync.master.jwt-secret", "", "File containing JWT secret used to access the Sync Master (from Sync Worker)")
	f.StringVar(&opts.sync.mq.Type, "sync.mq.type", "direct", "Type of message queue used by the Sync Master")
	f.StringVar(&opts.sync.server.keyFile, "sync.server.keyfile", "", "TLS keyfile of local sync master")
	f.StringVar(&opts.sync.server.clientCAFile, "sync.server.client-cafile", "", "CA Certificate used for client certificate verification")

	cmdMain.Flags().SetNormalizeFunc(normalizeOptionNames)

	config, flags, err := passthroughPrefixes.Parse(os.Args...)
	if err != nil {
		log.Fatal().Err(err).Msgf("Unable to parse arguments")
	}

	for _, flag := range flags {
		if f.Lookup(flag.CleanKey) != nil {
			// Do not override predefined flags, which match prefixed config parser (like `sync.server.keyfile`)
			continue
		}

		f.StringSliceVar(flag.Value, flag.CleanKey, nil, flag.Usage)
		if flag.DeprecatedHint != "" {
			f.MarkDeprecated(flag.CleanKey, flag.DeprecatedHint)
		}
	}

	passthroughOpts = config

	cmdStart.Flags().AddFlagSet(f)
	cmdStop.Flags().AddFlagSet(f)

	passthroughUsageHelp := passthroughPrefixes.UsageHint()
	decorateUsageOutputWithPassthroughHint(passthroughUsageHelp, cmdMain)
	decorateUsageOutputWithPassthroughHint(passthroughUsageHelp, cmdStart)
	decorateUsageOutputWithPassthroughHint(passthroughUsageHelp, cmdStop)
}

// decorateUsageOutputWithPassthroughHint enriches command usage output with info on
func decorateUsageOutputWithPassthroughHint(passthroughUsageHelp string, cmd *cobra.Command) {
	oldUsageFn := cmd.UsageFunc()
	cmd.SetUsageFunc(func(c *cobra.Command) error {
		err := oldUsageFn(c)
		if err != nil {
			return err
		}

		if c.Name() == cmd.Name() {
			_, err = c.OutOrStderr().Write([]byte("\n" + passthroughUsageHelp))
		}
		if err != nil {
			c.Println(err)
		}
		return err
	})
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
			log.Debug().Msgf("Received signal: %s", s.String())
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
	var localPaths []string
	if exePath, err := os.Executable(); err == nil {
		folder := filepath.Dir(exePath)
		localPaths = append(localPaths, filepath.Join(folder, processName+filepath.Ext(exePath)))

		// Also try searching in ../sbin in case if we are running from local installation
		if runtime.GOOS != "windows" {
			localPaths = append(localPaths, filepath.Join(folder, "../sbin", processName+filepath.Ext(exePath)))
		}
	}

	var pathList []string
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

	if opts.server.useLocalBin {
		pathList = append(localPaths, pathList...)
	} else {
		pathList = append(pathList, localPaths...)
	}

	// buildPath should be always first on the list
	buildPath := "build/bin/" + processName
	pathList = append([]string{buildPath}, pathList...)

	// Search for the first path that exists.
	for _, p := range pathList {
		if _, e := os.Stat(filepath.Clean(filepath.FromSlash(p))); e == nil || !os.IsNotExist(e) {
			executablePath, _ = filepath.Abs(filepath.FromSlash(p))
			isBuild = p == buildPath
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
	opts.server.arangodPath, isBuild = findExecutable("arangod", defaultArangodPath)
	opts.server.arangodJSPath = findJSDir(opts.server.arangodPath, isBuild)
	opts.server.arangoSyncPath, _ = findExecutable("arangosync", defaultArangoSyncPath)

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
	loadFlagValuesFromConfig(configFilePath, cmd.Flags(), cmd.PersistentFlags())

	// Setup log level
	configureLogging(false)

	log.Info().Msgf("Starting %s version %s, build %s", projectName, projectVersion, projectBuild)

	if len(args) > 0 {
		log.Fatal().Msgf("Expected no arguments, got %q", args)
	}

	sanityCheckPassThroughArgs(cmd.Flags(), cmd.PersistentFlags())

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
	setupConfig, relaunch, _ := service.ReadSetupConfig(log, opts.starter.dataDir)
	if relaunch {
		oldOpts := setupConfig.Peers.PersistentOptions
		newOpts := passthroughOpts.PersistentOptions
		if err := oldOpts.ValidateCompatibility(&newOpts); err != nil {
			log.Error().Err(err).Msg("Please check pass-through options")
		}

		bsCfg.LoadFromSetupConfig(setupConfig)
	}

	// Run the service
	if err := svc.Run(rootCtx, bsCfg, setupConfig.Peers, relaunch); err != nil {
		log.Fatal().Err(err).Msg("Failed to run service")
	}
}

// configureLogging configures the log object according to command line arguments.
func configureLogging(consoleOnly bool) {
	logOpts := logging.LoggerOutputOptions{
		Stderr:     opts.log.console,
		Color:      opts.log.color,
		TimeFormat: logging.TimeFormatLocal,
	}

	if opts.log.timeFormat == "utc-datestring" {
		logOpts.TimeFormat = logging.TimeFormatUTC
	}

	if opts.log.file && !consoleOnly {
		if opts.log.dir != "" {
			logOpts.LogFile = filepath.Join(opts.log.dir, logFileName)
		} else {
			logOpts.LogFile = filepath.Join(opts.starter.dataDir, logFileName)
		}
		logFileDir := filepath.Dir(logOpts.LogFile)
		if err := os.MkdirAll(logFileDir, 0755); err != nil {
			log.Fatal().Err(err).Str("directory", logFileDir).Msg("Failed to create log directory")
		}
	}
	defaultLevel := "INFO"
	if opts.log.verbose {
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
	dockerStarterImage := defaultDockerStarterImage
	runningInDocker := false
	if docker.IsRunningInDocker() {
		runningInDocker = true
		info, err := docker.FindDockerContainerInfo(opts.docker.endpoint)
		if err != nil {
			log.Warn().Err(err).Msgf("could not find docker container info")
			if opts.docker.containerName == "" {
				showDockerContainerNameMissingHelp()
			}
		} else {
			if opts.docker.containerName == "" {
				opts.docker.containerName = info.Name
			}
			if info.ImageName != "" {
				dockerStarterImage = info.ImageName
			}
		}
	}

	// Some plausibility checks:
	if opts.cluster.agencySize%2 == 0 || opts.cluster.agencySize <= 0 {
		showClusterAgencySizeInvalidHelp()
	}
	if opts.cluster.agencySize == 1 && opts.starter.ownAddress == "" {
		showClusterAgencySize1WithoutAddressHelp()
	}
	if opts.docker.arangodImage != "" && opts.server.rrPath != "" {
		showDockerImageWithRRIsNotAllowedHelp()
	}
	if opts.docker.netHost {
		if opts.docker.networkMode == "" {
			opts.docker.networkMode = "host"
		} else if opts.docker.networkMode != "host" {
			showDockerNetHostAndNotModeNotBothAllowedHelp()
		}
	}
	imagePullPolicy, err := service.ParseImagePullPolicy(opts.docker.imagePullPolicy, opts.docker.arangodImage)
	if err != nil {
		log.Fatal().Err(err).Msgf("Unsupported image pull policy '%s'", opts.docker.imagePullPolicy)
	}

	// Sanity checking URL scheme on advertised endpoints
	if _, err := url.Parse(opts.cluster.advertisedEndpoint); err != nil {
		log.Fatal().Err(err).Msgf("Advertised cluster endpoint %s does not meet URL standards", opts.cluster.advertisedEndpoint)
	}

	// Expand home-dis (~) in paths
	opts.server.arangodPath = mustExpand(opts.server.arangodPath)
	opts.server.arangodJSPath = mustExpand(opts.server.arangodJSPath)
	opts.server.arangoSyncPath = mustExpand(opts.server.arangoSyncPath)
	opts.server.rrPath = mustExpand(opts.server.rrPath)
	opts.starter.dataDir = mustExpand(opts.starter.dataDir)
	opts.auth.jwtSecretFile = mustExpand(opts.auth.jwtSecretFile)
	opts.ssl.keyFile = mustExpand(opts.ssl.keyFile)
	opts.ssl.caFile = mustExpand(opts.ssl.caFile)
	opts.rocksDB.encryptionKeyFile = mustExpand(opts.rocksDB.encryptionKeyFile)
	opts.rocksDB.encryptionKeyGenerator = mustExpand(opts.rocksDB.encryptionKeyGenerator)

	// Check database executable
	if !runningInDocker {
		if _, err := os.Stat(opts.server.arangodPath); os.IsNotExist(err) {
			showArangodExecutableNotFoundHelp(opts.server.arangodPath)
		}
		log.Debug().Msgf("Using %s as default arangod executable.", opts.server.arangodPath)
		log.Debug().Msgf("Using %s as default JS dir.", opts.server.arangodJSPath)
	}

	// Sort out work directory:
	if len(opts.starter.dataDir) == 0 {
		opts.starter.dataDir = "."
	}
	opts.starter.dataDir, _ = filepath.Abs(opts.starter.dataDir)
	if err := os.MkdirAll(opts.starter.dataDir, 0755); err != nil {
		log.Fatal().Err(err).Msgf("Cannot create data directory %s, giving up.", opts.starter.dataDir)
	}

	// Make custom log directory absolute
	if opts.log.dir != "" {
		opts.log.dir, _ = filepath.Abs(opts.log.dir)
		if err := os.MkdirAll(opts.log.dir, 0755); err != nil {
			log.Fatal().Err(err).Msgf("Cannot create custom log directory %s, giving up.", opts.log.dir)
		}
	}

	var jwtSecret string
	if opts.auth.jwtSecretFile != "" {
		content, err := ioutil.ReadFile(opts.auth.jwtSecretFile)
		if err != nil {
			log.Fatal().Err(err).Msgf("Failed to read JWT secret file '%s'", opts.auth.jwtSecretFile)
		}
		jwtSecret = strings.TrimSpace(string(content))
	}

	// Auto create key file (if needed)
	if opts.ssl.autoKeyFile && generateAutoKeyFile {
		if opts.ssl.keyFile != "" {
			showSslAutoKeyAndKeyFileNotBothAllowedHelp()
		}
		hosts := []string{"arangod.server"}
		if opts.ssl.autoServerName != "" {
			hosts = []string{opts.ssl.autoServerName}
		}
		if opts.starter.ownAddress != "" {
			hosts = append(hosts, opts.starter.ownAddress)
		}
		keyFile, err := service.CreateCertificate(service.CreateCertificateOptions{
			Hosts:        hosts,
			Organization: opts.ssl.autoOrganization,
		}, opts.starter.dataDir)
		if err != nil {
			log.Fatal().Err(err).Msg("Failed to create keyfile")
		}
		opts.ssl.keyFile = keyFile
		log.Info().Msgf("Using self-signed certificate: %s", opts.ssl.keyFile)
	}

	serviceMode := service.ServiceMode(opts.starter.mode)
	if serviceMode.IsActiveFailoverMode() || optionalBool(opts.cluster.startActiveFailover, false) {
		log.Warn().Msgf("Active-Failover (resilient-single) mode is deprecated and will be removed in coming releases")
	}

	// Check sync settings
	if opts.starter.enableSync {
		// Check mode
		if !serviceMode.SupportsArangoSync() {
			showArangoSyncNotAllowedWithModeHelp(opts.starter.mode)
		}
		if !runningInDocker {
			// Check arangosync executable
			if _, err := os.Stat(opts.server.arangoSyncPath); os.IsNotExist(err) {
				showArangoSyncExecutableNotFoundHelp(opts.server.arangoSyncPath)
			}
			log.Debug().Msgf("Using %s as default arangosync executable.", opts.server.arangoSyncPath)
		} else {
			// Check arangosync docker image
			if opts.docker.arangoSyncImage == "" {
				// Default to arangod docker image
				opts.docker.arangoSyncImage = opts.docker.arangodImage
			}
		}
		if startMaster := optionalBool(opts.sync.startSyncMaster, true); startMaster {
			if opts.sync.server.keyFile == "" {
				showSyncMasterServerKeyfileMissingHelp()
			}
			if opts.sync.server.clientCAFile == "" {
				showSyncMasterClientCAFileMissingHelp()
			}
		}
		if opts.sync.master.jwtSecretFile == "" {
			if opts.auth.jwtSecretFile != "" {
				// Use cluster JWT secret
				opts.sync.master.jwtSecretFile = opts.auth.jwtSecretFile
			} else {
				showSyncMasterJWTSecretMissingHelp()
			}
		}
		if opts.sync.monitoring.token == "" {
			opts.sync.monitoring.token = uniuri.New()
		}
	} else {
		opts.sync.startSyncMaster = []bool{false}
		opts.sync.startSyncWorker = []bool{false}
	}

	// Create service
	bsCfg := service.BootstrapConfig{
		ID:                            opts.starter.id,
		Mode:                          serviceMode,
		DataDir:                       opts.starter.dataDir,
		AgencySize:                    opts.cluster.agencySize,
		StartLocalSlaves:              opts.starter.startLocalSlaves,
		StartAgent:                    mustGetOptionalBoolRef("cluster.start-agent", opts.cluster.startAgent),
		StartDBserver:                 mustGetOptionalBoolRef("cluster.start-dbserver", opts.cluster.startDBServer),
		StartCoordinator:              mustGetOptionalBoolRef("cluster.start-coordinator", opts.cluster.startCoordinator),
		StartResilientSingle:          mustGetOptionalBoolRef("cluster.start-single", opts.cluster.startActiveFailover),
		StartSyncMaster:               mustGetOptionalBoolRef("sync.start-master", opts.sync.startSyncMaster),
		StartSyncWorker:               mustGetOptionalBoolRef("sync.start-worker", opts.sync.startSyncWorker),
		ServerStorageEngine:           opts.server.storageEngine,
		JwtSecret:                     jwtSecret,
		SslKeyFile:                    opts.ssl.keyFile,
		SslCAFile:                     opts.ssl.caFile,
		RocksDBEncryptionKeyFile:      opts.rocksDB.encryptionKeyFile,
		RocksDBEncryptionKeyGenerator: opts.rocksDB.encryptionKeyGenerator,
		DisableIPv6:                   opts.starter.disableIPv6,
	}
	bsCfg.Initialize()
	serviceConfig := service.Config{
		ArangodPath:             opts.server.arangodPath,
		ArangoSyncPath:          opts.server.arangoSyncPath,
		ArangodJSPath:           opts.server.arangodJSPath,
		AdvertisedEndpoint:      opts.cluster.advertisedEndpoint,
		MasterPort:              opts.starter.masterPort,
		RrPath:                  opts.server.rrPath,
		DataDir:                 opts.starter.dataDir,
		LogDir:                  opts.log.dir,
		OwnAddress:              opts.starter.ownAddress,
		BindAddress:             opts.starter.bindAddress,
		MasterAddresses:         opts.starter.masterAddresses,
		Verbose:                 opts.log.verbose,
		ServerThreads:           opts.server.threads,
		AllPortOffsetsUnique:    opts.starter.allPortOffsetsUnique,
		LogRotateFilesToKeep:    opts.log.rotateFilesToKeep,
		LogRotateInterval:       opts.log.rotateInterval,
		InstanceUpTimeout:       opts.starter.instanceUpTimeout,
		RunningInDocker:         docker.IsRunningInDocker(),
		DockerContainerName:     opts.docker.containerName,
		DockerEndpoint:          opts.docker.endpoint,
		DockerArangodImage:      opts.docker.arangodImage,
		DockerArangoSyncImage:   opts.docker.arangoSyncImage,
		DockerImagePullPolicy:   imagePullPolicy,
		DockerStarterImage:      dockerStarterImage,
		DockerUser:              opts.docker.user,
		DockerGCDelay:           opts.docker.gcDelay,
		DockerNetworkMode:       opts.docker.networkMode,
		DockerPrivileged:        opts.docker.privileged,
		DockerTTY:               opts.docker.tty,
		ProjectVersion:          projectVersion,
		ProjectBuild:            projectBuild,
		DebugCluster:            opts.starter.debugCluster,
		SyncEnabled:             opts.starter.enableSync,
		SyncMonitoringToken:     opts.sync.monitoring.token,
		SyncMasterKeyFile:       opts.sync.server.keyFile,
		SyncMasterClientCAFile:  opts.sync.server.clientCAFile,
		SyncMasterJWTSecretFile: opts.sync.master.jwtSecretFile,
		SyncMQType:              opts.sync.mq.Type,
		Configuration:           passthroughOpts,
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
