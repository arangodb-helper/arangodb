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
	"strings"

	"github.com/spf13/cobra"

	service "github.com/arangodb-helper/arangodb/service"
)

var (
	cmdAuth = &cobra.Command{
		Use:   "auth",
		Short: "ArangoDB authentication helper commands",
		Run:   cmdShowUsage,
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
	}
)

func init() {
	cmdMain.AddCommand(cmdAuth)
	cmdAuth.AddCommand(cmdAuthHeader)
	cmdAuth.AddCommand(cmdAuthToken)

	pf := cmdAuth.PersistentFlags()
	pf.StringVar(&authOptions.jwtSecretFile, "auth.jwt-secret", "", "name of a plain text file containing a JWT secret used for server authentication")
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
	token, err := service.CreateJwtToken(jwtSecret)
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
