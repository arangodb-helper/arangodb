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

	"github.com/spf13/cobra"
)

var (
	cmdRemove = &cobra.Command{
		Use:   "remove",
		Short: "Remove something",
		Run:   cmdShowUsage,
	}
	cmdRemoveStarter = &cobra.Command{
		Use:   "starter",
		Short: "Remove a starter from the cluster",
		Run:   cmdRemoveStarterRun,
	}
	removeStarterOptions struct {
		starterEndpoint string
		starterID       string
		force           bool
	}
)

func init() {
	f := cmdRemoveStarter.Flags()
	f.StringVar(&removeStarterOptions.starterEndpoint, "starter.endpoint", "", "The endpoint of the starter to connect to. E.g. http://localhost:8528")
	f.StringVar(&removeStarterOptions.starterID, "starter.id", "", "The ID of the starter to remove")
	f.BoolVar(&removeStarterOptions.force, "force", false, "If set to true, the starter will be removed even if the servers cannot be properly shutdown")

	cmdMain.AddCommand(cmdRemove)
	cmdRemove.AddCommand(cmdRemoveStarter)
}

func cmdRemoveStarterRun(cmd *cobra.Command, args []string) {
	// Setup logging
	consoleOnly := true
	configureLogging(consoleOnly)

	// Create starter client
	c := mustCreateStarterClient(removeStarterOptions.starterEndpoint)

	// Fetch the ID of the starter for which the endpoint is given
	ctx := context.Background()
	info, err := c.ID(ctx)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to fetch ID from starter")
	}

	// Compare ID with requested.
	if removeStarterOptions.starterID == "" || removeStarterOptions.starterID == info.ID {
		// Shutdown (with goodbye) the starter at given endpoint
		goodbye := true
		if err := c.Shutdown(ctx, goodbye); err != nil {
			log.Fatal().Err(err).Msg("Removing starter from cluster failed")
		} else {
			log.Info().Msg("Starter has been shutdown and removed from cluster")
		}
	} else {
		// Remove another starter from the cluster
		if err := c.RemovePeer(ctx, removeStarterOptions.starterID, removeStarterOptions.force); err != nil {
			log.Fatal().Err(err).Msg("Removing starter from cluster failed")
		} else {
			log.Info().Msg("Starter has been removed from cluster")
		}
	}
}
