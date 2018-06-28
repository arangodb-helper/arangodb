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
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	docs "github.com/arangodb-helper/go-cobra-json-docs"
)

var (
	cmdDoc = &cobra.Command{
		Use:    "doc",
		Short:  "Create commandline documentation for the ArangoDB starter",
		Run:    cmdDocRun,
		Hidden: true,
	}
	docFolder string
)

func init() {
	f := cmdDoc.Flags()
	docFolder = os.TempDir()
	f.StringVar(&docFolder, "doc.dir", docFolder, "Output folder in which documentation is created")

	cmdMain.AddCommand(cmdDoc)
}

func createDocs(cmd *cobra.Command, cmdPrefix []string) {
	if !cmd.Hidden || !cmd.IsAdditionalHelpTopicCommand() {
		nameSlice := append(cmdPrefix, cmd.Use)
		if cmd.IsAvailableCommand() {
			json, err := docs.ConvertToJSON(cmd)
			if err != nil {
				log.Fatal().Err(err).
					Str("command", cmd.Use).
					Msg("Failed to convert command to documentation")
			}
			name := strings.Join(nameSlice, "_")
			fileName := filepath.Join(docFolder, name+".json")
			os.MkdirAll(docFolder, 0755)
			if err := ioutil.WriteFile(fileName, json, 0644); err != nil {
				log.Fatal().Err(err).
					Str("command", cmd.Use).
					Str("file", fileName).
					Msg("Failed to write documentation to disk")
			}
			log.Info().
				Str("command", cmd.Use).
				Msgf("Write documentation file %s", fileName)
		}

		for _, child := range cmd.Commands() {
			createDocs(child, nameSlice)
		}
	}
}

func cmdDocRun(cmd *cobra.Command, args []string) {
	// Setup logging
	configureLogging()

	createDocs(cmdMain, nil)
}
