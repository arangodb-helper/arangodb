//
// DISCLAIMER
//
// Copyright 2017 ArangoDB GmbH, Cologne, Germany
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
	"os"
	"os/exec"
	"time"

	"github.com/arangodb-helper/arangodb/client"
	svc "github.com/arangodb-helper/arangodb/service"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var (
	cmdStart = &cobra.Command{
		Use:   "start",
		Short: "Start the ArangoDB starter in the background",
		Run:   cmdStartRun,
	}
	waitForServers bool
)

func init() {
	f := cmdStart.Flags()
	f.BoolVar(&waitForServers, "starter.wait", false, "If set, the (parent) starter waits until all database servers are ready before exiting.")

	cmdMain.AddCommand(cmdStart)
}

func cmdStartRun(cmd *cobra.Command, args []string) {
	log.Infof("Starting %s version %s, build %s in the background", projectName, projectVersion, projectBuild)

	// Setup logging
	configureLogging()

	// Build service
	service, bsCfg := mustPrepareService(true)

	// Find executable
	exePath, err := os.Executable()
	if err != nil {
		log.Fatalf("Cannot find executable path: %#v", err)
	}

	// Build command line
	childArgs := make([]string, 0, len(os.Args))
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if f.Changed {
			switch f.Name {
			case "ssl.auto-key", "ssl.auto-server-name", "ssl.auto-organization", "ssl.keyfile", "starter.wait":
				// Do not pass these along
			default:
				a := "--" + f.Name
				value := f.Value.String()
				if value != "" {
					a = a + "=" + value
				}
				childArgs = append(childArgs, a)
			}
		}
	})
	if bsCfg.SslKeyFile != "" {
		childArgs = append(childArgs, "--ssl.keyfile="+bsCfg.SslKeyFile)
	}

	log.Debugf("Found child args: %#v", childArgs)

	// Start detached child
	c := exec.Command(exePath, childArgs...)
	if err := c.Start(); err != nil {
		log.Fatalf("Failed to start detached child: %#v", err)
	}
	c.Process.Release()

	// Create starter client
	scheme := "http"
	if sslAutoKeyFile || sslKeyFile != "" {
		scheme = "https"
	}
	starterURL, err := url.Parse(fmt.Sprintf("%s://127.0.0.1:%d", scheme, masterPort))
	if err != nil {
		log.Fatalf("Failed to create starter URL: %#v", err)
	}
	client, err := client.NewArangoStarterClient(*starterURL)
	if err != nil {
		log.Fatalf("Failed to create starter client: %#v", err)
	}

	// Wait for detached starter to be alive
	log.Info("Waiting for starter API to be available...")
	rootCtx := context.Background()
	for {
		ctx, cancel := context.WithTimeout(rootCtx, time.Second)
		_, err := client.Version(ctx)
		cancel()
		if err == nil {
			break
		}
		time.Sleep(time.Millisecond * 100)
	}

	// Wait until all servers ready (if needed)
	if waitForServers {
		log.Info("Waiting for database instances to be available...")
		for {
			var err error
			ctx, cancel := context.WithTimeout(rootCtx, time.Second)
			list, err := client.Processes(ctx)
			cancel()
			if err == nil && list.ServersStarted {
				// Start says it has started the servers, now wait for servers to be up.
				allUp := true
				for _, server := range list.Servers {
					ctx, cancel := context.WithTimeout(rootCtx, time.Second)
					up, _, _, _, _, _, _ := service.TestInstance(ctx, svc.ServerType(server.Type), server.IP, server.Port, nil)
					cancel()
					if !up {
						allUp = false
						break
					}
				}
				if allUp {
					break
				}
			}
			time.Sleep(time.Millisecond * 100)
		}
		log.Info("Database instances are available.")
	}
}
