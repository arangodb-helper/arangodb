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

package arangod

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// NewClusterClient creates a new client implementation for a cluster with multiple endpoints.
func NewClusterClient(endpoints []url.URL, prepareRequest func(*http.Request) error) (API, error) {
	if len(endpoints) == 1 {
		return NewServerClient(endpoints[0], prepareRequest, true)
	}
	c := &multiClient{}
	for _, ep := range endpoints {
		epClient, err := NewServerClient(ep, prepareRequest, false)
		if err != nil {
			return nil, maskAny(err)
		}
		c.clients = append(c.clients, epClient)
	}
	return c, nil
}

type multiClient struct {
	clients []API
}

// Returns the endpoint of the specific agency this api targets.
func (c *multiClient) Endpoint() string {
	result := make([]string, 0, len(c.clients))
	for _, c := range c.clients {
		result = append(result, c.Agency().Endpoint())
	}
	return strings.Join(result, ",")
}

// Agency returns API of the agency
func (c *multiClient) Agency() AgencyAPI {
	return c
}

// Server returns API of single server
// Returns an error when multiple endpoints are configured.
func (c *multiClient) Server() (ServerAPI, error) {
	return nil, maskAny(fmt.Errorf("Not supported for multiple endpoints"))
}

// Cluster returns API of the cluster.
// Endpoints must be URL's of one or more coordinators of the cluster.
func (c *multiClient) Cluster() ClusterAPI {
	return c
}

// ReadKey reads the value of a given key in the agency.
func (c *multiClient) ReadKey(ctx context.Context, key []string) (interface{}, error) {
	op := func(ctx context.Context, c API) (interface{}, error) {
		if result, err := c.Agency().ReadKey(ctx, key); err != nil {
			return nil, maskAny(err)
		} else {
			return result, nil
		}
	}
	if result, err := c.handleMultiRequests(ctx, op); err != nil {
		return nil, maskAny(err)
	} else {
		return result, nil
	}
}

// WriteKey writes the given value with the given key.
func (c *multiClient) WriteKey(ctx context.Context, key []string, value interface{}, ttl time.Duration) error {
	op := func(ctx context.Context, c API) (interface{}, error) {
		if err := c.Agency().WriteKey(ctx, key, value, ttl); err != nil {
			return nil, maskAny(err)
		}
		return nil, nil
	}
	if _, err := c.handleMultiRequests(ctx, op); err != nil {
		return maskAny(err)
	}
	return nil
}

// WriteKeyIfEmpty writes the given value with the given key only if the key was empty before.
func (c *multiClient) WriteKeyIfEmpty(ctx context.Context, key []string, value interface{}, ttl time.Duration) error {
	op := func(ctx context.Context, c API) (interface{}, error) {
		if err := c.Agency().WriteKeyIfEmpty(ctx, key, value, ttl); err != nil {
			return nil, maskAny(err)
		}
		return nil, nil
	}
	if _, err := c.handleMultiRequests(ctx, op); err != nil {
		return maskAny(err)
	}
	return nil
}

// WriteKeyIfEqualTo writes the given new value with the given key only if the existing value for that key equals
// to the given old value.
func (c *multiClient) WriteKeyIfEqualTo(ctx context.Context, key []string, newValue, oldValue interface{}, ttl time.Duration) error {
	op := func(ctx context.Context, c API) (interface{}, error) {
		if err := c.Agency().WriteKeyIfEqualTo(ctx, key, newValue, oldValue, ttl); err != nil {
			return nil, maskAny(err)
		}
		return nil, nil
	}
	if _, err := c.handleMultiRequests(ctx, op); err != nil {
		return maskAny(err)
	}
	return nil
}

// RemoveKey removes the given key
func (c *multiClient) RemoveKey(ctx context.Context, key []string) error {
	op := func(ctx context.Context, c API) (interface{}, error) {
		if err := c.Agency().RemoveKey(ctx, key); err != nil {
			return nil, maskAny(err)
		}
		return nil, nil
	}
	if _, err := c.handleMultiRequests(ctx, op); err != nil {
		return maskAny(err)
	}
	return nil
}

// RemoveKeyIfEqualTo removes the given key only if the existing value for that key equals
// to the given old value.
func (c *multiClient) RemoveKeyIfEqualTo(ctx context.Context, key []string, oldValue interface{}) error {
	op := func(ctx context.Context, c API) (interface{}, error) {
		if err := c.Agency().RemoveKeyIfEqualTo(ctx, key, oldValue); err != nil {
			return nil, maskAny(err)
		}
		return nil, nil
	}
	if _, err := c.handleMultiRequests(ctx, op); err != nil {
		return maskAny(err)
	}
	return nil
}

