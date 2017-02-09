package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/coreos/go-semver/semver"
)

var (
	versionFile string
	releaseType string
)

func init() {
	flag.StringVar(&versionFile, "versionfile", "./VERSION", "Path of the VERSION file")
	flag.StringVar(&releaseType, "type", "patch", "Type of release to build (major|minor|patch)")
}

func main() {
	flag.Parse()
	checkCleanRepo()
	version := bumpVersion(releaseType)
	make("clean")
	make("docker-push-version")
	gitTag(version)
	bumpVersion("devel")
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

func run(cmd string, args ...string) error {
	c := exec.Command(cmd, args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}
