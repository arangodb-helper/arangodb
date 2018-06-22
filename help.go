//
// DISCLAIMER
//
// Copyright 2018 ArangoDB GmbH, Cologne, Germany
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

// arangosync is not allowed with given starter mode.
func showArangoSyncNotAllowedWithModeHelp(mode string) {
	showFatalHelp(
		fmt.Sprintf("ArangoSync is not supported in combination with mode '%s'\n", mode),
		"",
		"How to solve this:",
		"1 - Use the cluster starter mode:",
		"",
		"    `arangodb --starter.mode=cluster --starter.sync ...`",
		"",
	)
}

// ArangoSync is not found at given path.
func showArangoSyncExecutableNotFoundHelp(arangosyncPath string) {
	showFatalHelp(
		fmt.Sprintf("Cannot find `arangosync` (expected at `%s`).", arangosyncPath),
		"",
		"How to solve this:",
		"1 - Install ArangoSync locally or run the ArangoDB starter in docker. (see README for details).",
		"    Make sure to use an Enterprise version of ArangoDB.",
		"",
	)
}

// --sync.server.keyfile is missing
func showSyncMasterServerKeyfileMissingHelp() {
	showFatalHelp(
		"A TLS certificate used for the HTTPS connection of the arangosync syncmaster is missing.",
		"",
		"How to solve this:",
		"1 - If you do not have TLS CA certificate certificate, create it using the following command:",
		"",
		"    `arangodb create tls ca \\",
		"        --cert=<yourfolder>/tls-ca.crt --key=<yourfolder>/tls-ca.key`",
		"",
		"2 - Distribute the resulting `tls-ca.crt` file to all machines (in both datacenters)  that you run the starter on.",
		"3 - Create a TLS certificate certificate, using the following command:",
		"",
		"    `arangodb create tls keyfile \\",
		"        --cacert=<yourfolder>/tls-ca.crt --cakey=<yourfolder>/tls-ca.key \\",
		"        --keyfile=<yourfolder>/tls.keyfile \\",
		"        --host=<current host address/name>`",
		"",
		"4 - Add a commandline argument:",
		"",
		"    `arangodb ... --sync.server.keyfile=<yourfolder>/tls.keyfile`",
		"",
	)
}

// --sync.server.client-cafile is missing
func showSyncMasterClientCAFileMissingHelp() {
	showFatalHelp(
		"A CA certificate used for client authentication of the arangosync syncmaster is missing.",
		"",
		"How to solve this:",
		"1 - Create a client authentication CA certificate using the following command:",
		"",
		"    `arangodb create client-auth ca --cert=<yourfolder>/client-auth-ca.crt --key=<yourfolder>/client-auth-ca.key`",
		"",
		"2 - Distribute the resulting `client-auth-ca.crt` file to all machines (in both datacenters) that you run the starter on.",
		"3 - Add a commandline argument:",
		"",
		"    `arangodb ... --sync.server.client-cafile=<yourfolder>/client-auth-ca.crt`",
		"",
	)
}

// --sync.master.jwt-secret is missing
func showSyncMasterJWTSecretMissingHelp() {
	showFatalHelp(
		"A JWT secret used for authentication of the arangosync syncworkers at the syncmaster is missing.",
		"",
		"How to solve this:",
		"1 - If needed, create JWT secret file using the following command:",
		"",
		"    `arangodb create jwt-secret --secret=<yourfolder>/syncmaster.jwtsecret`",
		"",
		"2 - Distribute the resulting `syncmaster.jwtsecret` file to all machines in the current datacenter, that you run the starter on.",
		"3 - Add a commandline argument:",
		"",
		"    `arangodb ... --sync.master.jwt-secret=<yourfolder>/syncmaster.jwtsecret`",
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
