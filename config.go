//
// DISCLAIMER
//
// Copyright 2017-2023 ArangoDB GmbH, Cologne, Germany
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

package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/pkg/errors"
	"github.com/spf13/pflag"
	"gopkg.in/ini.v1"

	"github.com/arangodb-helper/arangodb/service/options"
)

// loadCfgFromFile returns config loaded from file if file exists, nil otherwise
func loadCfgFromFile(cfgFilePath string) (*ini.File, error) {
	if _, err := os.Stat(cfgFilePath); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, errors.WithStack(err)
	}

	f, err := ini.ShadowLoad(cfgFilePath)
	if err != nil {
		return nil, errors.Wrap(err, "while loading config file")
	}

	return f, nil
}

// findFlagByName searches for a flag in provided flag sets
func findFlagByName(n string, flagSets ...*pflag.FlagSet) *pflag.Flag {
	for _, fs := range flagSets {
		if f := fs.Lookup(n); f != nil {
			return f
		}
	}
	return nil
}

// trySetFlagFromConfig tries to find flagName in flag sets and set its value.
// If flag not found it tries to set passthrough option value.
// If flagName doesn't match passthrough prefixes, it will return an error.
func trySetFlagFromConfig(flagName string, k *ini.Key, flagSets ...*pflag.FlagSet) error {
	f := findFlagByName(flagName, flagSets...)
	if f != nil {
		if f.Changed {
			return nil
		}
		err := f.Value.Set(k.Value())
		if err != nil {
			return errors.Wrapf(err, "invalid value for key %s", flagName)
		}
		return nil
	}

	prefix, confPrefix, targ, err := passthroughPrefixes.Lookup(flagName)
	if err != nil {
		return errors.Wrapf(err, "invalid key %s", flagName)
	}
	if confPrefix != nil {
		valuePtr := confPrefix.FieldSelector(passthroughOpts, targ)
		if *valuePtr == nil {
			*valuePtr = k.ValueWithShadows()
		} else {
			*valuePtr = append(*valuePtr, k.ValueWithShadows()...)
		}
		if options.IsPersistentOption(targ) {
			passthroughOpts.PersistentOptions.Add(prefix, targ, valuePtr)
		}
		return nil
	}
	return fmt.Errorf("unknown key %s", flagName)
}

// loadFlagValuesFromConfig loads config and assigns its values to flag set (only if flag value wasn't changed before)
func loadFlagValuesFromConfig(cfgFilePath string, fs, persistentFs *pflag.FlagSet) {
	if strings.ToLower(cfgFilePath) == "none" {
		// Skip loading: special value to ignore config
		return
	}

	configFile, err := loadCfgFromFile(cfgFilePath)
	if err != nil || configFile == nil && cfgFilePath != defaultConfigFilePath {
		log.Fatal().Err(err).Msgf("Could not load config file %s", cfgFilePath)
		return
	}
	if configFile == nil {
		return
	}

	for _, currSection := range configFile.Sections() {
		for _, k := range currSection.Keys() {
			flagName := k.Name()
			if currSection.Name() != ini.DefaultSection {
				flagName = currSection.Name() + "." + flagName
			}

			err = trySetFlagFromConfig(flagName, k, fs, persistentFs)
			if err != nil {
				log.Fatal().Err(err).Msg("Invalid config")
				return
			}
		}
	}
}

// sanityCheckPassThroughArgs will print a warning if user does not specify value for option,
// example: --args.all.log.line-number --args.all.log.performance=true
func sanityCheckPassThroughArgs(fs, persistentFs *pflag.FlagSet) {
	sanityCheck := func(flag *pflag.Flag) {
		if found, _, _ := passthroughPrefixes.Lookup(flag.Name); found != nil {
			if flag.Value == nil {
				return
			}
			if sliceVal, ok := flag.Value.(pflag.SliceValue); ok {
				slice := sliceVal.GetSlice()
				if len(slice) > 0 && strings.HasPrefix(slice[0], "--") {
					log.Warn().Msgf("Possible wrong usage of pass-through argument --%s: its value %s looks like separate argument.", flag.Name, slice[0])
				}
			}
		}
	}
	fs.Visit(sanityCheck)
	persistentFs.Visit(sanityCheck)
}
