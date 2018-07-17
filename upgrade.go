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
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/arangodb-helper/arangodb/client"
)

var (
	cmdUpgrade = &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade an ArangoDB deployment to a new version",
		Run:   cmdUpgradeRun,
	}
	cmdRetry = &cobra.Command{
		Use:   "retry",
		Short: "Retry an operation",
		Run:   cmdShowUsage,
	}
	cmdRetryUpgrade = &cobra.Command{
		Use:   "upgrade",
		Short: "Retry a failed upgrade of an ArangoDB deployment to a new version",
		Run:   cmdRetryUpgradeRun,
	}
	upgradeOptions struct {
		starterEndpoint string
	}
	retryUpgradeOptions struct {
		starterEndpoint string
	}
)

func init() {
	f := cmdUpgrade.Flags()
	f.StringVar(&upgradeOptions.starterEndpoint, "starter.endpoint", "", "The endpoint of the starter to connect to. E.g. http://localhost:8528")

	f = cmdRetryUpgrade.Flags()
	f.StringVar(&retryUpgradeOptions.starterEndpoint, "starter.endpoint", "", "The endpoint of the starter to connect to. E.g. http://localhost:8528")

	cmdMain.AddCommand(cmdUpgrade)
	cmdMain.AddCommand(cmdRetry)
	cmdRetry.AddCommand(cmdRetryUpgrade)
}

func cmdUpgradeRun(cmd *cobra.Command, args []string) {
	runUpgrade(upgradeOptions.starterEndpoint, false, false)
}

func cmdRetryUpgradeRun(cmd *cobra.Command, args []string) {
	runUpgrade(retryUpgradeOptions.starterEndpoint, false, true)
}

func runUpgrade(starterEndpoint string, force, retry bool) {
	// Setup logging
	consoleOnly := true
	configureLogging(consoleOnly)

	// Check options
	if starterEndpoint == "" {
		log.Fatal().Msg("--starter.endpoint must be set")
	}
	ep, err := url.Parse(starterEndpoint)
	if err != nil {
		log.Fatal().Err(err).Msg("--starter.endpoint is invalid")
	}

	// Create starter client
	c, err := client.NewArangoStarterClient(*ep)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create Starter client")
	}
	ctx := context.Background()
	if retry {
		if err := c.RetryDatabaseUpgrade(ctx); err != nil {
			log.Fatal().Err(err).Msg("Failed to retry database automatic upgrade")
		}
		log.Info().Msg("Database automatic upgrade has been restarted")
	} else {
		if err := c.StartDatabaseUpgrade(ctx, force); err != nil {
			log.Fatal().Err(err).Msg("Failed to start database automatic upgrade")
		}
		log.Info().Msg("Database automatic upgrade has been started")
	}

	// Wait for the upgrade to finish
	remaining := ""
	finished := ""
	for {
		status, err := c.UpgradeStatus(ctx)
		if err != nil {
			log.Error().Err(err).Msg("Failed to fetch upgrade status")
		}
		if status.Failed {
			log.Error().Str("reason", status.Reason).Msg("Database upgrade has failed")
			return
		}
		if status.Ready {
			log.Info().Msg("Database upgrade has finished")
			return
		}
		r, f := formatServerStatusList(status.ServersRemaining), formatServerStatusList(status.ServersUpgraded)
		if remaining != r || finished != f {
			remaining, finished = r, f
			log.Info().Msgf("Servers upgraded: %s, remaining servers: %s", finished, remaining)
		}
		time.Sleep(time.Second)
	}
}

// formatServerStatusList formats the given server status list in a human readable format.
func formatServerStatusList(list []client.UpgradeStatusServer) string {
	counts := make(map[client.ServerType]int)
	for _, e := range list {
		counts[e.Type] = counts[e.Type] + 1
	}
	if len(counts) == 0 {
		return "none"
	}
	strList := make([]string, 0, len(counts))
	for t, c := range counts {
		name := string(t)
		if c > 1 {
			name = name + "s"
		}
		strList = append(strList, fmt.Sprintf("%d %s", c, name))
	}
	sort.Slice(strList, func(i, j int) bool {
		// Sort past the number
		a := strings.Split(strList[i], " ")[1]
		b := strings.Split(strList[j], " ")[1]
		return a < b
	})
	return strings.Join(strList, ", ")
}
