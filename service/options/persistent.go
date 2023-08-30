//
// DISCLAIMER
//
// Copyright 2023 ArangoDB GmbH, Cologne, Germany
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

package options

import (
	"fmt"
	"strconv"
	"strings"
)

func IsPersistentOption(arg string) bool {
	switch arg {
	case "database.extended-names", "database.extended-names-databases",
		"default-language", "icu-language":
		return true
	}
	return false
}

type PersistentOptions []PersistentOption

func (po *PersistentOptions) Add(prefix, arg string, val *[]string) {
	if po == nil {
		*po = make([]PersistentOption, 0)
	}
	*po = append(*po, PersistentOption{
		Prefix: prefix,
		Arg:    arg,
		Value:  val,
	})
}

func (po *PersistentOptions) ValidateCompatibility(otherOpts *PersistentOptions) error {
	if po == nil || otherOpts == nil {
		return nil
	}

	var reports []string
	for _, o := range *po {
		for _, other := range *otherOpts {
			if !o.canBeChangedWith(other) {
				reports = append(reports, fmt.Sprintf("--%s.%s", o.Prefix, o.Arg))
			}
		}
	}

	if len(reports) > 0 {
		return fmt.Errorf("it is impossible to change persistent options: %s", strings.Join(reports, ", "))
	}
	return nil
}

type PersistentOption struct {
	Prefix string    `json:"prefix"`
	Arg    string    `json:"arg"`
	Value  *[]string `json:"value"`
}

// asBool returns true if value can be parsed as bool and equals v.
// empty value assumed to be true
func (p PersistentOption) asBool() (bool, error) {
	normValue := p.normValue()
	if normValue == "" {
		return true, nil
	}

	return strconv.ParseBool(normValue)
}

func (p PersistentOption) normValue() string {
	if p.Value == nil {
		return ""
	}
	var normValue string
	// for persistent options only last value is important
	if p.Value != nil && len(*p.Value) > 0 {
		normValue = (*p.Value)[len(*p.Value)-1]
	}
	return strings.TrimSpace(normValue)
}

func (p PersistentOption) canBeChangedWith(other PersistentOption) bool {
	if p.Arg != other.Arg {
		return true
	}

	switch p.Arg {
	case "default-language", "icu-language":
		return p.normValue() == other.normValue()
	case "database.extended-names", "database.extended-names-databases":
		oldVal, err := p.asBool()
		if err != nil {
			return true
		}
		newVal, err := other.asBool()
		if err != nil {
			return true
		}
		return oldVal == newVal || !oldVal && newVal
	}
	return true
}
