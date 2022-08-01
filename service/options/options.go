//
// DISCLAIMER
//
// Copyright 2021 ArangoDB GmbH, Cologne, Germany
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
// Author Adam Janikowski
//

package options

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/arangodb-helper/arangodb/pkg/definitions"
)

type Configuration struct {
	lock sync.Mutex

	All          ConfigurationType
	Coordinators ConfigurationType
	DBServers    ConfigurationType
	Agents       ConfigurationType
	AllSync      ConfigurationType
	SyncMasters  ConfigurationType
	SyncWorkers  ConfigurationType
}

func NewConfiguration() Configuration {
	return Configuration{
		All:          NewConfigurationType(),
		Coordinators: NewConfigurationType(),
		DBServers:    NewConfigurationType(),
		Agents:       NewConfigurationType(),
		AllSync:      NewConfigurationType(),
		SyncMasters:  NewConfigurationType(),
		SyncWorkers:  NewConfigurationType(),
	}
}

func NewConfigurationType() ConfigurationType {
	return ConfigurationType{Args: map[string]*[]string{}, Envs: map[string]*[]string{}}
}

func (p *Configuration) ArgsForServerType(serverType definitions.ServerType) map[string][]string {
	m := map[string][]string{}

	z := p.ByProcessType(serverType)

	for k, v := range z.Args {
		m[k] = stringListCopy(*v)
	}

	z = p.ByServerType(serverType)

	for k, v := range z.Args {
		m[k] = append(m[k], stringListCopy(*v)...)
	}

	for k := range m {
		dedup := map[string]bool{}
		var args []string

		for _, v := range m[k] {
			if _, ok := dedup[v]; ok {
				continue
			}

			dedup[v] = true
			args = append(args, v)
		}

		m[k] = args
	}

	return m
}

func (p *Configuration) EnvsForServerType(serverType definitions.ServerType) map[string]string {
	m := map[string]string{}

	z := p.ByServerType(serverType)

	for k, v := range z.Envs {
		l := *v
		if len(l) == 0 {
			continue
		}
		m[k] = l[len(l)-1]
	}

	if len(m) > 0 {
		return m
	}

	z = p.ByProcessType(serverType)

	for k, v := range z.Envs {
		l := *v
		if len(l) == 0 {
			continue
		}
		m[k] = l[len(l)-1]
	}

	return m
}

func (p *Configuration) ArgByProcessTypeAndName(serverType definitions.ServerType, key string) *[]string {
	return p.argFromType(p.ByProcessType(serverType), key)
}

func (p *Configuration) ArgByServerTypeAndName(serverType definitions.ServerType, key string) *[]string {
	return p.argFromType(p.ByServerType(serverType), key)
}

func (p *Configuration) argFromType(t *ConfigurationType, key string) *[]string {
	p.lock.Lock()
	defer p.lock.Unlock()

	if k, ok := t.Args[key]; ok {
		return k
	} else {
		z := make([]string, 0)
		t.Args[key] = &z
		return &z
	}
}

func (p *Configuration) EnvByProcessTypeAndName(serverType definitions.ServerType, key string) *[]string {
	return p.envFromType(p.ByProcessType(serverType), key)
}

func (p *Configuration) EnvByServerTypeAndName(serverType definitions.ServerType, key string) *[]string {
	return p.envFromType(p.ByServerType(serverType), key)
}

func (p *Configuration) envFromType(t *ConfigurationType, key string) *[]string {
	p.lock.Lock()
	defer p.lock.Unlock()

	if k, ok := t.Envs[key]; ok {
		return k
	} else {
		z := make([]string, 0)
		t.Envs[key] = &z
		return &z
	}
}

func (p *Configuration) ByServerType(serverType definitions.ServerType) *ConfigurationType {
	switch serverType {
	case definitions.ServerTypeSingle:
		return &p.All
	case definitions.ServerTypeCoordinator:
		return &p.Coordinators
	case definitions.ServerTypeDBServer, definitions.ServerTypeResilientSingle:
		return &p.DBServers
	case definitions.ServerTypeAgent:
		return &p.Agents
	case definitions.ServerTypeSyncMaster:
		return &p.SyncMasters
	case definitions.ServerTypeSyncWorker:
		return &p.SyncWorkers
	default:
		return nil
	}
}

func (p *Configuration) ByProcessType(serverType definitions.ServerType) *ConfigurationType {
	switch serverType.ProcessType() {
	case definitions.ProcessTypeArangod:
		return &p.All
	case definitions.ProcessTypeArangoSync:
		return &p.AllSync
	default:
		return nil
	}
}

type ConfigurationType struct {
	Args map[string]*[]string
	Envs map[string]*[]string
}

type ConfigurationFlag struct {
	Key       string
	CleanKey  string
	Extension string
	Usage     string
	Value     *[]string

	Deprecated bool
}

type ConfigurationPrefixes map[string]ConfigurationPrefix

func (c ConfigurationPrefixes) Lookup(key string) (*ConfigurationPrefix, string, error) {
	for n, prefix := range c {
		p := fmt.Sprintf("%s.", n)
		if strings.HasPrefix(key, p) {
			targ := strings.TrimPrefix(key, p)
			if forbiddenOptions.IsForbidden(targ) {
				return nil, "", fmt.Errorf("option --%s is essential to the starters behavior and cannot be overwritten", targ)
			}
			prefix := prefix
			return &prefix, targ, nil
		}
	}
	return nil, "", nil
}

func (c ConfigurationPrefixes) Parse(args ...string) (*Configuration, []ConfigurationFlag, error) {
	var f []ConfigurationFlag
	config := NewConfiguration()

	flags := map[string]bool{}

	for _, arg := range args {
		arg = strings.SplitN(arg, "=", 2)[0]
		if !strings.HasPrefix(arg, "--") {
			continue
		}

		ckey := strings.TrimPrefix(arg, "--")
		prefix, targ, err := c.Lookup(ckey)
		if err != nil {
			return nil, nil, err
		}
		if prefix == nil {
			continue
		}

		if _, ok := flags[arg]; ok {
			continue
		} else {
			flags[arg] = true
		}

		f = append(f, ConfigurationFlag{
			Key:        arg,
			CleanKey:   ckey,
			Extension:  targ,
			Usage:      prefix.Usage(targ),
			Value:      prefix.FieldSelector(&config, targ),
			Deprecated: prefix.Deprecated,
		})
	}

	return &config, f, nil
}

func (c ConfigurationPrefixes) UsageHint() string {
	maxNameLen := 0
	for n, prefix := range c {
		if !prefix.Deprecated {
			if len(n) > maxNameLen {
				maxNameLen = len(n)
			}
		}
	}

	parts := make([]string, 0)
	for n, prefix := range c {
		if !prefix.Deprecated {
			postfix := "<xxx>=<value>"
			pad := maxNameLen - len(n) + len(postfix) + 8
			parts = append(parts,
				fmt.Sprintf("%s--%s.%-*s%s", strings.Repeat(" ", 6), n, pad, postfix, prefix.Usage(postfix)),
			)
		}
	}

	sort.Strings(parts)
	passthroughUsageHelp := "Passing through other database options:\n"
	passthroughUsageHelp += strings.Join(parts, "\n")
	return passthroughUsageHelp
}

type ConfigurationPrefix struct {
	Usage         func(key string) string
	FieldSelector func(p *Configuration, key string) *[]string
	Deprecated    bool
}

func stringListCopy(a []string) []string {
	z := make([]string, len(a))

	copy(z, a)

	return z
}
