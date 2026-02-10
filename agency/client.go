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
	"net/http"

	driver_http "github.com/arangodb/go-driver/v2/connection"
)

type client struct {
	conn driver_http.Connection
	http *http.Client
}

var _ Agency = (*client)(nil)

// NewAgencyConnection creates a new HTTP connection for agency operations.
// It wraps driver_http.NewHttpConnection to provide a consistent interface
// that returns an error (for consistency with other connection creation patterns).
func NewAgencyConnection(config driver_http.HttpConfiguration) (driver_http.Connection, error) {
	if config.Endpoint == nil {
		return nil, fmt.Errorf("agency.NewAgencyConnection: endpoint cannot be nil")
	}
	conn := driver_http.NewHttpConnection(config)
	return conn, nil
}

func NewAgency(conn driver_http.Connection) (Agency, error) {
	if conn == nil {
		return nil, fmt.Errorf("agency.New: connection cannot be nil")
	}
	return &client{
		conn: conn,
		http: &http.Client{},
	}, nil
}

func (c *client) RemoveKeyIfEqualTo(ctx context.Context, key []string, oldValue interface{}) error {
	tx := &Transaction{
		Ops: []KeyChanger{
			RemoveKey(key),
		},
		Conds: []WriteCondition{
			KeyEquals(key, "", oldValue), // adjust depending on your condition struct
		},
	}
	return c.Write(ctx, tx)
}
