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

package service

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestRecoveryContactAddresses(t *testing.T) {
	t.Parallel()

	addresses := recoveryContactAddresses(
		[]string{"node1:8528", "node2:8528", "node3:8528"},
		"node1:8528",
	)
	if len(addresses) != 2 {
		t.Fatalf("expected 2 contact addresses, got %d: %v", len(addresses), addresses)
	}
	if addresses[0] != "node2:8528" || addresses[1] != "node3:8528" {
		t.Fatalf("unexpected contact addresses: %v", addresses)
	}

	if len(recoveryContactAddresses([]string{"node1:8528"}, "node1:8528")) != 0 {
		t.Fatal("expected no contact addresses when only the recovery address is configured")
	}

	if len(recoveryContactAddresses([]string{"127.0.0.1:8528"}, "127.0.0.1:8528")) != 0 {
		t.Fatal("expected no contact addresses for self-only loopback join")
	}
	if len(recoveryContactAddresses([]string{"127.0.0.1:8528"}, "localhost:8528")) != 0 {
		t.Fatal("expected 127.0.0.1 and localhost to be treated as the same starter address")
	}

	addresses = recoveryContactAddresses(
		[]string{"node2:8528", " node3:8528"},
		"node1:8528",
	)
	if len(addresses) != 2 {
		t.Fatalf("expected 2 contact addresses, got %d: %v", len(addresses), addresses)
	}
	if addresses[0] != "node2:8528" || addresses[1] != "node3:8528" {
		t.Fatalf("expected trimmed join addresses, got %v", addresses)
	}
}

func TestGetCoordinatorEndpointsExcludingPeerID(t *testing.T) {
	t.Parallel()

	cfg := ClusterConfig{
		AllPeers: []Peer{
			{ID: "master", Address: "localhost", Port: 8528, peerServers: peerServers{HasCoordinatorFlag: boolRef(true), HasAgentFlag: true}},
			{ID: "slave1", Address: "localhost", Port: 8628, peerServers: peerServers{HasCoordinatorFlag: boolRef(true), HasAgentFlag: true}},
			{ID: "slave2", Address: "localhost", Port: 8728, peerServers: peerServers{HasCoordinatorFlag: boolRef(true), HasAgentFlag: true}},
		},
	}

	all, err := cfg.GetCoordinatorEndpoints()
	if err != nil {
		t.Fatalf("GetCoordinatorEndpoints failed: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 coordinator endpoints, got %d: %v", len(all), all)
	}

	survivors, err := cfg.GetCoordinatorEndpointsExcludingPeerID("master")
	if err != nil {
		t.Fatalf("GetCoordinatorEndpointsExcludingPeerID failed: %v", err)
	}
	if len(survivors) != 2 {
		t.Fatalf("expected 2 survivor coordinator endpoints, got %d: %v", len(survivors), survivors)
	}
	for _, ep := range survivors {
		if strings.Contains(ep, ":8529") {
			t.Fatalf("bootstrap master coordinator must be excluded, got %v", survivors)
		}
	}

	onlyPeerCfg := ClusterConfig{
		AllPeers: []Peer{
			{ID: "only", Address: "localhost", Port: 8528, peerServers: peerServers{HasCoordinatorFlag: boolRef(true)}},
		},
	}
	if _, err := onlyPeerCfg.GetCoordinatorEndpointsExcludingPeerID("only"); err == nil {
		t.Fatal("expected error when the only coordinator belongs to the excluded peer")
	}
}

func TestHandleHelloRecoveryRequestReturnsLocalConfig(t *testing.T) {
	t.Parallel()

	deadMaster := Peer{ID: "dead", Address: "node1", Port: 8528}
	liveSlave := Peer{ID: "live", Address: "node2", Port: 8528}
	clusterConfig := ClusterConfig{
		AllPeers:   []Peer{deadMaster, liveSlave},
		AgencySize: 3,
	}

	s := &Service{
		log:   zerolog.Nop(),
		state: stateRunningSlave,
		runtimeClusterManager: runtimeClusterManager{
			lastMasterURL: "http://node1:8528/",
			myPeers:       clusterConfig,
		},
	}

	result, err := s.HandleHello("node2", "127.0.0.1:12345", nil, false, true)
	if err != nil {
		t.Fatalf("HandleHello failed: %v", err)
	}
	if len(result.AllPeers) != 2 {
		t.Fatalf("expected local cluster config with 2 peers, got %d", len(result.AllPeers))
	}
}

func TestHandleHelloWithoutRecoveryRedirectsRunningSlave(t *testing.T) {
	t.Parallel()

	s := &Service{
		log:   zerolog.Nop(),
		state: stateRunningSlave,
		runtimeClusterManager: runtimeClusterManager{
			lastMasterURL: "http://node1:8528/",
			myPeers:       ClusterConfig{AllPeers: []Peer{{ID: "live", Address: "node2", Port: 8528}}},
		},
	}

	_, err := s.HandleHello("node2", "127.0.0.1:12345", nil, false, false)
	if err == nil {
		t.Fatal("expected redirect error without recovery flag")
	}
	if _, ok := IsRedirect(err); !ok {
		t.Fatalf("expected redirect error, got: %v", err)
	}
}

