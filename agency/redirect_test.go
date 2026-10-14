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
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	driver_http "github.com/arangodb/go-driver/v2/connection"
)

const testAuthorization = "Bearer bts2482-test-token"

type scriptedRedirectConnection struct {
	driver_http.Connection
	response driver_http.Response
}

func (c *scriptedRedirectConnection) Do(context.Context, driver_http.Request, interface{}, ...int) (driver_http.Response, error) {
	return c.response, nil
}

type redirectResponse struct {
	code     int
	endpoint string
	header   http.Header
}

func (r *redirectResponse) Code() int                 { return r.code }
func (r *redirectResponse) Response() interface{}     { return nil }
func (r *redirectResponse) Endpoint() string          { return r.endpoint }
func (r *redirectResponse) Content() string           { return "application/json" }
func (r *redirectResponse) Header(name string) string { return r.header.Get(name) }
func (r *redirectResponse) RawResponse() *http.Response {
	return &http.Response{StatusCode: r.code, Header: r.header}
}

func newRedirectTestClient(t *testing.T, location string) Agency {
	t.Helper()

	config := driver_http.HttpConfiguration{
		Endpoint:       driver_http.NewRoundRobinEndpoints([]string{"http://follower.invalid"}),
		Authentication: driver_http.NewHeaderAuth("Authorization", testAuthorization),
	}
	base := driver_http.NewHttpConnection(config)
	conn := &scriptedRedirectConnection{
		Connection: base,
		response: &redirectResponse{
			code:     http.StatusTemporaryRedirect,
			endpoint: "http://follower.invalid",
			header:   http.Header{"Location": []string{location}},
		},
	}
	result, err := NewAgency(conn, 1, &config)
	if err != nil {
		t.Fatalf("failed to create agency client: %v", err)
	}
	return result
}

func TestReadFollowsAgencyRedirectWithAuthentication(t *testing.T) {
	var authorization string
	leader := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		if r.Method != http.MethodPost || r.URL.Path != "/_api/agency/read" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"arangodb-helper":{"arangodb":{"leader":{"data":"http://starter:8528/","ttl":4102444800}}}}]`)
	}))
	defer leader.Close()

	client := newRedirectTestClient(t, leader.URL+"/_api/agency/read")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	var got leaderStruct[string]
	if err := client.ReadKey(ctx, []string{"arangodb-helper", "arangodb", "leader"}, &got); err != nil {
		t.Fatalf("redirected agency read failed: %v", err)
	}
	if got.Data != "http://starter:8528/" || got.TTL != 4102444800 {
		t.Fatalf("unexpected leader value: %+v", got)
	}
	if authorization != testAuthorization {
		t.Fatalf("redirected request lost authentication: got %q", authorization)
	}
}

func TestWriteFollowsAgencyRedirectWithAuthentication(t *testing.T) {
	var authorization string
	leader := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		if r.Method != http.MethodPost || r.URL.Path != "/_api/agency/write" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"results":[1]}`)
	}))
	defer leader.Close()

	client := newRedirectTestClient(t, leader.URL+"/_api/agency/write")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := client.WriteKey(ctx, []string{"test"}, "value", 0); err != nil {
		t.Fatalf("redirected agency write failed: %v", err)
	}
	if authorization != testAuthorization {
		t.Fatalf("redirected request lost authentication: got %q", authorization)
	}
}

func TestReadReportsRedirectTargetFailure(t *testing.T) {
	leader := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":true,"code":401}`)
	}))
	defer leader.Close()

	client := newRedirectTestClient(t, leader.URL+"/_api/agency/read")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	var got string
	err := client.ReadKey(ctx, []string{"test"}, &got)
	if err == nil || !strings.Contains(err.Error(), "redirect read failed: code=401") {
		t.Fatalf("expected redirect target error, got %v", err)
	}
}

func TestReadReportsRedirectWithoutLocation(t *testing.T) {
	client := newRedirectTestClient(t, "")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	var got string
	err := client.ReadKey(ctx, []string{"test"}, &got)
	if err == nil || !strings.Contains(err.Error(), "without Location") {
		t.Fatalf("expected missing Location error, got %v", err)
	}
}
