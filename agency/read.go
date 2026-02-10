// agency/read.go
package agency

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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

	reqBody := map[string]any{
		"keys": [][]string{key},
	}

	req, err := c.conn.NewRequest(http.MethodPost, "/_api/agency/read")
	if err != nil {
		return fmt.Errorf("failed to create agency read request: %w", err)
	}
	if err := req.SetBody(reqBody); err != nil {
		return fmt.Errorf("failed to set request body: %w", err)
	}

	var rawResponse any
	resp, err := c.conn.Do(ctx, req, &rawResponse)
	if err != nil {
		return fmt.Errorf("agency read request failed: %w", err)
	}
	if resp == nil {
		return ErrKeyNotFound
	}

	arr, ok := rawResponse.([]any)
	if !ok || len(arr) == 0 {
		return ErrKeyNotFound
	}

	root, ok := arr[0].(map[string]any)
	if !ok {
		return ErrKeyNotFound
	}

	current := any(root)
	for _, part := range key {
		m, ok := current.(map[string]any)
		if !ok {
			return ErrKeyNotFound
		}
		v, ok := m[part]
		if !ok {
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
