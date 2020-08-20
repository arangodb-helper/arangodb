//
// DISCLAIMER
//
// Copyright 2020 ArangoDB GmbH, Cologne, Germany
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

package features

import (
	"fmt"

	"github.com/arangodb/go-driver"
	flag "github.com/spf13/pflag"
)

type Version struct {
	Enterprise bool
	Version    driver.Version
}

type Feature interface {
	Register(i *flag.FlagSet) error

	Enabled(v Version) bool
}

func NewFeature(name, description string, enabled bool, check func(v Version) bool) Feature {
	return &feature{
		name:        name,
		description: description,
		enabled:     enabled,
		check:       check,
	}
}

type feature struct {
	name, description string
	enabled           bool
	check             func(v Version) bool
}

func (f *feature) Register(i *flag.FlagSet) error {
	i.BoolVar(&f.enabled, fmt.Sprintf("feature.%s", f.name), f.enabled, f.description)

	return nil
}

func (f feature) Enabled(v Version) bool {
	return f.check(v)
}
