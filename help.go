//
// DISCLAIMER
//
// Copyright 2018-2024 ArangoDB GmbH, Cologne, Germany
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
	"os"
	"strings"

	"github.com/fatih/color"
)

// --docker.container missing
func showDockerContainerNameMissingHelp() {
	showFatalHelp(
		"Cannot find docker container name.",
		"",
		"How to solve this:",
		"1 - Add a commandline argument:",
		"",
		"    `arangodb ... --docker.container=<name of container you run starter in>`",
		"",
	)
}

// --cluster.agency-size invalid.
func showClusterAgencySizeInvalidHelp() {
	showFatalHelp(
		"Cluster agency size is invalid cluster.agency-size needs to be a positive, odd number.",
		"",
		"How to solve this:",
		"1 - Use a positive, odd number as agency size:",
		"",
		"    `arangodb ... --cluster.agency-size=<1, 3 or 5...>`",
		"",
	)
}

// --cluster.agency-size=1 without specifying --starter.address.
func showClusterAgencySize1WithoutAddressHelp() {
	showFatalHelp(
		"With a cluster agency size of 1, a starter address is required.",
		"",
		"How to solve this:",
		"1 - Add a commandline argument:",
		"",
		"    `arangodb ... --starter.address=<IP address of current machine>`",
		"",
	)
}

// setting both --docker.image and --server.rr is not possible.
func showDockerImageWithRRIsNotAllowedHelp() {
	showFatalHelp(
		"Using RR is not possible with docker.",
		"",
		"How to solve this:",
		"1 - Remove `--server.rr=...` commandline argument.",
		"",
	)
}

// setting both --docker.net-host and --docker.net-mode is not possible
func showDockerNetHostAndNotModeNotBothAllowedHelp() {
	showFatalHelp(
		"It is not allowed to set `--docker.net-host` and `--docker.net-mode` at the same time.",
		"",
		"How to solve this:",
		"1 - Remove one of these two commandline arguments.",
		"",
	)
}

// Arangod is not found at given path.
func showArangodExecutableNotFoundHelp(arangodPath string) {
	showFatalHelp(
		fmt.Sprintf("Cannot find `arangod` (expected at `%s`).", arangodPath),
		"",
		"How to solve this:",
		"1 - Install ArangoDB locally or run the ArangoDB starter in docker. (see README for details).",
		"",
	)
}

// cannnot specify both `--ssl.auto-key` and `--ssl.keyfile`
func showSslAutoKeyAndKeyFileNotBothAllowedHelp() {
	showFatalHelp(
		"Specifying both `--ssl.auto-key` and `--ssl.keyfile` is not allowed.",
		"",
		"How to solve this:",
		"1 - Remove one of these two commandline arguments.",
		"",
	)
}

// showFatalHelp logs a title and prints additional usages
// underneeth and the exit with code 1.
// Backticks in the lines are colored yellow.
func showFatalHelp(title string, lines ...string) {
	log.Error().Msg(highlight(title))
	content := strings.Join(lines, "\n")
	fmt.Println(highlight(content))
	os.Exit(1)
}

func highlight(content string) string {
	parts := strings.Split(content, "`")
	for i, p := range parts {
		if i%2 == 1 {
			parts[i] = color.YellowString(p)
		}
	}
	return strings.Join(parts, "")
}
