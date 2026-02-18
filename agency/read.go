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
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"time"
)

func (c *client) Read(ctx context.Context, key []string, out any) error {
	if c == nil {
		return fmt.Errorf("agency client is nil")
	}
	if c.conn == nil {
		return fmt.Errorf("agency client connection is nil")
	}
	if len(key) == 0 {
		return ErrKeyNotFound
	}

	// Agency read API expects the request body to be an array of key arrays directly
	// Format: [["key", "path", "to", "key"]]
	reqBody := [][]string{key}

	// Retry on connection-level errors (connection refused, timeout, EOF) to handle
	// agency endpoint failover. The RoundRobinEndpoints rotates to the next
	// endpoint on each new request, so retrying naturally tries a different agent.
	var rawResponse any
	var lastErr error
	for attempt := 0; attempt < c.endpointCount; attempt++ {
		// Exponential backoff: wait before retry (except on first attempt)
		if attempt > 0 {
			backoffDuration := time.Duration(math.Pow(2, float64(attempt-1))) * 100 * time.Millisecond
			// Cap backoff at 2 seconds
			if backoffDuration > 2*time.Second {
				backoffDuration = 2 * time.Second
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoffDuration):
			}
		}

		req, err := c.conn.NewRequest(http.MethodPost, "/_api/agency/read")
		if err != nil {
			return fmt.Errorf("failed to create agency read request: %w", err)
		}
		if err := req.SetBody(reqBody); err != nil {
			return fmt.Errorf("failed to set request body: %w", err)
		}

		resp, err := c.conn.Do(ctx, req, &rawResponse)
		if err != nil {
			if isConnectionError(err) && attempt < c.endpointCount-1 {
				lastErr = err
				continue
			}
			return fmt.Errorf("agency read request failed: %w", err)
		}
		if resp == nil {
			return ErrKeyNotFound
		}
		lastErr = nil
		break
	}
	if lastErr != nil {
		return fmt.Errorf("agency read request failed after %d attempts: %w", c.endpointCount, lastErr)
	}

	// Agency read API can return either:
	// 1. An array: [<value_at_key_path>] - where the first element is a map containing the agency tree
	// 2. A map directly: {<agency_tree>} - the agency tree starting from the root
	var root map[string]any

	if arr, ok := rawResponse.([]any); ok {
		// Response is an array
		if len(arr) == 0 {
			return ErrKeyNotFound
		}
		// The first element should be a map containing the agency tree starting from the root
		var ok2 bool
		root, ok2 = arr[0].(map[string]any)
		if !ok2 {
			// If it's not a map, the key might not exist or the response format is different
			return ErrKeyNotFound
		}
	} else if rootMap, ok := rawResponse.(map[string]any); ok {
		// Response is a map directly - this is the agency tree starting from the root
		root = rootMap
	} else {
		return fmt.Errorf("agency read response is neither an array nor a map: %T", rawResponse)
	}

	current := any(root)
	for i, part := range key {
		m, ok := current.(map[string]any)
		if !ok {
			return ErrKeyNotFound
		}
		v, ok := m[part]
		if !ok {
			// Agency may return value-at-path as arr[0] (e.g. {"Holder":"","Expires":"..."} for lock key).
			// Then root has no key[0]; treat current as the value at path and return it.
			if i == 0 {
				data, err := json.Marshal(current)
				if err != nil {
					return err
				}
				return json.Unmarshal(data, out)
			}
			return ErrKeyNotFound
		}
		current = v
	}

	data, err := json.Marshal(current)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func (c *client) ReadKey(ctx context.Context, key []string, out any) error {
	return c.Read(ctx, key, out)
}
