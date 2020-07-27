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
	"context"
	"time"

	"github.com/spf13/cobra"
)

var (
	cmdAdmin = &cobra.Command{
		Use:  "admin",
		Long: `ArangoDB Starter admin commands to manage and fetch current cluster state.`,
	}

	cmdInventory = &cobra.Command{
		Use:  "inventory",
		Long: `Fetch inventory details about members including versions, and if supported, JWT Checksums`,
	}

	cmdJWT = &cobra.Command{
		Use:  "jwt",
		Long: `Cluster JWT Management. Requires Enterprise 3.7.1+`,
	}

	cmdJWTRefresh = &cobra.Command{
		Use:  "refresh",
		Run:  jwtRefresh,
		Long: `Reload local JWT Tokens from Starter JWT folder. Starter will remove obsolete keys if they are not used as Active on any member in inventory.`,
	}

	cmdJWTActivate = &cobra.Command{
		Use:  "activate",
		Run:  jwtActivate,
		Long: `Activate JWT Token. Token needs to be installed on all instances, at least in passive mode.`,
	}

	cmdInventoryLocal = &cobra.Command{
		Use:  "local",
		Run:  localInventory,
		Long: `Fetch inventory details about current starter instance members`,
	}

	cmdInventoryCluster = &cobra.Command{
		Use:  "cluster",
		Run:  clusterInventory,
		Long: `Fetch inventory details about starter instances members from cluster`,
	}

	adminOptions struct {
		starterEndpoint string
	}

	jwtToken string
)

func init() {
	f := cmdAdmin.PersistentFlags()
	f.StringVar(&adminOptions.starterEndpoint, "starter.endpoint", "http://localhost:8528", "The endpoint of the starter to connect to. E.g. http://localhost:8528")

	cmdMain.AddCommand(cmdAdmin)

	cmdAdmin.AddCommand(cmdInventory)

	cmdAdmin.AddCommand(cmdJWT)

	cmdJWTActivate.Flags().StringVar(&jwtToken, "token", "", "Token to be activated")

	cmdJWT.AddCommand(cmdJWTActivate)

	cmdJWT.AddCommand(cmdJWTRefresh)

	cmdInventory.AddCommand(cmdInventoryLocal)

	cmdInventory.AddCommand(cmdInventoryCluster)
}

func localInventory(cmd *cobra.Command, args []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := mustCreateStarterClient(adminOptions.starterEndpoint)

	i, err := c.Inventory(ctx)
	if err != nil {
		log.Fatal().Err(err).Msgf("Unable to load inventory")
	}

	for n, m := range i.Members {
		if m.Error != nil {
			log.Error().Msgf("Member %s is in failed state: %s", n.String(), m.Error)
		}
		log.Info().Msgf("Member %s, Version %s, License: %s", n.String(), m.Version.Version, m.Version.License)

		if m.Hashes != nil {
			log.Info().Msgf("Hashes:")
			log.Info().Msgf("\tJWT:")
			if a := m.Hashes.JWT.Active; a != nil {
				log.Info().Msgf("\t\tActive: %s", a.GetSHA())
			}
			if len(m.Hashes.JWT.Passive) != 0 {
				log.Info().Msgf("\t\tPassive:")
				for _, j := range m.Hashes.JWT.Passive {
					log.Info().Msgf("\t\t- %s", j.GetSHA())
				}
			}
		}
	}
}

func clusterInventory(cmd *cobra.Command, args []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := mustCreateStarterClient(adminOptions.starterEndpoint)

	i, err := c.ClusterInventory(ctx)
	if err != nil {
		log.Fatal().Err(err).Msgf("Unable to load inventory")
	}

	for pn, p := range i.Peers {
		log.Info().Msgf("Peer %s, Members %d", pn, len(p.Members))
		for n, m := range p.Members {
			if m.Error != nil {
				log.Error().Msgf("\tMember %s is in failed state: %s", n.String(), m.Error)
				continue
			}
			log.Info().Msgf("\tMember %s, Version %s, License: %s", n.String(), m.Version.Version, m.Version.License)

			if m.Hashes != nil {
				log.Info().Msgf("\tHashes:")
				log.Info().Msgf("\t\tJWT:")
				if a := m.Hashes.JWT.Active; a != nil {
					log.Info().Msgf("\t\t\tActive: %s", a.GetSHA())
				}
				if len(m.Hashes.JWT.Passive) != 0 {
					log.Info().Msgf("\t\t\tPassive:")
					for _, j := range m.Hashes.JWT.Passive {
						log.Info().Msgf("\t\t\t- %s", j.GetSHA())
					}
				}
			}
		}
	}
}

func jwtRefresh(cmd *cobra.Command, args []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := mustCreateStarterClient(adminOptions.starterEndpoint)

	log.Info().Msgf("Refreshing JWT Tokens")

	_, err := c.AdminJWTRefresh(ctx)
	if err != nil {
		log.Fatal().Msgf("Error while refreshing JWT tokens: %s", err.Error())
	}

	log.Info().Msgf("JWT Tokens refreshed")
}

func jwtActivate(cmd *cobra.Command, args []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := mustCreateStarterClient(adminOptions.starterEndpoint)

	log.Info().Msgf("Activating JWT Token")

	if jwtToken == "" {
		log.Fatal().Msgf("JWT Token not provided")
	}

	_, err := c.AdminJWTActivate(ctx, jwtToken)
	if err != nil {
		log.Fatal().Msgf("Error while activating JWT tokens: %s", err.Error())
	}

	log.Info().Msgf("JWT Token %s activated", jwtToken)
}
