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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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

type sequencedAgencyConnection struct {
	driver_http.Connection
	do      func(attempt int, ctx context.Context) (driver_http.Response, error)
	attempt int
}

func (c *sequencedAgencyConnection) Do(ctx context.Context, _ driver_http.Request, _ interface{}, _ ...int) (driver_http.Response, error) {
	c.attempt++
	return c.do(c.attempt, ctx)
}

func newRedirectRetryTestClient(t *testing.T, endpointCount int, do func(attempt int, ctx context.Context) (driver_http.Response, error)) Agency {
	t.Helper()
	return newRedirectRetryTestClientWithTransport(t, endpointCount, nil, do)
}

func newRedirectRetryTestClientWithTransport(
	t *testing.T,
	endpointCount int,
	transport http.RoundTripper,
	do func(attempt int, ctx context.Context) (driver_http.Response, error),
) Agency {
	t.Helper()

	config := driver_http.HttpConfiguration{
		Endpoint:  driver_http.NewRoundRobinEndpoints([]string{"http://follower.invalid", "http://leader.invalid"}),
		Transport: transport,
	}
	base := driver_http.NewHttpConnection(config)
	result, err := NewAgency(&sequencedAgencyConnection{Connection: base, do: do}, endpointCount, &config)
	if err != nil {
		t.Fatalf("failed to create agency client: %v", err)
	}
	return result
}

type deadlineOnHostTransport struct {
	host string
	base http.RoundTripper
}

func (t *deadlineOnHostTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host == t.host {
		return nil, context.DeadlineExceeded
	}
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req)
}

type cancelOnHostTransport struct {
	host   string
	cancel context.CancelFunc
	base   http.RoundTripper
}

func (t *cancelOnHostTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host == t.host {
		t.cancel()
		return nil, context.Canceled
	}
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req)
}

func redirectTo(location string) driver_http.Response {
	return &redirectResponse{
		code:     http.StatusTemporaryRedirect,
		endpoint: "http://follower.invalid",
		header:   http.Header{"Location": []string{location}},
	}
}

func redirectWithoutLocation() driver_http.Response {
	return &redirectResponse{
		code:     http.StatusTemporaryRedirect,
		endpoint: "http://follower.invalid",
		header:   http.Header{},
	}
}

type noRedirectRequest struct {
	driver_http.Request
	body any
}

func (r *noRedirectRequest) SetBody(body any) error {
	r.body = body
	return r.Request.SetBody(body)
}

type noRedirectHTTPConnection struct {
	driver_http.Connection
	httpClient *http.Client
}

