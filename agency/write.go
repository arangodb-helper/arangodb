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
	"net/url"
	"strings"
	"time"

	driver_http "github.com/arangodb/go-driver/v2/connection"
)

func (c *client) Write(ctx context.Context, tx *Transaction) error {
	if c == nil {
		return fmt.Errorf("agency client is nil")
	}
	if c.conn == nil {
		return fmt.Errorf("agency client connection is nil")
	}
	if tx == nil {
		return fmt.Errorf("transaction is nil")
	}

	// Convert operations to flat key path format with operation descriptors
	// Agency API expects: {"/key/path": {"op": "set", "new": value, "ttl": N}}
	operations := c.buildOperationsMap(tx.Ops, tx.TTLs)

	// Convert conditions to flat key path format for preconditions
	preconditions := c.buildPreconditionsMap(tx.Conds)

	// Agency write API expects: [[{operations}, {preconditions}, "clientId"]]
	body := []any{
		[]any{
			operations,
			preconditions,
			"", // clientId
		},
	}

	// Retry on connection-level errors (connection refused, timeout, EOF) to handle
	// agency endpoint failover. The RoundRobinEndpoints rotates to the next
	// endpoint on each new request, so retrying naturally tries a different agent.
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

		req, err := c.conn.NewRequest("POST", "/_api/agency/write")
		if err != nil {
			return fmt.Errorf("failed to create agency write request: %w", err)
		}

		if err := req.SetBody(body); err != nil {
			return fmt.Errorf("failed to set request body: %w", err)
		}

		var result writeResult

		resp, err := c.conn.Do(ctx, req, &result)
		if err != nil {
			if isConnectionError(err) && time.Now().Before(deadline) {
				lastErr = err
				continue
			}
			return fmt.Errorf("agency write request failed: %w", err)
		}
		lastErr = nil

		if resp == nil {
			return fmt.Errorf("agency write response is nil")
		}

		statusCode := resp.Code()
		if len(result.Results) == 0 {
			// 307 from non-leader returns empty body; follow Location once so leader election completes.
			if statusCode >= 300 && statusCode < 400 && c.redirectConfig != nil {
				if location := resp.Header("Location"); location != "" {
					absoluteLocation := resolveRedirectLocation(location, resp.Endpoint())
					redirectResp, redirectResult, redirectErr := c.doWriteToEndpoint(ctx, body, absoluteLocation)
					if redirectErr == nil && redirectResp != nil && redirectResult != nil && len(redirectResult.Results) > 0 {
						result = *redirectResult
						resp = redirectResp
						statusCode = redirectResp.Code()
					}
				}
			}
			if len(result.Results) == 0 {
				return fmt.Errorf("agency write failed: no results returned")
			}
		}

		// Check results[0] first - 0 means precondition failed, 1 means success
		// Status code 412 (Precondition Failed) is a valid response indicating precondition didn't match
		// Status codes 200/201 with results[0] == 0 also indicate precondition failed
		if result.Results[0] == 0 {
			// Precondition failed - this is expected during leader election and other concurrent operations
			return &PreconditionFailedError{
				message: fmt.Sprintf("agency write precondition failed: statusCode=%d, results=%v", statusCode, result.Results),
			}
		}

		// If we got here, results[0] == 1 (success)
		// Accept 200, 201, or 412 as valid status codes when results indicate success
		if statusCode != 200 && statusCode != 201 && statusCode != 412 {
			return fmt.Errorf("agency write returned non-success status code %d: Results: %v", statusCode, result.Results)
		}

		// Write succeeded.
		break
	}
	if lastErr != nil {
		return fmt.Errorf("agency write request failed after retry window %s: %w", agencyConnectionRetryWindow, lastErr)
	}
	return nil
}

type writeResult struct {
	Results []int `json:"results"`
}

func resolveRedirectLocation(location, responseEndpoint string) string {
	u, err := url.Parse(location)
	if err != nil {
		return location
	}
	if u.Scheme != "" && u.Host != "" {
		return location
	}
	base, err := url.Parse(responseEndpoint)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return location
	}
	return base.ResolveReference(u).String()
}

func (c *client) doWriteToEndpoint(ctx context.Context, body []any, locationURL string) (driver_http.Response, *writeResult, error) {
	u, err := url.Parse(locationURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, nil, fmt.Errorf("invalid redirect Location: %q", locationURL)
	}
	baseURL := u.Scheme + "://" + u.Host
	config := *c.redirectConfig
	config.Endpoint = driver_http.NewRoundRobinEndpoints([]string{baseURL})
	redirectConn := driver_http.NewHttpConnection(config)
	req, err := redirectConn.NewRequest("POST", "/_api/agency/write")
	if err != nil {
		return nil, nil, err
	}
	if err := req.SetBody(body); err != nil {
		return nil, nil, err
	}
	var result writeResult
	resp, err := redirectConn.Do(ctx, req, &result)
	if err != nil {
		return nil, nil, err
	}
	return resp, &result, nil
}

// buildOperationsMap converts KeyChanger operations to flat key path format
// with operation descriptors as expected by the ArangoDB Agency write API.
// Expected format: {"/key/path/to/key": {"op": "set", "new": value, "ttl": N}}
func (c *client) buildOperationsMap(ops []KeyChanger, ttls map[string]time.Duration) map[string]any {
	result := make(map[string]any)

	for _, op := range ops {
		keyStr := "/" + strings.TrimPrefix(op.GetKey(), "/")

		opMap := map[string]any{
			"op": op.GetOperation(),
		}
		if op.GetOperation() != "delete" {
			opMap["new"] = op.GetNew()
		}
		if ttls != nil {
			if ttl, ok := ttls[op.GetKey()]; ok && ttl > 0 {
				opMap["ttl"] = int(ttl.Seconds())
			}
		}
		result[keyStr] = opMap
	}

	return result
}

// buildPreconditionsMap converts WriteCondition slice to flat key path format
// as expected by the ArangoDB Agency write API.
// Expected format: {"/key/path/to/key": {"old": value}}
func (c *client) buildPreconditionsMap(conds []WriteCondition) map[string]any {
	if len(conds) == 0 {
		// Return empty map (not nil) - agency expects {} for empty preconditions
		return make(map[string]any)
	}

	result := make(map[string]any)

	for _, cond := range conds {
		if cond == nil {
			continue
		}
		keyPath := cond.GetKey()
		if len(keyPath) == 0 {
			continue
		}

		keyStr := "/" + strings.Join(keyPath, "/")
		result[keyStr] = map[string]any{
			"old": cond.GetValue(),
		}
	}

	return result
}

func (c *client) WriteKey(
	ctx context.Context,
	key []string,
	value any,
	ttl time.Duration,
	conditions ...WriteCondition,
) error {
	op := SetKey(key, value)
	tx := &Transaction{
		Ops:   []KeyChanger{op},
		Conds: conditions,
	}
	if ttl > 0 {
		tx.TTLs = map[string]time.Duration{
			op.GetKey(): ttl,
		}
	}
	return c.Write(ctx, tx)
}

func (c *client) RemoveKey(
	ctx context.Context,
	key []string,
	conditions ...WriteCondition,
) error {
	tx := &Transaction{
		Ops: []KeyChanger{
			RemoveKey(key),
		},
		Conds: conditions,
	}

	return c.Write(ctx, tx)
}
