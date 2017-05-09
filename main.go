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
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"

	_ "github.com/arangodb-helper/arangodb/client"
	service "github.com/arangodb-helper/arangodb/service"
	homedir "github.com/mitchellh/go-homedir"
	logging "github.com/op/go-logging"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Configuration data with defaults:

const (
	projectName          = "arangodb"
	defaultDockerGCDelay = time.Minute * 10
)

var (
	projectVersion = "dev"
	projectBuild   = "dev"
	cmdMain        = cobra.Command{
		Use:   projectName,
		Short: "Start ArangoDB clusters & single servers with ease",
		Run:   cmdMainRun,
	}
	log                  = logging.MustGetLogger(projectName)
	id                   string
	agencySize           int
	arangodPath          string
	arangodJSPath        string
	masterPort           int
	rrPath               string
	startCoordinator     bool
	startDBserver        bool
	startLocalSlaves     bool
	mode                 string
	dataDir              string
	ownAddress           string
	masterAddress        string
	verbose              bool
	serverThreads        int
	allPortOffsetsUnique bool
	jwtSecretFile        string
	sslKeyFile           string
	sslAutoKeyFile       bool
	sslAutoServerName    string
	sslAutoOrganization  string
	sslCAFile            string
	dockerEndpoint       string
	dockerImage          string
	dockerUser           string
	dockerContainerName  string
	dockerGCDelay        time.Duration
	dockerNetHost        bool // Deprecated
	dockerNetworkMode    string
	dockerPrivileged     bool

	maskAny = errors.WithStack
)

