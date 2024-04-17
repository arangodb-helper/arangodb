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
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/coreos/go-semver/semver"
)

const (
	preReleasePrefix = "preview-"
)

var (
	versionFile string // Full path of VERSION file
	releaseType string // What type of release to create (major|minor|patch)
	ghRelease   string // Full path of github-release tool
	ghUser      string // Github account name to create release in
	ghRepo      string // Github repository name to create release in
	binFolder   string // Folder containing binaries
	preRelease  bool   // If set, mark release as preview
	dryRun      bool   // If set, do not really push a release or any git changes

	binaries = map[string]string{
		"arangodb-linux-amd64": "linux/amd64/arangodb",
		"arangodb-linux-arm64": "linux/arm64/arangodb",
	}
)

func init() {
	flag.StringVar(&versionFile, "versionfile", "./VERSION", "Path of the VERSION file")
	flag.StringVar(&releaseType, "type", "patch", "Type of release to build (major|minor|patch)")
	flag.StringVar(&ghRelease, "github-release", ".gobuild/bin/github-release", "Full path of github-release tool")
	flag.StringVar(&ghUser, "github-user", "arangodb-helper", "Github account name to create release in")
	flag.StringVar(&ghRepo, "github-repo", "arangodb", "Github repository name to create release in")
	flag.StringVar(&binFolder, "bin-folder", "./bin", "Folder containing binaries")
	flag.BoolVar(&preRelease, "prerelease", false, "If set, mark release as preview")
	flag.BoolVar(&dryRun, "dryrun", false, "If set, do not really push a release or any git changes")
}

func main() {
	flag.Parse()
	ensureGithubToken()
	checkCleanRepo()
	version := bumpVersionInFile(releaseType)
	tagName := fmt.Sprintf("v%s", version)
	make("clean")
	make("binaries")
	createSHA256Sums()
	pushDockerImages()
	gitTag(version, tagName) // push both old and new tags for backward compatibility
	githubCreateRelease(tagName)
	bumpVersionInFile("devel")
}

