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

package client

import (
	"bytes"
	"context"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"

	"github.com/arangodb-helper/arangodb/pkg/api"

	driver "github.com/arangodb/go-driver"
	"github.com/pkg/errors"
)

// NewArangoStarterClient creates a new client implementation.
func NewArangoStarterClient(endpoint url.URL) (API, error) {
	endpoint.Path = ""
	return &client{
		endpoint: endpoint,
		client:   shardHTTPClient,
	}, nil
}

var (
	shardHTTPClient = DefaultHTTPClient()
)

type client struct {
	endpoint url.URL
	client   *http.Client
}

func (c *client) AdminJWTActivate(ctx context.Context, token string) (api.Empty, error) {
	var q = url.Values{}

	q.Add("token", token)

	url := c.createURL("/admin/jwt/activate", q)

	var result api.Empty
	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return api.Empty{}, maskAny(err)
	}
	if ctx != nil {
		req = req.WithContext(ctx)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return api.Empty{}, maskAny(err)
	}
	if err := c.handleResponse(resp, "POST", url, &result); err != nil {
		return api.Empty{}, maskAny(err)
	}

	return result, nil
}

func (c *client) AdminJWTRefresh(ctx context.Context) (api.Empty, error) {
	url := c.createURL("/admin/jwt/refresh", nil)

	var result api.Empty
	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return api.Empty{}, maskAny(err)
	}
	if ctx != nil {
		req = req.WithContext(ctx)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return api.Empty{}, maskAny(err)
	}
	if err := c.handleResponse(resp, "POST", url, &result); err != nil {
		return api.Empty{}, maskAny(err)
	}

	return result, nil
}

func (c *client) ClusterInventory(ctx context.Context) (api.ClusterInventory, error) {
	url := c.createURL("/cluster/inventory", nil)

	var result api.ClusterInventory
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return api.ClusterInventory{}, maskAny(err)
	}
	if ctx != nil {
		req = req.WithContext(ctx)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return api.ClusterInventory{}, maskAny(err)
	}
	if err := c.handleResponse(resp, "GET", url, &result); err != nil {
		return api.ClusterInventory{}, maskAny(err)
	}

	return result, nil
}

func (c *client) Inventory(ctx context.Context) (api.Inventory, error) {
	url := c.createURL("/local/inventory", nil)

	var result api.Inventory
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return api.Inventory{}, maskAny(err)
	}
	if ctx != nil {
		req = req.WithContext(ctx)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return api.Inventory{}, maskAny(err)
	}
	if err := c.handleResponse(resp, "GET", url, &result); err != nil {
		return api.Inventory{}, maskAny(err)
	}

	return result, nil
}

const (
	contentTypeJSON = "application/json"
)

// ID requests the starters ID.
func (c *client) ID(ctx context.Context) (IDInfo, error) {
	url := c.createURL("/id", nil)

	var result IDInfo
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return IDInfo{}, maskAny(err)
	}
	if ctx != nil {
		req = req.WithContext(ctx)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return IDInfo{}, maskAny(err)
	}
	if err := c.handleResponse(resp, "GET", url, &result); err != nil {
		return IDInfo{}, maskAny(err)
	}

	return result, nil
}

// Version requests the starter version.
func (c *client) Version(ctx context.Context) (VersionInfo, error) {
	url := c.createURL("/version", nil)

	var result VersionInfo
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return VersionInfo{}, maskAny(err)
	}
	if ctx != nil {
		req = req.WithContext(ctx)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return VersionInfo{}, maskAny(err)
	}
	if err := c.handleResponse(resp, "GET", url, &result); err != nil {
		return VersionInfo{}, maskAny(err)
	}

	return result, nil
}

// DatabaseVersion returns the version of the `arangod` binary that is being
// used by this starter.
func (c *client) DatabaseVersion(ctx context.Context) (driver.Version, error) {
	url := c.createURL("/database-version", nil)

	var result DatabaseVersionResponse
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", maskAny(err)
	}
	if ctx != nil {
		req = req.WithContext(ctx)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return "", maskAny(err)
	}
	if err := c.handleResponse(resp, "GET", url, &result); err != nil {
		return "", maskAny(err)
	}

	return result.Version, nil
}

// Processes loads information of all the server processes launched by a specific arangodb.
func (c *client) Processes(ctx context.Context) (ProcessList, error) {
	url := c.createURL("/process", nil)

	var result ProcessList
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return ProcessList{}, maskAny(err)
	}
	if ctx != nil {
		req = req.WithContext(ctx)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return ProcessList{}, maskAny(err)
	}
	if err := c.handleResponse(resp, "GET", url, &result); err != nil {
		return ProcessList{}, maskAny(err)
	}

	return result, nil
}