func init() {
	f := cmdMain.Flags()

	f.StringVar(&masterAddress, "starter.join", "", "join a cluster with master at given address")
	f.StringVar(&mode, "starter.mode", "cluster", "Set the mode of operation to use (cluster|single)")
	f.BoolVar(&startLocalSlaves, "starter.local", false, "If set, local slaves will be started to create a machine local (test) cluster")
	f.StringVar(&ownAddress, "starter.address", "", "address under which this server is reachable, needed for running in docker or in single mode")
	f.StringVar(&id, "starter.id", "", "Unique identifier of this peer")
	f.IntVar(&masterPort, "starter.port", service.DefaultMasterPort, "Port to listen on for other arangodb's to join")
	f.BoolVar(&allPortOffsetsUnique, "starter.unique-port-offsets", false, "If set, all peers will get a unique port offset. If false (default) only portOffset+peerAddress pairs will be unique.")

	f.StringVar(&dataDir, "data.dir", getEnvVar("DATA_DIR", "."), "directory to store all data")

	f.BoolVar(&verbose, "log.verbose", false, "Turn on debug logging")

	f.IntVar(&agencySize, "cluster.agency-size", 3, "Number of agents in the cluster")
	f.BoolVar(&startCoordinator, "cluster.start-coordinator", true, "should a coordinator instance be started")
	f.BoolVar(&startDBserver, "cluster.start-dbserver", true, "should a dbserver instance be started")

	f.StringVar(&arangodPath, "server.arangod-path", "/usr/sbin/arangod", "Path of arangod")
	f.StringVar(&arangodJSPath, "server.js-dir", "/usr/share/arangodb3/js", "Path of arango JS folder")
	f.StringVar(&rrPath, "server.rr-path", "", "Path of rr")
	f.IntVar(&serverThreads, "server.threads", 0, "Adjust server.threads of each server")

	f.StringVar(&dockerEndpoint, "docker.endpoint", "unix:///var/run/docker.sock", "Endpoint used to reach the docker daemon")
	f.StringVar(&dockerImage, "docker.image", getEnvVar("DOCKER_IMAGE", ""), "name of the Docker image to use to launch arangod instances (leave empty to avoid using docker)")
	f.StringVar(&dockerUser, "docker.user", "", "use the given name as user to run the Docker container")
	f.StringVar(&dockerContainerName, "docker.container", "", "name of the docker container that is running this process")
	f.DurationVar(&dockerGCDelay, "docker.gc-delay", defaultDockerGCDelay, "Delay before stopped containers are garbage collected")
	f.BoolVar(&dockerNetHost, "docker.net-host", false, "Run containers with --net=host")
	f.Lookup("docker.net-host").Deprecated = "use --docker.net-mode=host instead"
	f.StringVar(&dockerNetworkMode, "docker.net-mode", "", "Run containers with --net=<value>")
	f.BoolVar(&dockerPrivileged, "docker.privileged", false, "Run containers with --privileged")

	f.StringVar(&jwtSecretFile, "auth.jwt-secret-path", "", "name of a plain text file containing a JWT secret used for server authentication")

	f.StringVar(&sslKeyFile, "ssl.key-path", "", "path of a PEM encoded file containing a server certificate + private key")
	f.StringVar(&sslCAFile, "ssl.ca-path", "", "path of a PEM encoded file containing a CA certificate used for client authentication")
	f.BoolVar(&sslAutoKeyFile, "ssl.auto-key", false, "If set, a self-signed certificate will be created and used as --ssl.key-path")
	f.StringVar(&sslAutoServerName, "ssl.auto-server-name", "", "Server name put into self-signed certificate. See --ssl.auto-key")
	f.StringVar(&sslAutoOrganization, "ssl.auto-organization", "ArangoDB", "Organization name put into self-signed certificate. See --ssl.auto-key")

	f.SetNormalizeFunc(normalizeOptionNames)
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
		"dataDir":             "data.dir",
		"verbose":             "log.verbose",
		"arangod":             "server.arangod-path",
		"jsDir":               "server.js-dir",
		"rr":                  "server.rr-path",
		"dockerEndpoint":      "docker.endpoint",
		"docker":              "docker.image",
		"dockerContainer":     "docker.container",
		"dockerUser":          "docker.user",
		"dockerGCDelay":       "docker.gc-delay",
		"dockerNetHost":       "docker.net-host",
		"dockerNetworkMode":   "docker.net-mode",
		"dockerPrivileged":    "docker.privileged",
		"jwtSecretFile":       "auth.jwt-secret-path",
		"sslKeyPath":          "ssl.key-path",
		"sslCAFile":           "ssl.ca-path",
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
func handleSignal(sigChannel chan os.Signal, cancel context.CancelFunc) {
	signalCount := 0
	for s := range sigChannel {
		signalCount++
		fmt.Println("Received signal:", s)
		if signalCount > 1 {
			os.Exit(1)
		}
		cancel()
	}
}

// For Windows we need to change backslashes to slashes, strangely enough:
func slasher(s string) string {
	return strings.Replace(s, "\\", "/", -1)
}

func findExecutable() {
	var pathList = make([]string, 0, 10)
	pathList = append(pathList, "build/bin/arangod")
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
								"/usr/bin/arangod.exe")
						}
					}
				}
			} else {
				log.Errorf("Could not read directory %s to look for executable.", basePath)
			}
			d.Close()
		} else {
			log.Errorf("Could not open directory %s to look for executable.", basePath)
		}
		sort.Sort(sort.Reverse(sort.StringSlice(foundPaths)))
		pathList = append(pathList, foundPaths...)
	case "darwin":
		pathList = append(pathList,
			"/Applications/ArangoDB3-CLI.app/Contents/MacOS/usr/sbin/arangod",
			"/usr/local/opt/arangodb/sbin/arangod",
		)
	case "linux":
		pathList = append(pathList,
			"/usr/sbin/arangod",
		)
	}
	for _, p := range pathList {
		if _, e := os.Stat(filepath.Clean(filepath.FromSlash(p))); e == nil || !os.IsNotExist(e) {
			arangodPath, _ = filepath.Abs(filepath.FromSlash(p))
			if p == "build/bin/arangod" {
				arangodJSPath, _ = filepath.Abs("js")
			} else {
				arangodJSPath, _ = filepath.Abs(filepath.FromSlash(filepath.Dir(p) + "/../share/arangodb3/js"))
			}
			return
		}
	}
}

func main() {
	// Find executable and jsdir default in a platform dependent way:
	findExecutable()

	cmdMain.Execute()
}

