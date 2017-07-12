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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"

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
func (c *client) ReadKey(ctx context.Context, key []string, output interface{}) error {
	url := c.createURL("/read", nil)

	reqBody := fmt.Sprintf(`[["%s"]]`, "/"+strings.Join(key, "/"))
	fmt.Printf("Sending `%s` to %s\n", reqBody, url)
	req, err := http.NewRequest("POST", url, strings.NewReader(reqBody))
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
	var result []map[string]interface{}
	if err := c.handleResponse(resp, "POST", url, &result); err != nil {
		return maskAny(err)
	}
	// Result should be 1 long
	if len(result) != 1 {
		return maskAny(fmt.Errorf("Expected result of 1 long, got %d", len(result)))
	}
	var ptr interface{}
	ptr = result[0]
	// Resolve result
	for _, keyElem := range key {
		if ptrMap, ok := ptr.(map[string]interface{}); !ok {
			return maskAny(fmt.Errorf("Expected map as key element '%s' got %v", keyElem, ptr))
		} else {
			if child, found := ptrMap[keyElem]; !found {
				return maskAny(fmt.Errorf("Cannot find key element '%s'", keyElem))
			} else {
				ptr = child
			}
		}
	}

	// Now ptr contain reference to our data
	// Copy it into output by marshalling & unmarshalling
	raw, err := json.Marshal(ptr)
	if err != nil {
		return maskAny(err)
	}
	if err := json.Unmarshal(raw, output); err != nil {
		return maskAny(err)
	}

	return nil
}

// WriteKeyIfEmpty writes the given value with the given key only if the key was empty before.
func (c *client) WriteKeyIfEmpty(ctx context.Context, key []string, value interface{}) error {
	return maskAny(fmt.Errorf("Not implemented"))
}

// WriteKeyIfEqualTo writes the given new value with the given key only if the existing value for that key equals
// to the given old value.
func (c *client) WriteKeyIfEqualTo(ctx context.Context, key []string, newValue, oldValue interface{}) error {
	return maskAny(fmt.Errorf("Not implemented"))
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
