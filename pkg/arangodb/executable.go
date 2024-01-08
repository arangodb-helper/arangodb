//
// DISCLAIMER
//
// Copyright 2024 ArangoDB GmbH, Cologne, Germany
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

package arangodb

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/rs/zerolog"
)

// FindExecutable uses a platform dependent approach to find an executable
// with given process name.
func FindExecutable(log zerolog.Logger, processName, defaultPath string, useLocalBin bool) (executablePath string, isBuild bool) {
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

	if useLocalBin {
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
