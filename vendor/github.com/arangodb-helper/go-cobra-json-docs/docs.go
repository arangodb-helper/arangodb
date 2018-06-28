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

package docs

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// OptionDescriptions contains the descriptions for all commandline options of a command.
type OptionDescriptions map[string]OptionDescription

// OptionDescription contains a properties that describe a commandline option.
type OptionDescription struct {
	Default     interface{} `json:"default"`
	Description string      `json:"description"`
	Hidden      bool        `json:"hidden"`
	Section     string      `json:"section"`
	Type        string      `json:"type"`
	Values      string      `json:"values,omitempty"`
}

// convertPFlagType converts type names used by pflag to type names understood by the JSON format.
func convertPFlagType(pflagType string) string {
	if strings.HasSuffix(pflagType, "Slice") {
		return "[]" + convertPFlagType(pflagType[:len(pflagType)-len("Slice")])
	}
	switch pflagType {
	default:
		return pflagType
	}
}

type convFunc func(fs *pflag.FlagSet, name string) (interface{}, error)

var (
	convFuncs = map[string]convFunc{
		"bool":        func(fs *pflag.FlagSet, name string) (interface{}, error) { return fs.GetBool(name) },
		"boolSlice":   func(fs *pflag.FlagSet, name string) (interface{}, error) { return fs.GetBoolSlice(name) },
		"duration":    func(fs *pflag.FlagSet, name string) (interface{}, error) { return fs.GetDuration(name) },
		"int":         func(fs *pflag.FlagSet, name string) (interface{}, error) { return fs.GetInt(name) },
		"intSlice":    func(fs *pflag.FlagSet, name string) (interface{}, error) { return fs.GetIntSlice(name) },
		"int32":       func(fs *pflag.FlagSet, name string) (interface{}, error) { return fs.GetInt32(name) },
		"int64":       func(fs *pflag.FlagSet, name string) (interface{}, error) { return fs.GetInt64(name) },
		"string":      func(fs *pflag.FlagSet, name string) (interface{}, error) { return fs.GetString(name) },
		"stringSlice": func(fs *pflag.FlagSet, name string) (interface{}, error) { return fs.GetStringSlice(name) },
	}
)

// getDefaultValue extracts the default value from the given flag.
func getDefaultValue(flag *pflag.Flag) (interface{}, error) {
	fs := &pflag.FlagSet{}
	fs.AddFlag(flag)
	cf, found := convFuncs[flag.Value.Type()]
	if found {
		if result, err := cf(fs, flag.Name); err == nil {
			return result, nil
		} else {
			return nil, err
		}
	}
	return nil, fmt.Errorf("No converter function found for type '%s'", flag.Value.Type())
}

// createOptionDescription creates a description for the given flag.
// Returns description, name, error.
func createOptionDescription(flag *pflag.Flag) (OptionDescription, string, error) {
	name := flag.Name
	nameParts := strings.Split(name, ".")
	section := ""
	if len(nameParts) > 1 {
		section = nameParts[0]
	}
	defValue, err := getDefaultValue(flag)
	if err != nil {
		return OptionDescription{}, "", err
	}
	d := OptionDescription{
		Default:     defValue,
		Description: flag.Usage,
		Section:     section,
		Hidden:      flag.Hidden,
		Type:        convertPFlagType(flag.Value.Type()),
	}
	return d, name, nil
}

// CreateOptionDescriptions creates a map of descriptions for all the commandline
// options of the given command.
func CreateOptionDescriptions(cmd *cobra.Command) (OptionDescriptions, error) {
	result := make(OptionDescriptions)

	flags := cmd.Flags()
	var lastErr error
	flags.VisitAll(func(flag *pflag.Flag) {
		d, name, err := createOptionDescription(flag)
		if err != nil {
			lastErr = err
		}
		result[name] = d
	})
	if lastErr != nil {
		return nil, lastErr
	}

	return result, nil
}

// ConvertToJSON converts all the commandline options of the given command to JSON.
func ConvertToJSON(cmd *cobra.Command) ([]byte, error) {
	descriptions, err := CreateOptionDescriptions(cmd)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(descriptions)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}
