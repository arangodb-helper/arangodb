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
	"testing"

	"github.com/spf13/cobra"

	"github.com/arangodb-helper/arangodb/pkg/definitions"
	"github.com/stretchr/testify/require"
)

func Test_Args(t *testing.T) {
	prefixes := ConfigurationPrefixes{
		"args.all": {
			Usage: func(arg, key string) string {
				return "usage"
			},
			FieldSelector: func(p *Configuration, key string) *[]string {
				return p.ArgByProcessTypeAndName(definitions.ServerTypeAgent, key)
			},
		},
		"args.dbservers": {
			Usage: func(arg, key string) string {
				return "usage"
			},
			FieldSelector: func(p *Configuration, key string) *[]string {
				return p.ArgByServerTypeAndName(definitions.ServerTypeDBServer, key)
			},
		},
		"envs.all": {
			Usage: func(arg, key string) string {
				return "usage"
			},
			FieldSelector: func(p *Configuration, key string) *[]string {
				return p.EnvByProcessTypeAndName(definitions.ServerTypeAgent, key)
			},
		},
	}

	t.Run("Without args", func(t *testing.T) {
		c, p, err := prefixes.Parse()
		require.NoError(t, err)
		require.NotNil(t, c)
		require.Len(t, p, 0)
	})

	t.Run("With multi args", func(t *testing.T) {
		args := []string{"--args.all.zzz=1", "--args.dbservers.zzz=2"}

		c, p, err := prefixes.Parse(args...)
		require.NoError(t, err)
		require.NotNil(t, c)
		require.Len(t, p, 2)

		cmd := &cobra.Command{}

		f := cmd.Flags()

		for _, flag := range p {
			f.StringSliceVar(flag.Value, flag.CleanKey, nil, flag.Usage)
		}

		cmd.SetArgs(args)

		require.NoError(t, cmd.Execute())

		parsedArgs := c.ArgsForServerType(definitions.ServerTypeDBServer)

		require.Len(t, parsedArgs, 1)
		require.Len(t, parsedArgs["zzz"], 2)
		require.Equal(t, parsedArgs["zzz"][0], "1")
		require.Equal(t, parsedArgs["zzz"][1], "2")
	})

	t.Run("With multi args - reorder", func(t *testing.T) {
		args := []string{"--args.dbservers.zzz=2", "--args.all.zzz=1"}

		c, p, err := prefixes.Parse(args...)
		require.NoError(t, err)
		require.NotNil(t, c)
		require.Len(t, p, 2)

		cmd := &cobra.Command{}

		f := cmd.Flags()

		for _, flag := range p {
			f.StringSliceVar(flag.Value, flag.CleanKey, nil, flag.Usage)
		}

		cmd.SetArgs(args)

		require.NoError(t, cmd.Execute())

		parsedArgs := c.ArgsForServerType(definitions.ServerTypeDBServer)

		require.Len(t, parsedArgs, 1)
		require.Len(t, parsedArgs["zzz"], 2)
		require.Equal(t, parsedArgs["zzz"][0], "1")
		require.Equal(t, parsedArgs["zzz"][1], "2")
	})

	t.Run("With args", func(t *testing.T) {
		c, p, err := prefixes.Parse("--args.all.zzz")
		require.NoError(t, err)
		require.NotNil(t, c)
		require.Len(t, p, 1)

		*p[0].Value = append(*p[0].Value, "test")

		require.Len(t, c.All.Args, 1)
		require.Contains(t, c.All.Args, "zzz")
		require.Len(t, *c.All.Args["zzz"], 1)
		require.Equal(t, (*c.All.Args["zzz"])[0], "test")
	})

	t.Run("With envs", func(t *testing.T) {
		c, p, err := prefixes.Parse("--envs.all.zzz")
		require.NoError(t, err)
		require.NotNil(t, c)
		require.Len(t, p, 1)

		*p[0].Value = append(*p[0].Value, "test")

		require.Len(t, c.All.Envs, 1)
		require.Contains(t, c.All.Envs, "zzz")
		require.Len(t, *c.All.Envs["zzz"], 1)
		require.Equal(t, (*c.All.Envs["zzz"])[0], "test")
	})
}
