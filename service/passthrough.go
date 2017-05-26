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

import "strings"

type PassthroughOption struct {
	Name   string
	Values struct {
		All          []string
		Coordinators []string
		DBServers    []string
		Agents       []string
	}
}

var (
	// forbiddenPassthroughOptions holds a list of options that are not allowed to be overriden.
	forbiddenPassthroughOptions = []string{
		"agency.activate",
		"agency.endpoint",
		"agency.my-address",
		"agency.size",
		"agency.supervision",
		"cluster.agency-endpoint",
		"cluster.my-address",
		"cluster.my-role",
		"cluster.my-local-info",
		"database.directory",
		"javascript.startup-directory",
		"javascript.app-path",
		"rocksdb.encryption-keyfile",
		"server.endpoint",
		"server.authentication",
		"server.jwt-secret",
		"server.storage-engine",
		"ssl.cafile",
		"ssl.keyfile",
	}
)

// valueForServerType returns the value for the given option for a specific server type.
// If no value is given for the specific server type, any value for `all` is returned.
func (o *PassthroughOption) valueForServerType(serverType ServerType) []string {
	var result []string
	switch serverType {
	case ServerTypeSingle:
		result = o.Values.All
	case ServerTypeCoordinator:
		result = o.Values.Coordinators
	case ServerTypeDBServer:
		result = o.Values.DBServers
	case ServerTypeAgent:
		result = o.Values.Agents
	}
	if len(result) > 0 {
		return result
	}
	return o.Values.All
}

// IsForbidden returns true if the option cannot be overwritten.
func (o *PassthroughOption) IsForbidden() bool {
	for _, x := range forbiddenPassthroughOptions {
		if x == o.Name {
			return true
		}
	}
	return false
}

// FormattedOptionName returns the option ready to be used in a command line argument,
// prefixed with `--`.
func (o *PassthroughOption) FormattedOptionName() string {
	return "--" + o.Name
}

// sectionName returns the name of the configuration section this option belongs to.
func (o *PassthroughOption) sectionName() string {
	return strings.SplitN(o.Name, ".", 2)[0]
}

// sectionKey returns the name of this option within its configuration section.
func (o *PassthroughOption) sectionKey() string {
	parts := strings.SplitN(o.Name, ".", 2)
	if len(parts) > 1 {
		return parts[1]
	}
	return ""
}

func (c *Config) passthroughOptionValuesForServerType(name string, serverType ServerType) []string {
	for _, ptOpt := range c.PassthroughOptions {
		if ptOpt.Name != name {
			continue
		}
		values := ptOpt.valueForServerType(serverType)
		if len(values) > 0 {
			return values
		}
		return nil
	}
	return nil
}
