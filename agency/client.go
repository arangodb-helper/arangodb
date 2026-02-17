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
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"syscall"

	driver_http "github.com/arangodb/go-driver/v2/connection"
)

type client struct {
	conn          driver_http.Connection
	http          *http.Client
	endpointCount int
}

// isConnectionError returns true if the error is a TCP-level connection error
// (as opposed to an HTTP-level error). This is used to decide whether to retry
// on the next agency endpoint.
func isConnectionError(err error) bool {
	if err == nil {
		return false
	}

	// Unwrap the error chain to check all wrapped errors
	for err != nil {
		// Check for net.OpError (network operation errors)
		var opErr *net.OpError
		if errors.As(err, &opErr) {
			// Check for dial operations (connection attempts) - these are always connection errors
			if opErr.Op == "dial" {
				return true
			}
			// Check for specific syscall errors that indicate connection issues
			if opErr.Err != nil {
				var errno syscall.Errno
				if errors.As(opErr.Err, &errno) {
					switch errno {
					case syscall.ECONNREFUSED, syscall.ECONNRESET, syscall.ETIMEDOUT,
						syscall.EHOSTUNREACH, syscall.ENETUNREACH, syscall.EAGAIN:
						return true
					}
				}
				// Check for timeout errors
				if opErr.Timeout() {
					return true
				}
			}
		}

		// Check for DNS errors
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) {
			return true
		}

		// Check error string for connection-related patterns
		errStr := err.Error()
		if strings.Contains(errStr, "connection refused") ||
			strings.Contains(errStr, "connection reset") ||
			strings.Contains(errStr, "no such host") ||
			strings.Contains(errStr, "i/o timeout") ||
			strings.Contains(errStr, "EOF") ||
			strings.Contains(errStr, "dial tcp") ||
			strings.Contains(errStr, "dial:") ||
			strings.Contains(errStr, "network is unreachable") ||
			strings.Contains(errStr, "host is unreachable") ||
			strings.Contains(errStr, "connect: connection refused") {
			return true
		}

		// Try to unwrap the error for next iteration
		err = errors.Unwrap(err)
	}

	return false
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

func NewAgency(conn driver_http.Connection, endpointCount int) (Agency, error) {
	if conn == nil {
		return nil, fmt.Errorf("agency.New: connection cannot be nil")
	}
	if endpointCount < 1 {
		endpointCount = 1
	}
	return &client{
		conn:          conn,
		http:          &http.Client{},
		endpointCount: endpointCount,
	}, nil
}

func (c *client) RemoveKeyIfEqualTo(ctx context.Context, key []string, oldValue interface{}) error {
	if len(key) == 0 {
		return fmt.Errorf("agency.RemoveKeyIfEqualTo: key cannot be empty")
	}
	parentPath := key[:len(key)-1]
	lastElement := key[len(key)-1]
	tx := &Transaction{
		Ops: []KeyChanger{
			RemoveKey(key),
		},
		Conds: []WriteCondition{
			// Only remove the key if its current value in the agency matches oldValue.
			KeyEquals(parentPath, lastElement, oldValue),
		},
	}
	return c.Write(ctx, tx)
}
