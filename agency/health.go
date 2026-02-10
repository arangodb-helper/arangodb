// DISCLAIMER
//
// # Copyright 2017-2026 ArangoDB GmbH, Cologne, Germany
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
package agency

import (
	"context"
	"fmt"
	"time"

	driver_http "github.com/arangodb/go-driver/v2/connection"
)

func AreAgentsHealthy(ctx context.Context, conns []driver_http.Connection) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	testKey := []string{"arango", "Plan"}
	var result any

	for _, conn := range conns {
		client := &client{conn: conn}

		if err := client.ReadKey(ctx, testKey, &result); err != nil {
			if IsKeyNotFound(err) {
				continue
			}
			return fmt.Errorf("agency health check failed: %w", err)
		}
	}

	return nil
}