// Register a URL to receive notification callbacks when the value of the given key changes
func (c *multiClient) RegisterChangeCallback(ctx context.Context, key []string, cbURL string) error {
	op := func(ctx context.Context, c API) (interface{}, error) {
		if err := c.Agency().RegisterChangeCallback(ctx, key, cbURL); err != nil {
			return nil, maskAny(err)
		}
		return nil, nil
	}
	if _, err := c.handleMultiRequests(ctx, op); err != nil {
		return maskAny(err)
	}
	return nil
}

// Register a URL to receive notification callbacks when the value of the given key changes
func (c *multiClient) UnregisterChangeCallback(ctx context.Context, key []string, cbURL string) error {
	op := func(ctx context.Context, c API) (interface{}, error) {
		if err := c.Agency().UnregisterChangeCallback(ctx, key, cbURL); err != nil {
			return nil, maskAny(err)
		}
		return nil, nil
	}
	if _, err := c.handleMultiRequests(ctx, op); err != nil {
		return maskAny(err)
	}
	return nil
}

// CleanOutServer triggers activities to clean out a DBServers.
func (c *multiClient) CleanOutServer(ctx context.Context, serverID string) error {
	op := func(ctx context.Context, c API) (interface{}, error) {
		if err := c.Cluster().CleanOutServer(ctx, serverID); err != nil {
			return nil, maskAny(err)
		}
		return nil, nil
	}
	if _, err := c.handleMultiRequests(ctx, op); err != nil {
		return maskAny(err)
	}
	return nil
}

// IsCleanedOut checks if the dbserver with given ID has been cleaned out.
func (c *multiClient) IsCleanedOut(ctx context.Context, serverID string) (bool, error) {
	r, err := c.NumberOfServers(ctx)
	if err != nil {
		return false, maskAny(err)
	}
	for _, id := range r.CleanedServerIDs {
		if id == serverID {
			return true, nil
		}
	}
	return false, nil
}

// NumberOfServers returns the number of coordinator & dbservers in a clusters and the
// ID's of cleaned out servers.
func (c *multiClient) NumberOfServers(ctx context.Context) (NumberOfServersResponse, error) {
	op := func(ctx context.Context, c API) (interface{}, error) {
		if result, err := c.Cluster().NumberOfServers(ctx); err != nil {
			return NumberOfServersResponse{}, maskAny(err)
		} else {
			return result, nil
		}
	}
	if result, err := c.handleMultiRequests(ctx, op); err != nil {
		return NumberOfServersResponse{}, maskAny(err)
	} else {
		return result.(NumberOfServersResponse), nil
	}
}

// handleMultiRequests starts the given operation on all endpoints
// in parallel. The first to return a result or a permanent failure cancels
// all other operations.
func (c *multiClient) handleMultiRequests(ctx context.Context, operation func(context.Context, API) (interface{}, error)) (interface{}, error) {
	ctx, cancel := context.WithCancel(ctx)
	results := make(chan interface{}, len(c.clients))
	errors := make(chan error, len(c.clients))
	wg := sync.WaitGroup{}
	for _, epClient := range c.clients {
		wg.Add(1)
		go func(epClient API) {
			defer wg.Done()
			result, err := operation(ctx, epClient)
			if err == nil {
				// Success
				results <- result
				// Cancel all other requests
				cancel()
				return
			}
			// Check error
			if statusCode, ok := IsStatusError(err); ok {
				// We have a status code, check it
				if statusCode >= 400 && statusCode < 500 {
					// Permanent error, return it
					errors <- maskAny(err)
					// Cancel all other requests
					cancel()
					return
				}
			}
			// Key not found
			if IsKeyNotFound(err) {
				// Permanent error, return it
				errors <- maskAny(err)
				// Cancel all other requests
				cancel()
				return
			}
			// No permanent error, try next agent
		}(epClient)
	}

	// Wait for go routines to finished
	wg.Wait()
	cancel()
	close(results)
	close(errors)
	if result, ok := <-results; ok {
		// Return first result
		return result, nil
	}
	if err, ok := <-errors; ok {
		// Return first error
		return nil, maskAny(err)
	}
	return nil, maskAny(fmt.Errorf("All %d servers responded with temporary failure", len(c.clients)))
}