// newNoRedirectHTTPConnection keeps 307 responses visible to the agency client
// while still exercising real HTTP servers and the driver's round-robin endpoint selection.
func newNoRedirectHTTPConnection(config driver_http.HttpConfiguration) *noRedirectHTTPConnection {
	return &noRedirectHTTPConnection{
		Connection: driver_http.NewHttpConnection(config),
		httpClient: &http.Client{
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (c *noRedirectHTTPConnection) NewRequest(method string, urls ...string) (driver_http.Request, error) {
	req, err := c.Connection.NewRequest(method, urls...)
	if err != nil {
		return nil, err
	}
	return &noRedirectRequest{Request: req}, nil
}

func (c *noRedirectHTTPConnection) Do(
	ctx context.Context,
	request driver_http.Request,
	output interface{},
	_ ...int,
) (driver_http.Response, error) {
	req, ok := request.(*noRedirectRequest)
	if !ok {
		return nil, fmt.Errorf("unexpected request type %T", request)
	}
	body, err := json.Marshal(req.body)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, req.Method(), req.URL(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()

	if output != nil {
		if err := json.NewDecoder(httpResp.Body).Decode(output); err != nil && err != io.EOF {
			return nil, err
		}
	}
	return &redirectResponse{
		code:     httpResp.StatusCode,
		endpoint: req.Endpoint(),
		header:   httpResp.Header,
	}, nil
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

func newRoundRobinRedirectTestClient(t *testing.T, endpoints []string) Agency {
	t.Helper()

	config := driver_http.HttpConfiguration{
		Endpoint: driver_http.NewRoundRobinEndpoints(endpoints),
	}
	result, err := NewAgency(newNoRedirectHTTPConnection(config), len(endpoints), &config)
	if err != nil {
		t.Fatalf("failed to create agency client: %v", err)
	}
	return result
}

func localhostEndpoint(t *testing.T, rawURL string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil || u.Port() == "" {
		t.Fatalf("invalid test URL %q: %v", rawURL, err)
	}
	return "http://localhost:" + u.Port()
}

func newProductionAgencyClient(t *testing.T, endpoint string) Agency {
	t.Helper()
	config := driver_http.HttpConfiguration{
		Endpoint:       driver_http.NewRoundRobinEndpoints([]string{endpoint}),
		Authentication: driver_http.NewHeaderAuth("Authorization", testAuthorization),
	}
	conn, err := NewAgencyConnection(config)
	if err != nil {
		t.Fatalf("failed to create agency connection: %v", err)
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

func TestReadFollowsCrossHostRedirectWithProductionDriver(t *testing.T) {
	var authorization string
	leader := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		if r.Header.Get("Authorization") != testAuthorization {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":true,"code":401}`)
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/_api/agency/read" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"test":"value"}]`)
	}))
	defer leader.Close()

	follower := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", leader.URL+"/_api/agency/read")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer follower.Close()

	client := newProductionAgencyClient(t, localhostEndpoint(t, follower.URL))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var got string
	if err := client.ReadKey(ctx, []string{"test"}, &got); err != nil {
		t.Fatalf("production driver cross-host redirect failed: %v", err)
	}
	if got != "value" || authorization != testAuthorization {
		t.Fatalf("unexpected production redirect result: value=%q authorization=%q", got, authorization)
	}
}

func TestWriteFollowsCrossHostRedirectWithProductionDriver(t *testing.T) {
	var authorization string
	leader := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		if r.Header.Get("Authorization") != testAuthorization {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":true,"code":401}`)
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/_api/agency/write" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"results":[1]}`)
	}))
	defer leader.Close()

	follower := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", leader.URL+"/_api/agency/write")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer follower.Close()

	client := newProductionAgencyClient(t, localhostEndpoint(t, follower.URL))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := client.WriteKey(ctx, []string{"test"}, "value", 0); err != nil {
		t.Fatalf("production driver cross-host write redirect failed: %v", err)
	}
	if authorization != testAuthorization {
		t.Fatalf("production write redirect lost authentication: got %q", authorization)
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

func TestWriteReportsRedirectTargetFailure(t *testing.T) {
	leader := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":true,"code":401}`)
	}))
	defer leader.Close()

	client := newRedirectTestClient(t, leader.URL+"/_api/agency/write")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := client.WriteKey(ctx, []string{"test"}, "value", 0)
	if err == nil || !strings.Contains(err.Error(), "returned HTTP 401 with no result") {
		t.Fatalf("expected redirected write status error, got %v", err)
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

func TestReadPreservesRedirectErrorWhenRetryHitsDeadline(t *testing.T) {
	client := newRedirectRetryTestClient(t, 2, func(int, context.Context) (driver_http.Response, error) {
		return redirectWithoutLocation(), nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	var got string
	err := client.ReadKey(ctx, []string{"test"}, &got)
	if err == nil || !strings.Contains(err.Error(), "without Location") {
		t.Fatalf("expected preserved redirect diagnostic after retry deadline, got %v", err)
	}
}

func TestWritePreservesRedirectErrorWhenRetryHitsDeadline(t *testing.T) {
	client := newRedirectRetryTestClient(t, 2, func(int, context.Context) (driver_http.Response, error) {
		return redirectWithoutLocation(), nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := client.WriteKey(ctx, []string{"test"}, "value", 0)
	if err == nil || !strings.Contains(err.Error(), "without Location") {
		t.Fatalf("expected preserved redirect diagnostic after retry deadline, got %v", err)
	}
}

func TestReadPreservesRedirectErrorWhenNextRequestHitsDeadline(t *testing.T) {
	client := newRedirectRetryTestClient(t, 2, func(attempt int, ctx context.Context) (driver_http.Response, error) {
		if attempt == 1 {
			return redirectWithoutLocation(), nil
		}
		<-ctx.Done()
		return nil, ctx.Err()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	var got string
	err := client.ReadKey(ctx, []string{"test"}, &got)
	if err == nil || !strings.Contains(err.Error(), "without Location") {
		t.Fatalf("expected preserved redirect diagnostic after next request deadline, got %v", err)
	}
}

func TestReadReturnsCanceledWhenCallerCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := newRedirectRetryTestClient(t, 2, func(attempt int, _ context.Context) (driver_http.Response, error) {
		if attempt == 1 {
			cancel()
		}
		return redirectWithoutLocation(), nil
	})

	var got string
	err := client.ReadKey(ctx, []string{"test"}, &got)
	if !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "without Location") {
		t.Fatalf("expected caller cancellation without earlier agency error, got %v", err)
	}
}

func TestWriteReturnsCanceledWhenCallerCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := newRedirectRetryTestClient(t, 2, func(attempt int, _ context.Context) (driver_http.Response, error) {
		if attempt == 1 {
			cancel()
		}
		return redirectWithoutLocation(), nil
	})

	err := client.WriteKey(ctx, []string{"test"}, "value", 0)
	if !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "without Location") {
		t.Fatalf("expected caller cancellation without earlier agency error, got %v", err)
	}
}

func TestWritePreservesRedirectErrorWhenNextRequestHitsDeadline(t *testing.T) {
	client := newRedirectRetryTestClient(t, 2, func(attempt int, ctx context.Context) (driver_http.Response, error) {
		if attempt == 1 {
			return redirectWithoutLocation(), nil
		}
		<-ctx.Done()
		return nil, ctx.Err()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	err := client.WriteKey(ctx, []string{"test"}, "value", 0)
	if err == nil || !strings.Contains(err.Error(), "without Location") {
		t.Fatalf("expected preserved redirect diagnostic after next request deadline, got %v", err)
	}
}

func TestReadRetriesNextEndpointAfterRedirectTargetFailure(t *testing.T) {
	failedTarget := httptest.NewServer(http.NotFoundHandler())
	failedTargetURL := failedTarget.URL
	failedTarget.Close()

	followerRequests := 0
	follower := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		followerRequests++
		if r.Method != http.MethodPost || r.URL.Path != "/_api/agency/read" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Location", failedTargetURL+"/_api/agency/read")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer follower.Close()

	leaderRequests := 0
	leader := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaderRequests++
		if r.Method != http.MethodPost || r.URL.Path != "/_api/agency/read" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"test":"value"}]`)
	}))
	defer leader.Close()

	client := newRoundRobinRedirectTestClient(t, []string{follower.URL, leader.URL})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var got string
	if err := client.ReadKey(ctx, []string{"test"}, &got); err != nil {
		t.Fatalf("agency read did not fail over after redirect target failure: %v", err)
	}
	if got != "value" || followerRequests != 1 || leaderRequests != 1 {
		t.Fatalf("unexpected failover result: value=%q followerRequests=%d leaderRequests=%d",
			got, followerRequests, leaderRequests)
	}
}

func TestWriteRetriesNextEndpointWithOriginalTransaction(t *testing.T) {
	failedTarget := httptest.NewServer(http.NotFoundHandler())
	failedTargetURL := failedTarget.URL
	failedTarget.Close()

	followerRequests := 0
	follower := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		followerRequests++
		if r.Method != http.MethodPost || r.URL.Path != "/_api/agency/write" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Location", failedTargetURL+"/_api/agency/write")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer follower.Close()

	leaderRequests := 0
	leader := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaderRequests++
		if r.Method != http.MethodPost || r.URL.Path != "/_api/agency/write" {
			http.NotFound(w, r)
			return
		}

		var body [][]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("failed to decode agency write: %v", err)
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		if len(body) != 1 || len(body[0]) != 3 {
			t.Errorf("unexpected agency write body: %v", body)
			http.Error(w, "invalid transaction", http.StatusBadRequest)
			return
		}

		var operations map[string]map[string]any
		var preconditions map[string]map[string]any
		if err := json.Unmarshal(body[0][0], &operations); err != nil {
			t.Errorf("failed to decode operations: %v", err)
		}
		if err := json.Unmarshal(body[0][1], &preconditions); err != nil {
			t.Errorf("failed to decode preconditions: %v", err)
		}
		if operation := operations["/test"]; operation["op"] != "set" || operation["new"] != "value" {
			t.Errorf("write operation was not preserved: %+v", operations)
		}
		if condition := preconditions["/guard"]; condition["old"] != "expected" {
			t.Errorf("write precondition was not preserved: %+v", preconditions)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"results":[1]}`)
	}))
	defer leader.Close()

	client := newRoundRobinRedirectTestClient(t, []string{follower.URL, leader.URL})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := client.Write(ctx, &Transaction{
		Ops:   []KeyChanger{SetKey([]string{"test"}, "value")},
		Conds: []WriteCondition{IfEqualTo([]string{"guard"}, "expected")},
	})
	if err != nil {
		t.Fatalf("agency write did not fail over after redirect target failure: %v", err)
	}
	if followerRequests != 1 || leaderRequests != 1 {
		t.Fatalf("unexpected write attempts: follower=%d leader=%d", followerRequests, leaderRequests)
	}
}

func TestReadPreservesRedirectErrorAfterAllEndpointsFail(t *testing.T) {
	failedTarget := httptest.NewServer(http.NotFoundHandler())
	failedTargetURL := failedTarget.URL
	failedTarget.Close()

	newFollower := func() *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", failedTargetURL+r.URL.Path)
			w.WriteHeader(http.StatusTemporaryRedirect)
		}))
	}
	follower1 := newFollower()
	defer follower1.Close()
	follower2 := newFollower()
	defer follower2.Close()

	client := newRoundRobinRedirectTestClient(t, []string{follower1.URL, follower2.URL})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var got string
	err := client.ReadKey(ctx, []string{"test"}, &got)
	if err == nil || !strings.Contains(err.Error(), "agency read redirect to "+failedTargetURL) ||
		!strings.Contains(err.Error(), "failed") {
		t.Fatalf("expected redirect target error after exhausting endpoints, got %v", err)
	}
}

