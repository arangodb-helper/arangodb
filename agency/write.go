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
	"strings"
	"time"
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

	// Convert operations to nested map format by key path
	// Agency API expects nested structure: {"key": {"path": {"to": {"key": value}}}}
	operations := c.buildOperationsMap(tx.Ops)

	// Convert conditions to nested map format for preconditions
	preconditions := c.buildPreconditionsMap(tx.Conds)

	// Agency write API expects: [[{operations}, {preconditions}, "clientId"]]
	body := []any{
		[]any{
			operations,
			preconditions,
			"", // clientId
		},
	}

	req, err := c.conn.NewRequest("POST", "/_api/agency/write")
	if err != nil {
		return fmt.Errorf("failed to create agency write request: %w", err)
	}

	if err := req.SetBody(body); err != nil {
		return fmt.Errorf("failed to set request body: %w", err)
	}

	var result struct {
		Results []int `json:"results"`
	}

	resp, err := c.conn.Do(ctx, req, &result)
	if err != nil {
		return fmt.Errorf("agency write request failed: %w", err)
	}
	if resp == nil {
		return fmt.Errorf("agency write response is nil")
	}

	statusCode := resp.Code()
	if statusCode != 200 && statusCode != 201 {
		return fmt.Errorf("agency write returned non-success status code %d: Results: %v", statusCode, result.Results)
	}

	if len(result.Results) == 0 {
		return fmt.Errorf("agency write failed: no results returned")
	}

	if result.Results[0] == 0 {
		return fmt.Errorf("agency write failed: results=%v (result[0]=0 means failure)", result.Results)
	}

	// Write succeeded - return success
	return nil
}

// buildOperationsMap converts KeyChanger operations to nested map format
// Expected format: {"key": {"path": {"to": {"key": value}}}}
func (c *client) buildOperationsMap(ops []KeyChanger) map[string]any {
	result := make(map[string]any)

	for _, op := range ops {
		keyStr := op.GetKey()
		// Remove leading slash if present
		keyStr = strings.TrimPrefix(keyStr, "/")
		keyPath := strings.Split(keyStr, "/")
		if len(keyPath) == 0 || (len(keyPath) == 1 && keyPath[0] == "") {
			continue
		}

		current := result
		for i, part := range keyPath {
			if part == "" {
				continue
			}

			if i == len(keyPath)-1 {
				// Last part - set the value based on operation type
				switch op.GetOperation() {
				case "set":
					current[part] = op.GetNew()
				case "delete":
					// For delete, agency expects null
					current[part] = nil
				default:
					current[part] = op.GetNew()
				}
			} else {
				// Intermediate parts - create nested maps
				if _, exists := current[part]; !exists {
					current[part] = make(map[string]any)
				}
				// Type assert to ensure we can continue nesting
				if nestedMap, ok := current[part].(map[string]any); ok {
					current = nestedMap
				} else {
					// Overwrite with a new map
					current[part] = make(map[string]any)
					current = current[part].(map[string]any)
				}
			}
		}
	}

	return result
}

// buildPreconditionsMap converts WriteCondition slice to nested map format
// Expected format: {"key": {"path": {"to": {"key": {"old": value}}}}}
func (c *client) buildPreconditionsMap(conds []WriteCondition) map[string]any {
	if len(conds) == 0 {
		// Return empty map (not nil) - agency expects {} for empty preconditions
		return make(map[string]any)
	}

	result := make(map[string]any)

	for _, cond := range conds {
		// Skip nil conditions
		if cond == nil {
			continue
		}
		// Get the full key path from the condition
		keyPath := cond.GetKey()
		if len(keyPath) == 0 {
			continue
		}

		condValue := cond.GetValue()

		// Build nested structure for the key path
		current := result
		for i, part := range keyPath {
			if i == len(keyPath)-1 {
				// Last part - set the condition
				// For now, assume it's an "old" condition (ifEqual)
				current[part] = map[string]any{
					"old": condValue,
				}
			} else {
				// Intermediate parts - create nested maps
				if _, exists := current[part]; !exists {
					current[part] = make(map[string]any)
				}
				current = current[part].(map[string]any)
			}
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
	tx := &Transaction{
		Ops:   []KeyChanger{SetKey(key, value)},
		Conds: conditions,
	}

	err := c.Write(ctx, tx)
	return err
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

func (c *client) WriteKeyIfEmpty(ctx context.Context, key []string, value interface{}, ttl time.Duration) error {
	// implement the logic
	return nil
}

func (c *client) WriteKeyIfEqualTo(ctx context.Context, key []string, newValue, oldValue interface{}, ttl time.Duration) error {
	// implement the logic
	return nil
}
