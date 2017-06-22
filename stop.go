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
	"time"

	"github.com/arangodb-helper/arangodb/client"
	"github.com/spf13/cobra"
)

var (
	cmdStop = &cobra.Command{
		Use:   "stop",
		Short: "Stop a ArangoDB starter",
		Run:   cmdStopRun,
	}
)

func init() {
	cmdMain.AddCommand(cmdStop)
}

func cmdStopRun(cmd *cobra.Command, args []string) {
	// Setup logging
	configureLogging()

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

	// Shutdown starter
	rootCtx := context.Background()
	ctx, cancel := context.WithTimeout(rootCtx, time.Minute)
	err = client.Shutdown(ctx, false)
	cancel()
	if err != nil {
		log.Fatalf("Failed to shutdown starter: %#v", err)
	}

	// Wait for starter to be really gone
	for {
		ctx, cancel := context.WithTimeout(rootCtx, time.Second)
		_, err := client.Version(ctx)
		cancel()
		if err != nil {
			break
		}
		time.Sleep(time.Millisecond * 100)
	}
}
