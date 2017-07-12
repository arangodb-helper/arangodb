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

package agency

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// NewAgencyClient creates a new client implementation for an agency with multiple endpoints.
func NewAgencyClient(endpoints []url.URL, prepareRequest func(*http.Request) error) (API, error) {
	if len(endpoints) == 1 {
		return newAgencyClient(endpoints[0], prepareRequest)
	}
	c := &multiClient{}
	for _, ep := range endpoints {
		epClient, err := newAgencyClient(ep, prepareRequest)
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

// ReadKey reads the value of a given key in the agency.
func (c *multiClient) ReadKey(ctx context.Context, key []string, output interface{}) error {
	for _, epClient := range c.clients {
		err := epClient.ReadKey(ctx, key, output)
		if err == nil {
			return nil
		}
		// Check error
		statusCode, ok := IsStatusError(err)
		if !ok {
			return maskAny(err)
		}
		// We have a status code, check it
		if statusCode >= 400 && statusCode < 500 {
			// Real error, return it
			return maskAny(err)
		}
		// No permanent error, try next agent
	}
	return maskAny(fmt.Errorf("No available agents"))
}

// WriteKeyIfEmpty writes the given value with the given key only if the key was empty before.
func (c *multiClient) WriteKeyIfEmpty(ctx context.Context, key []string, value interface{}) error {
	for _, epClient := range c.clients {
		err := epClient.WriteKeyIfEmpty(ctx, key, value)
		if err == nil {
			return nil
		}
		// Check error
		statusCode, ok := IsStatusError(err)
		if !ok {
			return maskAny(err)
		}
		// We have a status code, check it
		if statusCode >= 400 && statusCode < 500 {
			// Real error, return it
			return maskAny(err)
		}
		// No permanent error, try next agent
	}
	return maskAny(fmt.Errorf("No available agents"))
}

// WriteKeyIfEqualTo writes the given new value with the given key only if the existing value for that key equals
// to the given old value.
func (c *multiClient) WriteKeyIfEqualTo(ctx context.Context, key []string, newValue, oldValue interface{}) error {
	for _, epClient := range c.clients {
		err := epClient.WriteKeyIfEqualTo(ctx, key, newValue, oldValue)
		if err == nil {
			return nil
		}
		// Check error
		statusCode, ok := IsStatusError(err)
		if !ok {
			return maskAny(err)
		}
		// We have a status code, check it
		if statusCode >= 400 && statusCode < 500 {
			// Real error, return it
			return maskAny(err)
		}
		// No permanent error, try next agent
	}
	return maskAny(fmt.Errorf("No available agents"))
}
