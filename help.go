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

// --sync.server.keyfile
func showSyncMasterServerKeyfileMissingHelp() {
	showFatalHelp(
		"A TLS certificate used for the HTTPS connection of the arangosync syncmaster is missing.",
		"",
		"How to solve this:",
		"1 - If you do not have TLS CA certificate certificate, create it using the following command:",
		"",
		"    `arangosync create tls ca \\",
		"        --cert=<yourfolder>/tls-ca.crt --key=<yourfolder>/tls-ca.key`",
		"",
		"2 - Distribute the resulting `tls-ca.crt` file to all machines that you run the starer on.",
		"3 - Create a TLS certificate certificate, using the following command:",
		"",
		"    `arangosync create tls keyfile \\",
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
		"    `arangosync create client-auth ca --cert=<yourfolder>/client-auth-ca.crt --key=<yourfolder>/client-auth-ca.key`",
		"",
		"2 - Distribute the resulting `client-auth-ca.crt` file to all machines that you run the starer on.",
		"3 - Add a commandline argument:",
		"",
		"    `arangodb ... --sync.server.client-cafile=<yourfolder>/client-auth-ca.crt`",
		"",
	)
}

// showFatalHelp logs a title and prints additional usages
// underneeth and the exit with code 1.
// Backticks in the lines are colored yellow.
func showFatalHelp(title string, lines ...string) {
	log.Error(title)
	content := strings.Join(lines, "\n")
	parts := strings.Split(content, "`")
	for i, p := range parts {
		if i%2 == 1 {
			parts[i] = color.YellowString(p)
		}
	}
	fmt.Println(strings.Join(parts, ""))
	os.Exit(1)
}
