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

package main

import (
	"fmt"
	"io/ioutil"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	service "github.com/arangodb-helper/arangodb/service"
)

var (
	cmdAuth = &cobra.Command{
		Use:               "auth",
		Short:             "ArangoDB authentication helper commands",
		PersistentPreRunE: persistentAuthPreFunE,
		Run:               cmdShowUsage,
	}
	cmdAuthHeader = &cobra.Command{
		Use:   "header",
		Short: "Create a full HTTP Authorization header for accessing an ArangoDB server",
		Run:   cmdAuthHeaderRun,
	}
	cmdAuthToken = &cobra.Command{
		Use:   "token",
		Short: "Create a JWT authentication token for accessing an ArangoDB server",
		Run:   cmdAuthTokenRun,
	}
	authOptions struct {
		jwtSecretFile string
		user          string
		paths         []string
		exp           string
		expDuration   time.Duration

		fieldsOverride    []string
		fieldsOverrideMap map[string]interface{}
	}
)

func init() {
	cmdMain.AddCommand(cmdAuth)
	cmdAuth.AddCommand(cmdAuthHeader)
	cmdAuth.AddCommand(cmdAuthToken)

	pf := cmdAuth.PersistentFlags()
	pf.StringVar(&authOptions.jwtSecretFile, "auth.jwt-secret", "", "name of a plain text file containing a JWT secret used for server authentication")
	pf.StringVar(&authOptions.user, "auth.user", "", "name of a user to authenticate as. If empty, 'super-user' authentication is used")
	pf.StringSliceVar(&authOptions.paths, "auth.paths", nil, "a list of allowed pathes. The path must not include the '_db/DBNAME' prefix.")
	pf.StringVar(&authOptions.exp, "auth.exp", "", "a time in which token should expire - based on current time in UTC. Supported units: h, m, s (default)")
	pf.StringSliceVar(&authOptions.fieldsOverride, "auth.fields", nil, "a list of additional fields set in the token. This flags override one auto-generated in token")
}

// mustAuthCreateJWTToken creates a the JWT token based on authentication options.
// On error the process is exited with a non-zero exit code.
func mustAuthCreateJWTToken() string {
	authOptions.jwtSecretFile = mustExpand(authOptions.jwtSecretFile)

	if authOptions.jwtSecretFile == "" {
		log.Fatal().Msg("A JWT secret file is required. Set --auth.jwt-secret option.")
	}
	content, err := ioutil.ReadFile(authOptions.jwtSecretFile)
	if err != nil {
		log.Fatal().Err(err).Msgf("Failed to read JWT secret file '%s'", authOptions.jwtSecretFile)
	}
	jwtSecret := strings.TrimSpace(string(content))
	token, err := service.CreateJwtToken(jwtSecret, authOptions.user, "", authOptions.paths, authOptions.expDuration, authOptions.fieldsOverrideMap)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create JWT token")
	}
	return token
}

// cmdAuthHeaderRun prints a JWT authorization header on stdout and exits.
func cmdAuthHeaderRun(cmd *cobra.Command, args []string) {
	token := mustAuthCreateJWTToken()
	fmt.Printf("%s: %s%s\n", service.AuthorizationHeader, service.BearerPrefix, token)
}

// cmdAuthTokenRun prints a JWT authorization token on stdout and exits.
func cmdAuthTokenRun(cmd *cobra.Command, args []string) {
	token := mustAuthCreateJWTToken()
	fmt.Println(token)
}

func persistentAuthPreFunE(cmd *cobra.Command, args []string) error {
	cmdMain.PersistentPreRun(cmd, args)

	if authOptions.exp != "" {
		d, err := durationParser(authOptions.exp, "s")
		if err != nil {
			return err
		}

		if d < 0 {
			return fmt.Errorf("negative duration under --auth.exp is not allowed")
		}

		authOptions.expDuration = d
	}

	authOptions.fieldsOverrideMap = map[string]interface{}{}

	for _, field := range authOptions.fieldsOverride {
		tokens := strings.Split(field, "=")
		if len(tokens) == 0 {
			return fmt.Errorf("invalid format of the field override: `%s`", field)
		}

		key := tokens[0]
		value := strings.Join(tokens[1:], "=")
		var calculatedValue interface{} = value

		switch value {
		case "true":
			calculatedValue = true
		case "false":
			calculatedValue = false
		default:
			if i, err := strconv.Atoi(value); err == nil {
				calculatedValue = i
			}
		}

		authOptions.fieldsOverrideMap[key] = calculatedValue
	}

	return nil
}

func durationParser(duration string, defaultUnit string) (time.Duration, error) {
	if d, err := time.ParseDuration(duration); err == nil {
		return d, nil
	} else {
		if !strings.HasPrefix(err.Error(), "time: missing unit in duration ") {
			return 0, err
		}

		duration = fmt.Sprintf("%s%s", duration, defaultUnit)

		if d, err := time.ParseDuration(duration); err == nil {
			return d, nil
		} else {
			return 0, err
		}
	}
}
