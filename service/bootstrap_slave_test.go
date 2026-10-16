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
package service

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func registerPeerTestConfig(id string) ClusterConfig {
	return ClusterConfig{
		ServerStorageEngine: "rocksdb",
		AgencySize:          3,
		AllPeers:            []Peer{{ID: id, Address: "127.0.0.1", Port: 8528}},
	}
}

func writeRegisterPeerConfig(t *testing.T, w http.ResponseWriter, id string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(registerPeerTestConfig(id)); err != nil {
		t.Errorf("failed to write cluster config: %v", err)
	}
}

func TestRegisterPeerSuccess(t *testing.T) {
	const id = "joiner"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/hello" {
			http.NotFound(w, r)
			return
		}
		writeRegisterPeerConfig(t, w, id)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, err := registerPeer(ctx, zerolog.Nop(), server.URL, HelloRequest{SlaveID: id}, time.Second, time.Millisecond)
	if err != nil {
		t.Fatalf("registration failed: %v", err)
	}
	if _, found := got.PeerByID(id); !found {
		t.Fatalf("registered config does not contain %q: %+v", id, got.AllPeers)
	}
}

func TestRegisterPeerServiceUnavailableTimesOut(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.Error(w, "No master known", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := registerPeer(ctx, zerolog.Nop(), server.URL, HelloRequest{SlaveID: "joiner"}, 25*time.Millisecond, 5*time.Millisecond)
	if !errors.Is(err, ErrRegisterPeerUnavailable) {
		t.Fatalf("expected service-unavailable timeout, got %v", err)
	}
	if !strings.Contains(err.Error(), "No master known") {
		t.Fatalf("expected response diagnostics in error, got %v", err)
	}
	if calls.Load() < 2 {
		t.Fatalf("expected repeated 503 responses, got %d request(s)", calls.Load())
	}
}

func TestRegisterPeerServiceUnavailableRecovers(t *testing.T) {
	const id = "joiner"
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) <= 2 {
			http.Error(w, "No master known", http.StatusServiceUnavailable)
			return
		}
		writeRegisterPeerConfig(t, w, id)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, err := registerPeer(ctx, zerolog.Nop(), server.URL, HelloRequest{SlaveID: id}, 100*time.Millisecond, 5*time.Millisecond)
	if err != nil {
		t.Fatalf("expected recovery before timeout, got %v", err)
	}
	if _, found := got.PeerByID(id); !found || calls.Load() != 3 {
		t.Fatalf("unexpected recovery result: peers=%+v calls=%d", got.AllPeers, calls.Load())
	}
}

func TestRegisterPeerNotFoundResetsUnavailableBudget(t *testing.T) {
	const id = "joiner"
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		switch calls.Add(1) {
		case 1:
			http.Error(w, "No master known", http.StatusServiceUnavailable)
		case 2:
			w.WriteHeader(http.StatusNotFound)
		case 3:
			http.Error(w, "No master known", http.StatusServiceUnavailable)
		default:
			writeRegisterPeerConfig(t, w, id)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, err := registerPeer(ctx, zerolog.Nop(), server.URL, HelloRequest{SlaveID: id}, 12*time.Millisecond, 8*time.Millisecond)
	if err != nil {
		t.Fatalf("non-consecutive 503 responses consumed one timeout budget: %v", err)
	}
	if _, found := got.PeerByID(id); !found {
		t.Fatalf("registered config does not contain %q", id)
	}
}

func TestRegisterPeerConnectionErrorsDoNotConsumeUnavailableBudget(t *testing.T) {
	const id = "joiner"
	reserved, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := reserved.Addr().String()
	if err := reserved.Close(); err != nil {
		t.Fatal(err)
	}

	type startedServer struct {
		server *http.Server
		err    error
	}
	started := make(chan startedServer, 1)
	var calls atomic.Int32
	go func() {
		time.Sleep(70 * time.Millisecond)
		listener, listenErr := net.Listen("tcp", address)
		if listenErr != nil {
			started <- startedServer{err: listenErr}
			return
		}
		server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if calls.Add(1) <= 2 {
				http.Error(w, "No master known", http.StatusServiceUnavailable)
				return
			}
			writeRegisterPeerConfig(t, w, id)
		})}
		started <- startedServer{server: server}
		_ = server.Serve(listener)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	got, registerErr := registerPeer(ctx, zerolog.Nop(), "http://"+address, HelloRequest{SlaveID: id}, 40*time.Millisecond, 10*time.Millisecond)
	running := <-started
	if running.server != nil {
		_ = running.server.Shutdown(context.Background())
	}
	if running.err != nil {
		t.Fatalf("failed to start delayed server: %v", running.err)
	}
	if registerErr != nil {
		t.Fatalf("connection errors consumed the 503 timeout budget: %v", registerErr)
	}
	if _, found := got.PeerByID(id); !found {
		t.Fatalf("registered config does not contain %q", id)
	}
}
