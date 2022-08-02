//
// DISCLAIMER
//
// Copyright 2017-2022 ArangoDB GmbH, Cologne, Germany
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

package service

import (
	"bytes"
	"io/ioutil"
	"os"
	"strings"
	"text/template"
	"time"

	"github.com/rs/zerolog"
)

const commandFileHeaderTmpl = `# This is the startup command used by the ArangoDB Starter.
# Generated on {{ .DateTime }} for informational purposes only.
# 
# Updates to this file will not have any effect.
# If you want to make configuration changes, please adjust the Starter's command line options directly.
#

`

func createCommandFileHeader() string {
	type info struct {
		DateTime string
	}

	t := template.Must(template.New("header").Parse(commandFileHeaderTmpl))
	var buf bytes.Buffer

	err := t.Execute(&buf, info{DateTime: time.Now().Format(time.RFC3339)})
	if err != nil {
		panic(err)
	}
	return buf.String()
}

// writeCommandFile writes the command used to start a server in a file with given path.
func writeCommandFile(log zerolog.Logger, filename string, args []string) {
	content := createCommandFileHeader() + strings.Join(args, " \\\n") + "\n"
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		if err := ioutil.WriteFile(filename, []byte(content), 0755); err != nil {
			log.Error().Err(err).Msgf("Failed to write command to %s", filename)
		}
	}
}
