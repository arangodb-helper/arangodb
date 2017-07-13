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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pkg/errors"
)

// newAgencyClient creates a new client implementation.
func newAgencyClient(endpoint url.URL, prepareRequest func(*http.Request) error) (API, error) {
	endpoint.Path = ""
	return &client{
		endpoint:       endpoint,
		client:         DefaultHTTPClient(),
		prepareRequest: prepareRequest,
	}, nil
}

type client struct {
	endpoint       url.URL
	client         *http.Client
	prepareRequest func(*http.Request) error
}

const (
	contentTypeJSON = "application/json"
)

// ReadKey reads the value of a given key in the agency.
func (c *client) ReadKey(ctx context.Context, key []string) (interface{}, error) {
	url := c.createURL("/_api/agency/read", nil)

	reqBody := fmt.Sprintf(`[["%s"]]`, "/"+strings.Join(key, "/"))
	//fmt.Printf("Sending `%s` to %s\n", reqBody, url)
	req, err := http.NewRequest("POST", url, strings.NewReader(reqBody))
	if err != nil {
		return nil, maskAny(err)
	}
	if ctx != nil {
		req = req.WithContext(ctx)
	}
	if c.prepareRequest != nil {
		if err := c.prepareRequest(req); err != nil {
			return nil, maskAny(err)
		}
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, maskAny(err)
	}
	var result []map[string]interface{}
	if err := c.handleResponse(resp, "POST", url, &result); err != nil {
		return nil, maskAny(err)
	}
	// Result should be 1 long
	if len(result) != 1 {
		return nil, maskAny(fmt.Errorf("Expected result of 1 long, got %d", len(result)))
	}
	var ptr interface{}
	ptr = result[0]
	// Resolve result
	for _, keyElem := range key {
		if ptrMap, ok := ptr.(map[string]interface{}); !ok {
			return nil, maskAny(fmt.Errorf("Expected map as key element '%s' got %v", keyElem, ptr))
		} else {
			if child, found := ptrMap[keyElem]; !found {
				return nil, maskAny(errors.Wrapf(KeyNotFoundError, "Cannot find key element '%s'", keyElem))
			} else {
				ptr = child
			}
		}
	}

	// Now ptr contain reference to our data, return it
	return ptr, nil
}

type writeUpdate struct {
	Operation string      `json:"op,omitempty"`
	New       interface{} `json:"new,omitempty"`
	TTL       int64       `json:"ttl,omitempty"`
}

type writeCondition struct {
	Old      interface{} `json:"old,omitempty"`      // Require old value to be equal to this
	OldEmpty *bool       `json:"oldEmpty,omitempty"` // Require old value to be empty
	IsArray  *bool       `json:"isArray,omitempty"`  // Require old value to be array
}

type writeTransaction []map[string]interface{}
type writeTransactions []writeTransaction

type writeResult struct {
	Results []int64 `json:"results"`
}

// WriteKeyIfEmpty writes the given value with the given key only if the key was empty before.
func (c *client) WriteKeyIfEmpty(ctx context.Context, key []string, value interface{}, ttl time.Duration) error {
	oldEmpty := true
	condition := writeCondition{
		OldEmpty: &oldEmpty,
	}
	if err := c.write(ctx, key, value, condition, ttl); err != nil {
		return maskAny(err)
	}
	return nil
}

// WriteKeyIfEqualTo writes the given new value with the given key only if the existing value for that key equals
// to the given old value.
func (c *client) WriteKeyIfEqualTo(ctx context.Context, key []string, newValue, oldValue interface{}, ttl time.Duration) error {
	condition := writeCondition{
		Old: oldValue,
	}
	if err := c.write(ctx, key, newValue, condition, ttl); err != nil {
		return maskAny(err)
	}
	return nil
}

// write writes the given value with the given key only if the given condition is fullfilled.
func (c *client) write(ctx context.Context, key []string, value interface{}, condition writeCondition, ttl time.Duration) error {
	url := c.createURL("/_api/agency/write", nil)

	fullKey := createFullKey(key)
	writeTxs := writeTransactions{
		writeTransaction{
			// Update
			map[string]interface{}{
				fullKey: writeUpdate{
					Operation: "set",
					New:       value,
					TTL:       int64(ttl.Seconds()),
				},
			},
			// Condition
			map[string]interface{}{
				fullKey: condition,
			},
		},
	}

	reqBody, err := json.Marshal(writeTxs)
	if err != nil {
		return maskAny(err)
	}
	//fmt.Printf("Sending `%s` to %s\n", string(reqBody), url)
	req, err := http.NewRequest("POST", url, bytes.NewReader(reqBody))
	if err != nil {
		return maskAny(err)
	}
	if ctx != nil {
		req = req.WithContext(ctx)
	}
	if c.prepareRequest != nil {
		if err := c.prepareRequest(req); err != nil {
			return maskAny(err)
		}
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return maskAny(err)
	}
	var result writeResult
	if err := c.handleResponse(resp, "POST", url, &result); err != nil {
		return maskAny(err)
	}
	// "results" should be 1 long
	if len(result.Results) != 1 {
		return maskAny(fmt.Errorf("Expected results of 1 long, got %d", len(result.Results)))
	}

	// If results[0] == 0, condition failed, otherwise success
	if result.Results[0] == 0 {
		// Condition failed
		return maskAny(ConditionFailedError)
	}

	// Success
	return nil
}

// handleResponse checks the given response status and decodes any JSON result.
func (c *client) handleResponse(resp *http.Response, method, url string, result interface{}) error {
	// Read response body into memory
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return maskAny(errors.Wrapf(err, "Failed reading response data from %s request to %s: %v", method, url, err))
	}

	if resp.StatusCode != http.StatusOK {
		return maskAny(StatusError{resp.StatusCode})
	}

	// Got a success status
	if result != nil {
		if err := json.Unmarshal(body, result); err != nil {
			return maskAny(errors.Wrapf(err, "Failed decoding response data from %s request to %s: %v", method, url, err))
		}
	}
	return nil
}

// createURL creates a full URL for a request with given local path & query.
func (c *client) createURL(urlPath string, query url.Values) string {
	u := c.endpoint
	u.Path = urlPath
	if query != nil {
		u.RawQuery = query.Encode()
	}
	return u.String()
}

func createFullKey(key []string) string {
	return "/" + strings.Join(key, "/")
}
