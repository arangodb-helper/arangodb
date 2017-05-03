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
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/coreos/go-semver/semver"
)

var (
	versionFile string // Full path of VERSION file
	releaseType string // What type of release to create (major|minor|patch)
	ghRelease   string // Full path of github-release tool
	ghUser      string // Github account name to create release in
	ghRepo      string // Github repository name to create release in
	binFolder   string // Folder containing binaries
)

func init() {
	flag.StringVar(&versionFile, "versionfile", "./VERSION", "Path of the VERSION file")
	flag.StringVar(&releaseType, "type", "patch", "Type of release to build (major|minor|patch)")
	flag.StringVar(&ghRelease, "github-release", ".gobuild/bin/github-release", "Full path of github-release tool")
	flag.StringVar(&ghUser, "github-user", "arangodb-helper", "Github account name to create release in")
	flag.StringVar(&ghRepo, "github-repo", "ArangoDBStarter", "Github repository name to create release in")
	flag.StringVar(&binFolder, "bin-folder", "./bin", "Folder containing binaries")
}

func main() {
	flag.Parse()
	ensureGithubToken()
	checkCleanRepo()
	version := bumpVersion(releaseType)
	make("clean")
	make("binaries")
	make("docker-push-version")
	gitTag(version)
	githubCreateRelease(version)
	bumpVersion("devel")
}

// ensureGithubToken makes sure the GITHUB_TOKEN envvar is set.
func ensureGithubToken() {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		p := filepath.Join(os.Getenv("HOME"), ".arangodb/github-token")
		if raw, err := ioutil.ReadFile(p); err != nil {
			log.Fatalf("Failed to release '%s': %v", p, err)
		} else {
			token = strings.TrimSpace(string(raw))
			os.Setenv("GITHUB_TOKEN", token)
		}
	}
}

func checkCleanRepo() {
	output, err := exec.Command("git", "status", "--porcelain").Output()
	if err != nil {
		log.Fatalf("Failed to check git status: %v\n", err)
	}
	if strings.TrimSpace(string(output)) != "" {
		log.Fatal("Repository has uncommitted changes\n")
	}
}

func make(target string) {
	if err := run("make", target); err != nil {
		log.Fatalf("Failed to make %s: %v\n", target, err)
	}
}

func bumpVersion(action string) string {
	contents, err := ioutil.ReadFile(versionFile)
	if err != nil {
		log.Fatalf("Cannot read '%s': %v\n", versionFile, err)
	}
	version := semver.New(strings.TrimSpace(string(contents)))

	switch action {
	case "patch":
		version.BumpPatch()
	case "minor":
		version.BumpMinor()
	case "major":
		version.BumpMajor()
	case "devel":
		version.Metadata = "git"
	}
	contents = []byte(version.String())

	if err := ioutil.WriteFile(versionFile, contents, 0755); err != nil {
		log.Fatalf("Cannot write '%s': %v\n", versionFile, err)
	}

	gitCommitAll(fmt.Sprintf("Updated to %s", version))
	log.Printf("Updated '%s' to '%s'\n", versionFile, string(contents))

	return version.String()
}

func gitCommitAll(message string) {
	args := []string{
		"commit",
		"--all",
		"-m", message,
	}
	if err := run("git", args...); err != nil {
		log.Fatalf("Failed to commit: %v\n", err)
	}
	if err := run("git", "push"); err != nil {
		log.Fatalf("Failed to push commit: %v\n", err)
	}
}

func gitTag(version string) {
	if err := run("git", "tag", version); err != nil {
		log.Fatalf("Failed to tag: %v\n", err)
	}
	if err := run("git", "push", "--tags"); err != nil {
		log.Fatalf("Failed to push tags: %v\n", err)
	}
}

func githubCreateRelease(version string) {
	// Create draft release
	args := []string{
		"release",
		"--user", ghUser,
		"--repo", ghRepo,
		"--tag", version,
		"--draft",
	}
	if err := run(ghRelease, args...); err != nil {
		log.Fatalf("Failed to create github release: %v\n", err)
	}
	// Upload binaries
	assets := map[string]string{
		"arangodb-darwin-amd64":      "darwin/amd64/arangodb",
		"arangodb-linux-amd64":       "linux/amd64/arangodb",
		"arangodb-windows-amd64.exe": "windows/amd64/arangodb.exe",
	}
	for name, file := range assets {
		args := []string{
			"upload",
			"--user", ghUser,
			"--repo", ghRepo,
			"--tag", version,
			"--name", name,
			"--file", filepath.Join(binFolder, file),
		}
		if err := run(ghRelease, args...); err != nil {
			log.Fatalf("Failed to upload asset '%s': %v\n", name, err)
		}
	}
	// Finalize release
	args = []string{
		"edit",
		"--user", ghUser,
		"--repo", ghRepo,
		"--tag", version,
	}
	if err := run(ghRelease, args...); err != nil {
		log.Fatalf("Failed to finalize github release: %v\n", err)
	}
}

func run(cmd string, args ...string) error {
	c := exec.Command(cmd, args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}