// ensureGithubToken makes sure the GITHUB_TOKEN envvar is set.
func ensureGithubToken() {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		p := filepath.Join(os.Getenv("HOME"), ".arangodb/github-token")
		if raw, err := os.ReadFile(p); err != nil {
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

func pushDockerImages() {
	if dryRun {
		log.Printf("Skipping pushing docker images to registry")
	} else {
		make("docker-push-version")
	}
}

func make(target string) {
	if err := run("make", target); err != nil {
		log.Fatalf("Failed to make %s: %v\n", target, err)
	}
}

func bumpVersionInFile(action string) string {
	contents, err := os.ReadFile(versionFile)
	if err != nil {
		log.Fatalf("Cannot read '%s': %v\n", versionFile, err)
	}
	version := semver.New(strings.TrimSpace(string(contents)))
	bumpVersion(version, action, preRelease)

	contents = []byte(version.String())

	if dryRun {
		log.Printf("Skipping writing VERSION file. Content: %s", string(contents))
	} else {
		if err := os.WriteFile(versionFile, contents, 0755); err != nil {
			log.Fatalf("Cannot write '%s': %v\n", versionFile, err)
		}
	}

	gitCommitAll(fmt.Sprintf("Updated to v%s", version))
	log.Printf("Updated '%s' to '%s'\n", versionFile, string(contents))

	return version.String()
}

func bumpVersion(version *semver.Version, action string, isPreRelease bool) {
	if action == "devel" {
		version.Metadata = "git"
		return
	}
	version.Metadata = ""

	currVersionIsPreRelease := strings.HasPrefix(string(version.PreRelease), preReleasePrefix)
	if isPreRelease {
		firstPreRelease := semver.PreRelease(fmt.Sprintf("%s1", preReleasePrefix))
		switch action {
		case "patch":
			if currVersionIsPreRelease {
				version.PreRelease = bumpPreReleaseIndex(version.PreRelease)
			} else {
				version.BumpPatch()
				version.PreRelease = firstPreRelease
			}
		case "minor":
			if currVersionIsPreRelease && version.Patch == 0 {
				version.PreRelease = bumpPreReleaseIndex(version.PreRelease)
			} else {
				version.BumpMinor()
				version.PreRelease = firstPreRelease
			}
		case "major":
			if currVersionIsPreRelease && version.Minor == 0 && version.Patch == 0 {
				version.PreRelease = bumpPreReleaseIndex(version.PreRelease)
			} else {
				version.BumpMajor()
				version.PreRelease = firstPreRelease
			}
		}
	} else {
		version.PreRelease = ""
		if !currVersionIsPreRelease {
			switch action {
			case "patch":
				version.BumpPatch()
			case "minor":
				version.BumpMinor()
			case "major":
				version.BumpMajor()
			}
		}
	}
}

func bumpPreReleaseIndex(preReleaseStr semver.PreRelease) semver.PreRelease {
	indexStr := strings.TrimPrefix(string(preReleaseStr), preReleasePrefix)
	index, err := strconv.Atoi(indexStr)
	if err != nil {
		log.Fatalf("Could not parse prerelease: %s", preReleaseStr)
	}
	index++
	return semver.PreRelease(fmt.Sprintf("%s%d", preReleasePrefix, index))
}

func gitCommitAll(message string) {
	if dryRun {
		log.Printf("Skipping git commit and push with message '%s'", message)
		return
	}

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

func gitTag(tags ...string) {
	if dryRun {
		log.Printf("Skipping git tag with '%+v'", tags)
		return
	}

	for _, t := range tags {
		if err := run("git", "tag", t); err != nil {
			log.Fatalf("Failed to tag: %v\n", err)
		}
	}
	if err := run("git", "push", "--tags"); err != nil {
		log.Fatalf("Failed to push tags: %v\n", err)
	}
}

func createSHA256Sums() {
	var sums []string
	for name, p := range binaries {
		blob, err := os.ReadFile(filepath.Join(binFolder, p))
		if err != nil {
			log.Fatalf("Failed to read binary '%s': %#v\n", name, err)
		}
		bytes := sha256.Sum256(blob)
		sha := hex.EncodeToString(bytes[:])
		sums = append(sums, sha+"  "+name)
	}
	sumsPath := filepath.Join(binFolder, "SHA256SUMS")
	if err := os.WriteFile(sumsPath, []byte(strings.Join(sums, "\n")+"\n"), 0644); err != nil {
		log.Fatalf("Failed to write '%s': %#v\n", sumsPath, err)
	}
}

func githubCreateRelease(version string) {
	if dryRun {
		log.Printf("Skipping github release with tag name '%s'", version)
		return
	}

	// Create draft release
	args := []string{
		"release",
		"--user", ghUser,
		"--repo", ghRepo,
		"--tag", version,
		"--draft",
	}
	if preRelease {
		args = append(args, "--pre-release")
	}
	if err := run(ghRelease, args...); err != nil {
		log.Fatalf("Failed to create github release: %v\n", err)
	}
	// Ensure release created (sometimes there is a delay between creation request and it's availability for assets upload)
	ensureReleaseCreated(version)

	// Upload binaries
	assets := map[string]string{
		"SHA256SUMS": "SHA256SUMS",
	}
	for k, v := range binaries {
		assets[k] = v
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
	if preRelease {
		args = append(args, "--pre-release")
	}
	if err := run(ghRelease, args...); err != nil {
		log.Fatalf("Failed to finalize github release: %v\n", err)
	}
}

func ensureReleaseCreated(tagName string) {
	const attemptsCount = 5
	var interval = time.Second
	var err error

	for i := 1; i <= attemptsCount; i++ {
		time.Sleep(interval)
		interval *= 2

		args := []string{
			"info",
			"--user", ghUser,
			"--repo", ghRepo,
			"--tag", tagName,
		}
		err = run(ghRelease, args...)
		if err == nil {
			return
		}
		log.Printf("attempt #%d to get release info for tag %s failed. Retry in %s...", i, tagName, interval.String())
	}

	log.Fatalf("failed to get release info for tag %s", tagName)
}

func run(cmd string, args ...string) error {
	c := exec.Command(cmd, args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}