func cmdMainRun(cmd *cobra.Command, args []string) {
	log.Infof("Starting %s version %s, build %s", projectName, projectVersion, projectBuild)

	if len(args) > 0 {
		log.Fatalf("Expected no arguments, got %q", args)
	}

	// Setup log level
	if verbose {
		logging.SetLevel(logging.DEBUG, projectName)
	} else {
		logging.SetLevel(logging.INFO, projectName)
	}

	// Auto detect docker container ID (if needed)
	if isRunningInDocker() && dockerContainerName == "" {
		id, err := findDockerContainerName(dockerEndpoint)
		if err != nil {
			log.Fatalf("Cannot find docker container name. Please specify using --dockerContainer=...")
		}
		dockerContainerName = id
	}

	// Some plausibility checks:
	if agencySize%2 == 0 || agencySize <= 0 {
		log.Fatal("Error: cluster.agency-size needs to be a positive, odd number.")
	}
	if agencySize == 1 && ownAddress == "" {
		log.Fatal("Error: if cluster.agency-size==1, starter.address must be given.")
	}
	if dockerImage != "" && rrPath != "" {
		log.Fatal("Error: using --docker.image and --server.rr-path is not possible.")
	}
	if dockerNetHost {
		if dockerNetworkMode == "" {
			dockerNetworkMode = "host"
		} else if dockerNetworkMode != "host" {
			log.Fatal("Error: cannot set --docker.net-host and --docker.net-mode at the same time")
		}
	}
	log.Debugf("Using %s as default arangod executable.", arangodPath)
	log.Debugf("Using %s as default JS dir.", arangodJSPath)

	// Expand home-dis (~) in paths
	arangodPath = mustExpand(arangodPath)
	arangodJSPath = mustExpand(arangodJSPath)
	rrPath = mustExpand(rrPath)
	dataDir = mustExpand(dataDir)
	jwtSecretFile = mustExpand(jwtSecretFile)
	sslKeyFile = mustExpand(sslKeyFile)
	sslCAFile = mustExpand(sslCAFile)

	// Sort out work directory:
	if len(dataDir) == 0 {
		dataDir = "."
	}
	dataDir, _ = filepath.Abs(dataDir)
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		log.Fatalf("Cannot create data directory %s because %v, giving up.", dataDir, err)
	}

	// Read jwtSecret (if any)
	var jwtSecret string
	if jwtSecretFile != "" {
		content, err := ioutil.ReadFile(jwtSecretFile)
		if err != nil {
			log.Fatalf("Failed to read JWT secret file '%s': %v", jwtSecretFile, err)
		}
		jwtSecret = strings.TrimSpace(string(content))
	}

	// Auto create key file (if needed)
	if sslAutoKeyFile {
		if sslKeyFile != "" {
			log.Fatalf("Cannot specify both --ssl.auto-key and --ssl.key-path")
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
			RSABits:      2048,
			Organization: sslAutoOrganization,
		}, dataDir)
		if err != nil {
			log.Fatalf("Failed to create keyfile: %v", err)
		}
		sslKeyFile = keyFile
		log.Infof("Using self-signed certificate: %s", sslKeyFile)
	}

	// Interrupt signal:
	sigChannel := make(chan os.Signal)
	rootCtx, cancel := context.WithCancel(context.Background())
	signal.Notify(sigChannel, os.Interrupt, syscall.SIGTERM)
	go handleSignal(sigChannel, cancel)

	// Create service
	service, err := service.NewService(log, service.Config{
		ID:                   id,
		Mode:                 mode,
		AgencySize:           agencySize,
		ArangodPath:          arangodPath,
		ArangodJSPath:        arangodJSPath,
		MasterPort:           masterPort,
		RrPath:               rrPath,
		StartCoordinator:     startCoordinator,
		StartDBserver:        startDBserver,
		StartLocalSlaves:     startLocalSlaves,
		DataDir:              dataDir,
		OwnAddress:           ownAddress,
		MasterAddress:        masterAddress,
		Verbose:              verbose,
		ServerThreads:        serverThreads,
		AllPortOffsetsUnique: allPortOffsetsUnique,
		JwtSecret:            jwtSecret,
		SslKeyFile:           sslKeyFile,
		SslCAFile:            sslCAFile,
		RunningInDocker:      isRunningInDocker(),
		DockerContainerName:  dockerContainerName,
		DockerEndpoint:       dockerEndpoint,
		DockerImage:          dockerImage,
		DockerUser:           dockerUser,
		DockerGCDelay:        dockerGCDelay,
		DockerNetworkMode:    dockerNetworkMode,
		DockerPrivileged:     dockerPrivileged,
		ProjectVersion:       projectVersion,
		ProjectBuild:         projectBuild,
	}, false)
	if err != nil {
		log.Fatalf("Failed to create service: %#v", err)
	}

	// Run the service
	service.Run(rootCtx)
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
		log.Fatalf("Cannot expand '%s': %#v", s, err)
	}
	return result
}