// Endpoints loads the URL's needed to reach all starters, agents & coordinators in the cluster.
func (c *client) Endpoints(ctx context.Context) (EndpointList, error) {
	url := c.createURL("/endpoints", nil)

	var result EndpointList
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return EndpointList{}, maskAny(err)
	}
	if ctx != nil {
		req = req.WithContext(ctx)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return EndpointList{}, maskAny(err)
	}
	if err := c.handleResponse(resp, "GET", url, &result); err != nil {
		return EndpointList{}, maskAny(err)
	}

	return result, nil
}

// Shutdown will shutdown a starter (and all its started servers).
// With goodbye set, it will remove the peer slot for the starter.
func (c *client) Shutdown(ctx context.Context, goodbye bool) error {
	q := url.Values{}
	if goodbye {
		q.Set("mode", "goodbye")
	}
	url := c.createURL("/shutdown", q)

	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return maskAny(err)
	}
	if ctx != nil {
		req = req.WithContext(ctx)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return maskAny(err)
	}
	if err := c.handleResponse(resp, "POST", url, nil); err != nil {
		return maskAny(err)
	}

	return nil
}

// GoodbyeRequest is the JSON structure send in the request to /goodbye.
type GoodbyeRequest struct {
	SlaveID string // Unique ID of the slave that should be removed.
}

// RemovePeer removes a peer with given ID from the starter cluster.
// The removal tries to cleanout & properly shutdown servers first.
// If that does not succeed, the operation returns an error,
// unless force is set to true.
func (c *client) RemovePeer(ctx context.Context, id string, force bool) error {
	q := url.Values{}
	if force {
		q.Set("force", "true")
	}
	url := c.createURL("/goodbye", q)

	input := GoodbyeRequest{
		SlaveID: id,
	}
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return maskAny(err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(inputJSON))
	if err != nil {
		return maskAny(err)
	}
	if ctx != nil {
		req = req.WithContext(ctx)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return maskAny(err)
	}
	if err := c.handleResponse(resp, "POST", url, nil); err != nil {
		return maskAny(err)
	}

	return nil
}

// StartDatabaseUpgrade is called to start the upgrade process
func (c *client) StartDatabaseUpgrade(ctx context.Context, forceMinorUpgrade bool) error {
	q := url.Values{}
	if forceMinorUpgrade {
		q.Set("forceMinorUpgrade", "true")
	}
	url := c.createURL("/database-auto-upgrade", q)

	c.client.Timeout = time.Minute * 5
	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return maskAny(err)
	}
	if ctx != nil {
		req = req.WithContext(ctx)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return maskAny(err)
	}
	if err := c.handleResponse(resp, "POST", url, nil); err != nil {
		return maskAny(err)
	}

	return nil
}

// RetryDatabaseUpgrade resets a failure mark in the existing upgrade plan
// such that the starters will retry the upgrade once more.
func (c *client) RetryDatabaseUpgrade(ctx context.Context) error {
	url := c.createURL("/database-auto-upgrade", nil)

	req, err := http.NewRequest("PUT", url, nil)
	if err != nil {
		return maskAny(err)
	}
	if ctx != nil {
		req = req.WithContext(ctx)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return maskAny(err)
	}
	if err := c.handleResponse(resp, "PUT", url, nil); err != nil {
		return maskAny(err)
	}

	return nil
}

// AbortDatabaseUpgrade removes the existing upgrade plan.
// Note that Starters working on an entry of the upgrade
// will finish that entry.
// If there is no plan, a NotFoundError will be returned.
func (c *client) AbortDatabaseUpgrade(ctx context.Context) error {
	url := c.createURL("/database-auto-upgrade", nil)

	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return maskAny(err)
	}
	if ctx != nil {
		req = req.WithContext(ctx)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return maskAny(err)
	}
	if err := c.handleResponse(resp, "DELETE", url, nil); err != nil {
		return maskAny(err)
	}

	return nil
}

// Status returns the status of any upgrade plan
func (c *client) UpgradeStatus(ctx context.Context) (UpgradeStatus, error) {
	url := c.createURL("/database-auto-upgrade", nil)

	var result UpgradeStatus
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return UpgradeStatus{}, maskAny(err)
	}
	if ctx != nil {
		req = req.WithContext(ctx)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return UpgradeStatus{}, maskAny(err)
	}
	if err := c.handleResponse(resp, "GET", url, &result); err != nil {
		return UpgradeStatus{}, maskAny(err)
	}

	return result, nil
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
		var er ErrorResponse
		if err := json.Unmarshal(body, &er); err == nil {
			return maskAny(StatusError{
				StatusCode: resp.StatusCode,
				message:    er.Error,
			})
		}
		return maskAny(StatusError{
			StatusCode: resp.StatusCode,
		})
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
