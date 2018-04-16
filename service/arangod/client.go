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

// NewServerClient creates a new client implementation for a single server.
func NewServerClient(endpoint url.URL, prepareRequest func(*http.Request) error, followRedirects bool) (API, error) {
	endpoint.Path = ""
	c := &client{
		endpoint:       endpoint,
		client:         shardHTTPClient,
		prepareRequest: prepareRequest,
	}
	if !followRedirects {

		c.client.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse // Do not wrap, standard library will not understand
		}
	}
	return c, nil
}

var (
	shardHTTPClient = DefaultHTTPClient()
)

type client struct {
	endpoint       url.URL
	client         *http.Client
	prepareRequest func(*http.Request) error
}

const (
	contentTypeJSON = "application/json"
)

// Agency returns API of the agency
func (c *client) Agency() AgencyAPI {
	return c
}

// Server returns API of single server
// Returns an error when multiple endpoints are configured.
func (c *client) Server() (ServerAPI, error) {
	return c, nil
}

// Cluster returns API of the cluster.
// Endpoints must be URL's of one or more coordinators of the cluster.
func (c *client) Cluster() ClusterAPI {
	return c
}

// Returns the endpoint of the specific agency this api targets.
func (c *client) Endpoint() string {
	return c.endpoint.String()
}

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
	URL       string      `json:"url,omitempty"`
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

	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return maskAny(err)
	}
	if err := setJSONRequestBody(req, writeTxs); err != nil {
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

// RemoveKeyIfEqualTo removes the given key only if the existing value for that key equals
// to the given old value.
func (c *client) RemoveKeyIfEqualTo(ctx context.Context, key []string, oldValue interface{}) error {
	url := c.createURL("/_api/agency/write", nil)

	condition := writeCondition{
		Old: oldValue,
	}
	fullKey := createFullKey(key)
	writeTxs := writeTransactions{
		writeTransaction{
			// Update
			map[string]interface{}{
				fullKey: writeUpdate{
					Operation: "delete",
				},
			},
			// Condition
			map[string]interface{}{
				fullKey: condition,
			},
		},
	}

	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return maskAny(err)
	}
	if err := setJSONRequestBody(req, writeTxs); err != nil {
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

// Register a URL to receive notification callbacks when the value of the given key changes
func (c *client) RegisterChangeCallback(ctx context.Context, key []string, cbURL string) error {
	url := c.createURL("/_api/agency/write", nil)

	fullKey := createFullKey(key)
	writeTxs := writeTransactions{
		writeTransaction{
			// Update
			map[string]interface{}{
				fullKey: writeUpdate{
					Operation: "observe",
					URL:       cbURL,
				},
			},
		},
	}

	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return maskAny(err)
	}
	if err := setJSONRequestBody(req, writeTxs); err != nil {
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

	// Success
	return nil
}

// Register a URL to receive notification callbacks when the value of the given key changes
func (c *client) UnregisterChangeCallback(ctx context.Context, key []string, cbURL string) error {
	url := c.createURL("/_api/agency/write", nil)

	fullKey := createFullKey(key)
	writeTxs := writeTransactions{
		writeTransaction{
			// Update
			map[string]interface{}{
				fullKey: writeUpdate{
					Operation: "unobserve",
					URL:       cbURL,
				},
			},
		},
	}

	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return maskAny(err)
	}
	if err := setJSONRequestBody(req, writeTxs); err != nil {
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

	// Success
	return nil
}

type idResponse struct {
	ID string `json:"id,omitempty"`
}

// Gets the Version of this server.
func (c *client) Version(ctx context.Context) (VersionInfo, error) {
	url := c.createURL("/_api/version", nil)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return VersionInfo{}, maskAny(err)
	}
	if ctx != nil {
		req = req.WithContext(ctx)
	}
	if c.prepareRequest != nil {
		if err := c.prepareRequest(req); err != nil {
			return VersionInfo{}, maskAny(err)
		}
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return VersionInfo{}, maskAny(err)
	}
	var result VersionInfo
	if err := c.handleResponse(resp, "GET", url, &result); err != nil {
		return VersionInfo{}, maskAny(err)
	}

	// Success
	return result, nil
}

// Gets the ID of this server in the cluster.
// ID will be empty for single servers.
func (c *client) ID(ctx context.Context) (string, error) {
	url := c.createURL("/_admin/server/id", nil)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", maskAny(err)
	}
	if ctx != nil {
		req = req.WithContext(ctx)
	}
	if c.prepareRequest != nil {
		if err := c.prepareRequest(req); err != nil {
			return "", maskAny(err)
		}
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return "", maskAny(err)
	}
	var result idResponse
	if err := c.handleResponse(resp, "GET", url, &result); err != nil {
		return "", maskAny(err)
	}

	// Success
	return result.ID, nil
}

// Shutdown a specific server, optionally removing it from its cluster.
func (c *client) Shutdown(ctx context.Context, removeFromCluster bool) error {
	q := url.Values{}
	if removeFromCluster {
		q.Add("remove_from_cluster", "1")
	}
	url := c.createURL("/_admin/shutdown", q)

	req, err := http.NewRequest("DELETE", url, nil)
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
	if err := c.handleResponse(resp, "DELETE", url, nil); err != nil {
		return maskAny(err)
	}

	// Success
	return nil
}

type cleanOutServerRequest struct {
	Server string `json:"server"`
}

// CleanOutServer triggers activities to clean out a DBServers.
func (c *client) CleanOutServer(ctx context.Context, serverID string) error {
	url := c.createURL("/_admin/cluster/cleanOutServer", nil)

	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return maskAny(err)
	}
	if err := setJSONRequestBody(req, cleanOutServerRequest{Server: serverID}); err != nil {
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
	if err := c.handleResponse(resp, "POST", url, nil); err != nil {
		return maskAny(err)
	}

	// Success
	return nil
}

// IsCleanedOut checks if the dbserver with given ID has been cleaned out.
func (c *client) IsCleanedOut(ctx context.Context, serverID string) (bool, error) {
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
func (c *client) NumberOfServers(ctx context.Context) (NumberOfServersResponse, error) {
	url := c.createURL("/_admin/cluster/numberOfServers", nil)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return NumberOfServersResponse{}, maskAny(err)
	}
	if ctx != nil {
		req = req.WithContext(ctx)
	}
	if c.prepareRequest != nil {
		if err := c.prepareRequest(req); err != nil {
			return NumberOfServersResponse{}, maskAny(err)
		}
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return NumberOfServersResponse{}, maskAny(err)
	}
	var result NumberOfServersResponse
	if err := c.handleResponse(resp, "GET", url, &result); err != nil {
		return NumberOfServersResponse{}, maskAny(err)
	}

	// Success
	return result, nil
}

// setJSONRequestBody sets the content of the given request as JSON encoded value.
func setJSONRequestBody(req *http.Request, content interface{}) error {
	encoded, err := json.Marshal(content)
	if err != nil {
		return maskAny(err)
	}
	req.ContentLength = int64(len(encoded))
	req.Body = ioutil.NopCloser(bytes.NewReader(encoded))
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

	if resp.StatusCode == 307 {
		// Not leader
		location := resp.Header.Get("Location")
		return NotLeaderError{Leader: location}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
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
