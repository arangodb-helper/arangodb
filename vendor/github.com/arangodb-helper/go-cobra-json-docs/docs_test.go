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
	"reflect"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func TestCreateOptionDescription(t *testing.T) {
	buildFlag := func(creator func(fs *pflag.FlagSet)) *pflag.Flag {
		fs := &pflag.FlagSet{}
		creator(fs)
		var result *pflag.Flag
		fs.VisitAll(func(f *pflag.Flag) {
			if result == nil {
				result = f
			} else {
				t.Fatal("More than 1 flag found")
			}
		})
		if result == nil {
			t.Fatal("Flag not found")
		}
		return result
	}

	tests := []struct {
		Flag         *pflag.Flag
		Expected     OptionDescription
		ExpectedName string
	}{
		// Booleans
		{
			Flag: buildFlag(func(fs *pflag.FlagSet) { fs.Bool("bool_1", false, "Usage of bool_1") }),
			Expected: OptionDescription{
				Default:     false,
				Description: "Usage of bool_1",
				Type:        "bool",
			},
			ExpectedName: "bool_1",
		},
		{
			Flag: buildFlag(func(fs *pflag.FlagSet) { fs.Bool("bool_2", true, "Usage of bool_2") }),
			Expected: OptionDescription{
				Default:     true,
				Description: "Usage of bool_2",
				Type:        "bool",
			},
			ExpectedName: "bool_2",
		},
		{
			Flag: buildFlag(func(fs *pflag.FlagSet) { fs.BoolP("bool_3", "x", true, "Usage of bool_3") }),
			Expected: OptionDescription{
				Default:     true,
				Description: "Usage of bool_3",
				Type:        "bool",
			},
			ExpectedName: "bool_3",
		},
		// Bool slices
		{
			Flag: buildFlag(func(fs *pflag.FlagSet) { fs.BoolSlice("bs_1", nil, "Usage of bs_1") }),
			Expected: OptionDescription{
				Default:     []bool{},
				Description: "Usage of bs_1",
				Type:        "[]bool",
			},
			ExpectedName: "bs_1",
		},
		{
			Flag: buildFlag(func(fs *pflag.FlagSet) { fs.BoolSlice("bs_2", []bool{true, true, false}, "Usage of bs_2") }),
			Expected: OptionDescription{
				Default:     []bool{true, true, false},
				Description: "Usage of bs_2",
				Type:        "[]bool",
			},
			ExpectedName: "bs_2",
		},
		// Duration
		{
			Flag: buildFlag(func(fs *pflag.FlagSet) { fs.Duration("dur_1", time.Hour, "Usage of dur_1") }),
			Expected: OptionDescription{
				Default:     time.Hour,
				Description: "Usage of dur_1",
				Type:        "duration",
			},
			ExpectedName: "dur_1",
		},
		{
			Flag: buildFlag(func(fs *pflag.FlagSet) { fs.Duration("dur_2", 0, "Usage of dur_2") }),
			Expected: OptionDescription{
				Default:     time.Duration(0),
				Description: "Usage of dur_2",
				Type:        "duration",
			},
			ExpectedName: "dur_2",
		},
		{
			Flag: buildFlag(func(fs *pflag.FlagSet) { fs.DurationP("dur_3", "x", time.Second, "Usage of dur_3") }),
			Expected: OptionDescription{
				Default:     time.Second,
				Description: "Usage of dur_3",
				Type:        "duration",
			},
			ExpectedName: "dur_3",
		},
		// Ints
		{
			Flag: buildFlag(func(fs *pflag.FlagSet) { fs.Int("int_1", 7, "Usage of int_1") }),
			Expected: OptionDescription{
				Default:     7,
				Description: "Usage of int_1",
				Type:        "int",
			},
			ExpectedName: "int_1",
		},
		{
			Flag: buildFlag(func(fs *pflag.FlagSet) { fs.Int("int_2", 0, "Usage of int_2") }),
			Expected: OptionDescription{
				Default:     0,
				Description: "Usage of int_2",
				Type:        "int",
			},
			ExpectedName: "int_2",
		},
		{
			Flag: buildFlag(func(fs *pflag.FlagSet) { fs.IntP("int_3", "x", 3, "Usage of int_3") }),
			Expected: OptionDescription{
				Default:     3,
				Description: "Usage of int_3",
				Type:        "int",
			},
			ExpectedName: "int_3",
		},
		// Int slice
		{
			Flag: buildFlag(func(fs *pflag.FlagSet) { fs.IntSlice("foo", nil, "Usage of foo") }),
			Expected: OptionDescription{
				Default:     []int{},
				Description: "Usage of foo",
				Type:        "[]int",
			},
			ExpectedName: "foo",
		},
		{
			Flag: buildFlag(func(fs *pflag.FlagSet) { fs.IntSlice("foo", []int{0, 11, 42}, "Usage of foo") }),
			Expected: OptionDescription{
				Default:     []int{0, 11, 42},
				Description: "Usage of foo",
				Type:        "[]int",
			},
			ExpectedName: "foo",
		},
		// Strings
		{
			Flag: buildFlag(func(fs *pflag.FlagSet) { fs.String("string_1", "", "Usage of string_1") }),
			Expected: OptionDescription{
				Default:     "",
				Description: "Usage of string_1",
				Type:        "string",
			},
			ExpectedName: "string_1",
		},
		{
			Flag: buildFlag(func(fs *pflag.FlagSet) { fs.String("string_2", "someValue", "Usage of string_2") }),
			Expected: OptionDescription{
				Default:     "someValue",
				Description: "Usage of string_2",
				Type:        "string",
			},
			ExpectedName: "string_2",
		},
		// String slice
		{
			Flag: buildFlag(func(fs *pflag.FlagSet) { fs.StringSlice("foo", nil, "Usage of foo") }),
			Expected: OptionDescription{
				Default:     []string{},
				Description: "Usage of foo",
				Type:        "[]string",
			},
			ExpectedName: "foo",
		},
		{
			Flag: buildFlag(func(fs *pflag.FlagSet) { fs.StringSlice("foo", []string{"a", "b", "c"}, "Usage of foo") }),
			Expected: OptionDescription{
				Default:     []string{"a", "b", "c"},
				Description: "Usage of foo",
				Type:        "[]string",
			},
			ExpectedName: "foo",
		},
	}
	for _, test := range tests {
		actual, name, err := createOptionDescription(test.Flag)
		if err != nil {
			t.Errorf("createOptionDescription returned an error: %v", err)
		} else {
			if !reflect.DeepEqual(actual, test.Expected) {
				t.Errorf("createOptionDescription failed. Found '%#v', expected '%#v'", actual, test.Expected)
			}
			if name != test.ExpectedName {
				t.Errorf("createOptionDescription.name failed. Found '%s', expected '%s'", name, test.ExpectedName)
			}
		}
	}
}

func TestConvertToJSON(t *testing.T) {
	c1 := &cobra.Command{}
	c1.Flags().String("foo", "", "Usage of foo")
	c1.Flags().String("defString", "theDefault", "Usage of defString")
	c1.Flags().String("server.something", "", "Usage of server.something")

	tests := []struct {
		Command  *cobra.Command
		Expected string
	}{
		{
			Command:  c1,
			Expected: `{"defString":{"default":"theDefault","description":"Usage of defString","hidden":false,"section":"","type":"string"},"foo":{"default":"","description":"Usage of foo","hidden":false,"section":"","type":"string"},"server.something":{"default":"","description":"Usage of server.something","hidden":false,"section":"server","type":"string"}}`,
		},
	}
	for i, test := range tests {
		encoded, err := ConvertToJSON(test.Command)
		if err != nil {
			t.Errorf("ConvertToJSON failed on test %d", i)
		} else if string(encoded) != test.Expected {
			t.Errorf("Convert failed. Found '%s', expected '%s'", string(encoded), test.Expected)
		}
	}
}