func TestWritePreservesRedirectErrorAfterAllEndpointsFail(t *testing.T) {
	failedTarget := httptest.NewServer(http.NotFoundHandler())
	failedTargetURL := failedTarget.URL
	failedTarget.Close()

	newFollower := func() *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", failedTargetURL+r.URL.Path)
			w.WriteHeader(http.StatusTemporaryRedirect)
		}))
	}
	follower1 := newFollower()
	defer follower1.Close()
	follower2 := newFollower()
	defer follower2.Close()

	client := newRoundRobinRedirectTestClient(t, []string{follower1.URL, follower2.URL})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := client.Write(ctx, &Transaction{
		Ops:   []KeyChanger{SetKey([]string{"test"}, "value")},
		Conds: []WriteCondition{IfEqualTo([]string{"guard"}, "expected")},
	})
	if err == nil || !strings.Contains(err.Error(), "agency write redirect to "+failedTargetURL) ||
		!strings.Contains(err.Error(), "failed") {
		t.Fatalf("expected redirect target error after exhausting endpoints, got %v", err)
	}
}

func TestReadPreservesRedirectErrorWhenNextRedirectTimesOut(t *testing.T) {
	concrete := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":true,"code":401}`)
	}))
	defer concrete.Close()

	const timeoutHost = "timeout.invalid"
	client := newRedirectRetryTestClientWithTransport(t, 2, &deadlineOnHostTransport{host: timeoutHost},
		func(attempt int, _ context.Context) (driver_http.Response, error) {
			if attempt == 1 {
				return redirectTo(concrete.URL + "/_api/agency/read"), nil
			}
			return redirectTo("http://" + timeoutHost + "/_api/agency/read"), nil
		})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var got string
	err := client.ReadKey(ctx, []string{"test"}, &got)
	if err == nil || !strings.Contains(err.Error(), "code=401") {
		t.Fatalf("expected first redirect diagnostic after later redirect timeout, got %v", err)
	}
}

