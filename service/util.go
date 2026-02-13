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

package service

import (
	"crypto/rand"
	"encoding/hex"
	"net"
	"net/http"
	"net/url"
	"strings"

	driver_shared "github.com/arangodb/go-driver/v2/arangodb/shared"
)

// createUniqueID creates a new random ID.
func createUniqueID() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", maskAny(err)
	}
	return hex.EncodeToString(b), nil
}

// normalizeHostName normalizes all loopback addresses to "localhost"
func normalizeHostName(host string) string {
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() {
			return "localhost"
		}
	}
	return host
}

// For Windows we need to change backslashes to slashes, strangely enough:
func slasher(s string) string {
	return strings.Replace(s, "\\", "/", -1)
}

// boolRef returns a reference to given bool
func boolRef(v bool) *bool {
	return &v
}

// copyBoolRef returns a clone of the given reference
func copyBoolRef(v *bool) *bool {
	if v == nil {
		return nil
	}
	return boolRef(*v)
}

// boolFromRef returns a boolean from given reference, returning given default value
// when reference is nil.
func boolFromRef(v *bool, defaultValue bool) bool {
	if v == nil {
		return defaultValue
	}
	return *v
}

// getURLWithPath returns an URL consisting of the given rootURL with the given relative path.
func getURLWithPath(rootURL string, relPath string) (string, error) {
	u, err := url.Parse(rootURL)
	if err != nil {
		return "", maskAny(err)
	}
	parts := strings.SplitN(relPath, "?", 2)
	u.Path = parts[0]
	u.RawQuery = ""
	query := ""
	if len(parts) > 1 {
		query = "?" + parts[1]
	}
	return u.String() + query, nil
}

type causer interface {
	Cause() error
}

func getErrorCodeFromError(err error) int {
	if e, ok := err.(driver_shared.ArangoError); ok {
		return e.Code
	}

	if c, ok := err.(causer); ok && c.Cause() != err {
		return getErrorCodeFromError(c.Cause())
	}

	return http.StatusInternalServerError
}
