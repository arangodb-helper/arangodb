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

package service

import (
	"fmt"
	"io"
	"io/ioutil"
	"strings"
)

var confHeader = `# ArangoDB configuration file
#
# Documentation:
# https://www.arangodb.com/docs/stable/administration-configuration.html
#

`

type configFile []*configSection

// WriteTo writes the configuration sections to the given writer.
func (cf configFile) WriteTo(w io.Writer) (int64, error) {
	x := int64(0)
	n, err := w.Write([]byte(confHeader))
	if err != nil {
		return x, maskAny(err)
	}
	x += int64(n)
	for _, section := range cf {
		n, err := section.WriteTo(w)
		if err != nil {
			return x, maskAny(err)
		}
		x += n
	}
	return x, nil
}

// FindSection searches for a section with given name and returns it.
// If not found, nil is returned.
func (cf configFile) FindSection(sectionName string) *configSection {
	for _, sect := range cf {
		if sect.Name == sectionName {
			return sect
		}
	}
	return nil
}

type configSection struct {
	Name     string
	Settings map[string]string
}

// WriteTo writes the configuration section to the given writer.
func (s *configSection) WriteTo(w io.Writer) (int64, error) {
	lines := []string{"[" + s.Name + "]"}
	for k, v := range s.Settings {
		lines = append(lines, fmt.Sprintf("%s = %s", k, v))
	}
	lines = append(lines, "")
	n, err := w.Write([]byte(strings.Join(lines, "\n")))
	return int64(n), maskAny(err)
}

// readConfigFile loads the content of a config file.
func readConfigFile(path string) (configFile, error) {
	content, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, maskAny(err)
	}
	lines := strings.Split(string(content), "\n")
	config := configFile{}
	var section *configSection
	for _, line := range lines {
		idx := strings.Index(line, "#")
		if idx >= 0 {
			line = line[:idx]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			name := strings.TrimSpace(line[1 : len(line)-2])
			section = &configSection{
				Name:     name,
				Settings: make(map[string]string),
			}
			config = append(config, section)
		} else if section != nil {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 {
				continue
			}
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			section.Settings[key] = value
		}
	}
	return config, nil
}
