package main

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"

	service "github.com/neunhoef/ArangoDBStarter/service"
)

// Configuration data with defaults:

var agencySize = 3
var arangodExecutable = "/usr/sbin/arangod"
var arangodJSstartup = "/usr/share/arangodb3/js"
var masterPort = 4000
var rrPath = ""
var startCoordinator = true
var startDBserver = true
var dataDir = "./"
var ownAddress = ""
var masterAddress = ""
var verbose = false
var dockerEndpoint string
var dockerImage string
var dockerUser string

// Stuff for the signal handling:

var sigChannel chan os.Signal

func handleSignal(stopChan chan bool) {
	signalCount := 0
	for s := range sigChannel {
		signalCount++
		fmt.Println("Received signal:", s)
		if signalCount > 1 {
			os.Exit(1)
		}
		stopChan <- true
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
				fmt.Println("Could not read directory", basePath,
					"to look for executable.")
			}
			d.Close()
		} else {
			fmt.Println("Could not open directory", basePath,
				"to look for executable.")
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
			arangodExecutable, _ = filepath.Abs(filepath.FromSlash(p))
			if p == "build/bin/arangod" {
				arangodJSstartup, _ = filepath.Abs("js")
			} else {
				arangodJSstartup, _ = filepath.Abs(
					filepath.FromSlash(filepath.Dir(p) + "/../share/arangodb3/js"))
			}
			return
		}
	}
}

func usage() {
	fmt.Printf(`Usage of %s:
  --dataDir path
        directory to store all data (default "%s")
  --join addr
        join a cluster with master at address addr (default "")
  --agencySize int
        number of agents in agency (default %d)
  --ownAddress addr
        address under which this server is reachable, needed for 
        the case of --agencySize 1 in the master
  --dockerImage imagename
	      name of the Docker image to use to launch arangod instances
				(default "", which means do not use Docker
  --dockerEndpoint imagename
	      Endpoint used to reach the docker daemon (e.g. /var/run/docker.sock)
  --dockerUser name
        use the given name as user to run the Docker container (default "")
  --masterPort int
        port for arangodb master (default %d)
  --arangod path
        path to arangod executable (default "%s")
  --jsDir path
        path to JS library directory (default "%s")
  --startCoordinator bool
        should a coordinator instance be started (default %t)
  --startDBserver bool
        should a dbserver instance be started (default %t)
  --rr path
        path to rr executable to use if non-empty (default "%s")
  --verbose bool
        show more information (default %t)
	
`, os.Args[0], dataDir, agencySize, masterPort, arangodExecutable,
		arangodJSstartup, startCoordinator, startDBserver, rrPath, verbose)
}

func parseBool(option string, value string) (bool, error) {
	if value == "true" || value == "1" || value == "yes" || value == "y" ||
		value == "Y" || value == "YES" || value == "TRUE" || value == "True" {
		return true, nil
	}
	if value == "false" || value == "0" || value == "no" || value == "n" ||
		value == "N" || value == "NO" || value == "FALSE" || value == "False" {
		return false, nil
	}
	fmt.Println("Option", option, "needs a boolean value (true/false/1/0/yes/no)",
		"and not", value)
	return true, errors.New("boolean value expected")
}

func parseInt(option string, value string) (int64, error) {
	i, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		fmt.Println("Option", option, "needs an integer value and not", value)
		return 0, err
	}
	return i, nil
}

func main() {
	// Find executable and jsdir default in a platform dependent way:
	findExecutable()

	// Command line arguments:
	for i := 1; i < len(os.Args); i++ {
		if os.Args[i] == "-h" || os.Args[i] == "--help" {
			usage()
			return
		}
	}
	if len(os.Args) == 2 {
		fmt.Println("Need none or at least two arguments.")
		usage()
		return
	}
	for i := 1; i < len(os.Args)-1; i += 2 {
		switch os.Args[i] {
		case "--agencySize":
			if i, e := parseInt(os.Args[i], os.Args[i+1]); e == nil {
				agencySize = int(i)
			}
		case "--masterPort":
			if i, e := parseInt(os.Args[i], os.Args[i+1]); e == nil {
				masterPort = int(i)
			}
		case "--dataDir":
			dataDir = os.Args[i+1]
		case "--arangod":
			arangodExecutable = os.Args[i+1]
		case "--jsDir":
			arangodJSstartup = os.Args[i+1]
		case "--startCoordinator":
			if b, e := parseBool(os.Args[i], os.Args[i+1]); e == nil {
				startCoordinator = b
			}
		case "--startDBserver":
			if b, e := parseBool(os.Args[i], os.Args[i+1]); e == nil {
				startDBserver = b
			}
		case "--rr":
			rrPath = os.Args[i+1]
		case "--ownAddress":
			ownAddress = os.Args[i+1]
		case "--join":
			masterAddress = os.Args[i+1]
		case "--verbose":
			if b, e := parseBool(os.Args[i], os.Args[i+1]); e == nil {
				verbose = b
			}
		case "--dockerEndpoint":
			dockerEndpoint = os.Args[i+1]
		case "--dockerImage":
			dockerImage = os.Args[i+1]
		case "--dockerUser":
			dockerUser = os.Args[i+1]
		default:
			fmt.Println("Error: Wrong option", os.Args[i])
			usage()
			return
		}
	}

	// Some plausibility checks:
	if agencySize%2 == 0 || agencySize <= 0 {
		fmt.Println("Error: agencySize needs to be a positive, odd number.")
		return
	}
	if agencySize == 1 && ownAddress == "" {
		fmt.Println("Error: if agencySize==1, ownAddress must be given.")
		return
	}
	if dockerImage != "" && rrPath != "" {
		fmt.Println("Error: using --dockerImage and --rr is not possible.")
		return
	}
	if verbose {
		fmt.Println("Using", arangodExecutable, "as default arangod executable.")
		fmt.Println("Using", arangodJSstartup, "as default JS dir.")
	}

	// Sort out work directory:
	if len(dataDir) == 0 {
		dataDir = "./"
	}
	dataDir, _ = filepath.Abs(dataDir)
	if dataDir[len(dataDir)-1] != os.PathSeparator {
		dataDir = dataDir + string(os.PathSeparator)
	}
	err := os.MkdirAll(dataDir, 0755)
	if err != nil {
		fmt.Println("Cannot create data directory", dataDir, ", giving up.")
		return
	}

	// Interrupt signal:
	sigChannel = make(chan os.Signal)
	stopChan := make(chan bool)
	signal.Notify(sigChannel, os.Interrupt, syscall.SIGTERM)
	go handleSignal(stopChan)

	// Create service
	service, err := service.NewService(service.ServiceConfig{
		AgencySize:        agencySize,
		ArangodExecutable: arangodExecutable,
		ArangodJSstartup:  arangodJSstartup,
		MasterPort:        masterPort,
		RrPath:            rrPath,
		StartCoordinator:  startCoordinator,
		StartDBserver:     startDBserver,
		DataDir:           dataDir,
		OwnAddress:        ownAddress,
		MasterAddress:     masterAddress,
		Verbose:           verbose,
		DockerEndpoint:    dockerEndpoint,
		DockerImage:       dockerImage,
		DockerUser:        dockerUser,
	})
	if err != nil {
		fmt.Printf("Failed to create service: %#v\n", err)
		os.Exit(1)
	}

	// Run the service
	service.Run(stopChan)
}