func TestGetRecoveryClusterConfigFromLiveStarter(t *testing.T) {
	t.Parallel()

	clusterConfig := ClusterConfig{
		AllPeers: []Peer{
			{ID: "master", Address: "node1", Port: 8528},
			{ID: "slave", Address: "node2", Port: 8528},
		},
		AgencySize: 3,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/hello" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get(recoveryQueryParam) == "1" {
			_ = json.NewEncoder(w).Encode(clusterConfig)
			return
		}
		http.Redirect(w, r, "http://node1:8528/hello", http.StatusTemporaryRedirect)
	}))
	defer srv.Close()

	host, portStr, err := net.SplitHostPort(srv.Listener.Addr().String())
	if err != nil {
		t.Fatalf("SplitHostPort failed: %v", err)
	}

	s := &Service{
		log: zerolog.Nop(),
		cfg: Config{MasterPort: DefaultMasterPort},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := s.getRecoveryClusterConfig(ctx,
		[]string{net.JoinHostPort(host, portStr), "node1:8528"},
		"node1:8528",
	)
	if err != nil {
		t.Fatalf("getRecoveryClusterConfig failed: %v", err)
	}
	if len(result.AllPeers) != 2 {
		t.Fatalf("expected cluster config with 2 peers, got %d", len(result.AllPeers))
	}
}

func TestGetRecoveryClusterConfigFollowsRedirectWithRecoveryParam(t *testing.T) {
	t.Parallel()

	clusterConfig := ClusterConfig{
		AllPeers: []Peer{
			{ID: "master", Address: "node1", Port: 8528},
			{ID: "slave", Address: "node2", Port: 8528},
		},
		AgencySize: 3,
	}

	var masterRequests []*http.Request
	master := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		masterRequests = append(masterRequests, r)
		if r.URL.Path != "/hello" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get(recoveryQueryParam) != "1" {
			t.Errorf("expected recovery=1 on redirected request, got query %q", r.URL.RawQuery)
			http.Error(w, "missing recovery query param", http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(clusterConfig)
	}))
	defer master.Close()

	masterHost, masterPort, err := net.SplitHostPort(master.Listener.Addr().String())
	if err != nil {
		t.Fatalf("SplitHostPort failed: %v", err)
	}
	masterHelloURL := "http://" + net.JoinHostPort(masterHost, masterPort) + "/hello?update=1"

	slave := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/hello" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get(recoveryQueryParam) != "1" {
			http.Error(w, "expected recovery request", http.StatusBadRequest)
			return
		}
		http.Redirect(w, r, masterHelloURL, http.StatusFound)
	}))
	defer slave.Close()

	slaveHost, slavePort, err := net.SplitHostPort(slave.Listener.Addr().String())
	if err != nil {
		t.Fatalf("SplitHostPort failed: %v", err)
	}

	s := &Service{
		log: zerolog.Nop(),
		cfg: Config{MasterPort: DefaultMasterPort},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := s.getRecoveryClusterConfig(ctx,
		[]string{net.JoinHostPort(slaveHost, slavePort)},
		"node1:8528",
	)
	if err != nil {
		t.Fatalf("getRecoveryClusterConfig failed: %v", err)
	}
	if len(result.AllPeers) != 2 {
		t.Fatalf("expected cluster config with 2 peers, got %d", len(result.AllPeers))
	}
	if len(masterRequests) != 1 {
		t.Fatalf("expected exactly one redirected request to master, got %d", len(masterRequests))
	}
	if masterRequests[0].URL.Query().Get("update") != "1" {
		t.Fatalf("expected existing query params to be preserved, got %q", masterRequests[0].URL.RawQuery)
	}
}

func TestGetRecoveryClusterConfigRejectsRedirectToRecoveryAddress(t *testing.T) {
	t.Parallel()

	clusterConfig := ClusterConfig{
		AllPeers: []Peer{
			{ID: "master", Address: "node1", Port: 8528},
			{ID: "slave", Address: "node2", Port: 8528},
		},
		AgencySize: 3,
	}

	redirectToRecovery := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get(recoveryQueryParam) != "1" {
			http.Error(w, "expected recovery request", http.StatusBadRequest)
			return
		}
		http.Redirect(w, r, "http://node1:8528/hello", http.StatusTemporaryRedirect)
	}))
	defer redirectToRecovery.Close()

	live := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get(recoveryQueryParam) == "1" {
			_ = json.NewEncoder(w).Encode(clusterConfig)
			return
		}
		http.Error(w, "expected recovery request", http.StatusBadRequest)
	}))
	defer live.Close()

	badHost, badPort, err := net.SplitHostPort(redirectToRecovery.Listener.Addr().String())
	if err != nil {
		t.Fatalf("SplitHostPort failed: %v", err)
	}
	goodHost, goodPort, err := net.SplitHostPort(live.Listener.Addr().String())
	if err != nil {
		t.Fatalf("SplitHostPort failed: %v", err)
	}

	s := &Service{
		log: zerolog.Nop(),
		cfg: Config{MasterPort: DefaultMasterPort},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// First contact redirects to the node being recovered and must be skipped; second answers locally.
	result, err := s.getRecoveryClusterConfig(ctx,
		[]string{
			net.JoinHostPort(badHost, badPort),
			net.JoinHostPort(goodHost, goodPort),
		},
		"node1:8528",
	)
	if err != nil {
		t.Fatalf("getRecoveryClusterConfig failed: %v", err)
	}
	if len(result.AllPeers) != 2 {
		t.Fatalf("expected cluster config with 2 peers, got %d", len(result.AllPeers))
	}
}

func TestGetRecoveryClusterConfigNoContactAddresses(t *testing.T) {
	t.Parallel()

	s := &Service{log: zerolog.Nop(), cfg: Config{MasterPort: DefaultMasterPort}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := s.getRecoveryClusterConfig(ctx, []string{"node1:8528"}, "node1:8528")
	if err == nil {
		t.Fatal("expected error when no contact addresses remain")
	}
	if !strings.Contains(err.Error(), "no remaining starter addresses in --starter.join") {
		t.Fatalf("expected self-join error message, got: %v", err)
	}
}
