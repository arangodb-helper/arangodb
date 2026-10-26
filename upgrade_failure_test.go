//
// DISCLAIMER
//
// Copyright 2026 ArangoDB GmbH, Cologne, Germany
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

package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/arangodb-helper/arangodb/client"
	"github.com/stretchr/testify/require"
)

func TestUpgradeCommandReportsFailedPlan(t *testing.T) {
	reason := "dbserver auto-upgrade exited with code 1 (exit with error)\n2026-10-26T10:51:46Z ERROR [abcde] {engines} failed to open '_system': shutdown in progress"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("OK"))
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(client.UpgradeStatus{
				Failed: true,
				Reason: reason,
			})
		default:
			http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
		}
	}))
	defer srv.Close()

	oldLog := opts.log
	opts.log.console = true
	opts.log.file = false
	opts.log.color = false
	opts.log.verbose = false
	defer func() { opts.log = oldLog }()

	readErr, writeErr, err := os.Pipe()
	require.NoError(t, err)
	oldStderr := os.Stderr
	os.Stderr = writeErr
	defer func() { os.Stderr = oldStderr }()

	runUpgrade(srv.URL, false, false)

	require.NoError(t, writeErr.Close())
	out, err := io.ReadAll(readErr)
	require.NoError(t, err)
	require.Contains(t, string(out), "Database upgrade has failed")
	require.Contains(t, string(out), "exited with code 1")
	require.Contains(t, string(out), "failed to open '_system'")
}
