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
	"net/http"
	"net/url"
	"strings"
	"time"

	driver_http "github.com/arangodb/go-driver/v2/connection"
)

const (
	agencyConnectionRetryWindow = 20 * time.Second
	agencyConnectionRetryDelay  = 100 * time.Millisecond
	agencyRequestTimeout        = 10 * time.Second
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

	// Agency read API expects the body to be an array of flat key path strings.
	// Format: [["/key/path/to/key"]]
	fullKey := "/" + strings.Join(key, "/")
	reqBody := [][]string{{fullKey}}

	// Retry on connection-level errors (connection refused, timeout, EOF) to handle
	// agency endpoint failover. The RoundRobinEndpoints rotates to the next
	// endpoint on each new request, so retrying naturally tries a different agent.
	var rawResponse any
	var lastErr error
	deadline := time.Now().Add(agencyConnectionRetryWindow)
	for attempt := 0; ; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(agencyConnectionRetryDelay):
			}
		}

		req, err := c.conn.NewRequest(http.MethodPost, "/_api/agency/read")
		if err != nil {
			return fmt.Errorf("failed to create agency read request: %w", err)
		}
		if err := req.SetBody(reqBody); err != nil {
			return fmt.Errorf("failed to set request body: %w", err)
		}

		reqCtx, reqCancel := context.WithTimeout(ctx, agencyRequestTimeout)
		resp, err := c.conn.Do(reqCtx, req, &rawResponse)
		reqCancel()
		if err != nil {
			if isConnectionError(err) && time.Now().Before(deadline) {
				lastErr = err
				continue
			}
			// Treat context deadline from the per-request timeout as a connection error (retry).
			if reqCtx.Err() != nil && ctx.Err() == nil && time.Now().Before(deadline) {
				lastErr = err
				continue
			}
			return fmt.Errorf("agency read request failed: %w", err)
		}
		if resp == nil {
			return ErrKeyNotFound
		}
		respCode := resp.Code()

		// Server errors (5xx) are transient (e.g. 503 during Raft election) — retry.
		if respCode >= 500 {
			if time.Now().Before(deadline) {
				lastErr = fmt.Errorf("agency read returned HTTP %d", respCode)
				continue
			}
			return fmt.Errorf("agency read returned HTTP %d after retry window exhausted", respCode)
		}
		// Client errors (4xx) are not retryable.
		if respCode >= 400 {
			return fmt.Errorf("agency read returned HTTP %d", respCode)
		}

		if respCode == http.StatusTemporaryRedirect && c.redirectConfig == nil {
			return ErrRedirectNotFollowed
		}
		// 307 from non-leader returns empty body; follow Location once so leader election completes.
		if respCode >= 300 && respCode < 400 && c.redirectConfig != nil {
			if location := resp.Header("Location"); location != "" {
				absoluteLocation := resolveRedirectLocation(location, resp.Endpoint())
				redirectRaw, redirectErr := c.doReadToEndpoint(ctx, reqBody, absoluteLocation)
				if redirectErr == nil && redirectRaw != nil {
					rawResponse = redirectRaw
				}
			}
		}
		lastErr = nil
		break
	}
	if lastErr != nil {
		return fmt.Errorf("agency read request failed after retry window %s: %w", agencyConnectionRetryWindow, lastErr)
	}

	// Agency read API can return either:
	// 1. An array: [<value_at_key_path>] - where the first element is a map containing the agency tree
	// 2. A map directly: {<agency_tree>} - the agency tree starting from the root
	var root map[string]any

	if rawResponse == nil {
		return fmt.Errorf("agency read response is empty (possibly 307 redirect not followed)")
	}
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
		// An empty map means the key path does not exist in the agency.
		if len(root) == 0 {
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

func (c *client) doReadToEndpoint(ctx context.Context, reqBody [][]string, locationURL string) (any, error) {
	u, err := url.Parse(locationURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("redirect Location not absolute: %q", locationURL)
	}
	baseURL := u.Scheme + "://" + u.Host
	config := *c.redirectConfig
	config.Endpoint = driver_http.NewRoundRobinEndpoints([]string{baseURL})
	redirectConn := driver_http.NewHttpConnection(config)
	req, err := redirectConn.NewRequest(http.MethodPost, "/_api/agency/read")
	if err != nil {
		return nil, err
	}
	if err := req.SetBody(reqBody); err != nil {
		return nil, err
	}
	var rawResponse any
	resp, err := redirectConn.Do(ctx, req, &rawResponse)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("redirect read failed: no response")
	}
	if resp.Code() < 200 || resp.Code() >= 300 {
		return nil, fmt.Errorf("redirect read failed: code=%d", resp.Code())
	}
	return rawResponse, nil
}

func (c *client) ReadKey(ctx context.Context, key []string, out any) error {
	return c.Read(ctx, key, out)
}