func TestWritePreservesRedirectErrorWhenNextRedirectTimesOut(t *testing.T) {
	concrete := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":true,"code":401}`)
	}))
	defer concrete.Close()

	const timeoutHost = "timeout.invalid"
	client := newRedirectRetryTestClientWithTransport(t, 2, &deadlineOnHostTransport{host: timeoutHost},
		func(attempt int, _ context.Context) (driver_http.Response, error) {
			if attempt == 1 {
				return redirectTo(concrete.URL + "/_api/agency/write"), nil
			}
			return redirectTo("http://" + timeoutHost + "/_api/agency/write"), nil
		})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := client.Write(ctx, &Transaction{
		Ops:   []KeyChanger{SetKey([]string{"test"}, "value")},
		Conds: []WriteCondition{IfEqualTo([]string{"guard"}, "expected")},
	})
	if err == nil || !strings.Contains(err.Error(), "HTTP 401") {
		t.Fatalf("expected first redirect diagnostic after later redirect timeout, got %v", err)
	}
}

func TestReadReturnsCanceledWhenCallerCancelsDuringFinalRedirect(t *testing.T) {
	concrete := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":true,"code":401}`)
	}))
	defer concrete.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	const cancelHost = "cancel.invalid"
	client := newRedirectRetryTestClientWithTransport(t, 2, &cancelOnHostTransport{host: cancelHost, cancel: cancel},
		func(attempt int, _ context.Context) (driver_http.Response, error) {
			if attempt == 1 {
				return redirectTo(concrete.URL + "/_api/agency/read"), nil
			}
			return redirectTo("http://" + cancelHost + "/_api/agency/read"), nil
		})

	var got string
	err := client.ReadKey(ctx, []string{"test"}, &got)
	if !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "401") {
		t.Fatalf("expected caller cancellation during final redirect, got %v", err)
	}
}

func TestWriteReturnsCanceledWhenCallerCancelsDuringFinalRedirect(t *testing.T) {
	concrete := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":true,"code":401}`)
	}))
	defer concrete.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	const cancelHost = "cancel.invalid"
	client := newRedirectRetryTestClientWithTransport(t, 2, &cancelOnHostTransport{host: cancelHost, cancel: cancel},
		func(attempt int, _ context.Context) (driver_http.Response, error) {
			if attempt == 1 {
				return redirectTo(concrete.URL + "/_api/agency/write"), nil
			}
			return redirectTo("http://" + cancelHost + "/_api/agency/write"), nil
		})

	err := client.Write(ctx, &Transaction{
		Ops:   []KeyChanger{SetKey([]string{"test"}, "value")},
		Conds: []WriteCondition{IfEqualTo([]string{"guard"}, "expected")},
	})
	if !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "401") {
		t.Fatalf("expected caller cancellation during final redirect, got %v", err)
	}
}
