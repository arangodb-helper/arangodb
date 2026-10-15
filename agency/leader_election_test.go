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
	"reflect"
	"strings"
	"testing"
	"time"
)

type electionTestAgency struct {
	read  func(context.Context, []string, any) error
	write func(context.Context, *Transaction) error
}

func (a *electionTestAgency) Read(ctx context.Context, key []string, out any) error {
	return a.read(ctx, key, out)
}

func (a *electionTestAgency) ReadKey(ctx context.Context, key []string, out any) error {
	return a.read(ctx, key, out)
}

func (a *electionTestAgency) Write(ctx context.Context, tx *Transaction) error {
	return a.write(ctx, tx)
}

func (a *electionTestAgency) WriteKey(ctx context.Context, key []string, value any, ttl time.Duration, conditions ...WriteCondition) error {
	return a.write(ctx, &Transaction{Ops: []KeyChanger{SetKey(key, value)}, Conds: conditions})
}

func (a *electionTestAgency) RemoveKey(ctx context.Context, key []string, conditions ...WriteCondition) error {
	return a.write(ctx, &Transaction{Ops: []KeyChanger{RemoveKey(key)}, Conds: conditions})
}

func (a *electionTestAgency) RemoveKeyIfEqualTo(ctx context.Context, key []string, value interface{}) error {
	return a.RemoveKey(ctx, key, IfEqualTo(key, value))
}

func setElectionResult(t *testing.T, out any, value leaderStruct[string]) {
	t.Helper()
	target, ok := out.(*leaderStruct[string])
	if !ok {
		t.Fatalf("unexpected read target %T", out)
	}
	*target = value
}

func TestLeaderElectionReadsExistingLeader(t *testing.T) {
	const currentLeader = "http://starter2:9628/"
	api := &electionTestAgency{
		read: func(_ context.Context, _ []string, out any) error {
			setElectionResult(t, out, leaderStruct[string]{
				Data: currentLeader,
				TTL:  time.Now().Add(time.Minute).Unix(),
			})
			return nil
		},
		write: func(context.Context, *Transaction) error {
			t.Fatal("unexpected agency write")
			return nil
		},
	}

	cell := NewLeaderElectionCell[string]([]string{"starter", "leader"}, 30*time.Second)
	got, leading, _, err := cell.Update(context.Background(), api, "http://starter1:9528/")
	if err != nil {
		t.Fatalf("leader election failed: %v", err)
	}
	if leading || got != currentLeader {
		t.Fatalf("expected follower of %q, got leader=%q leading=%v", currentLeader, got, leading)
	}
}

func TestLeaderElectionReplacesExpiredLeader(t *testing.T) {
	const ownURL = "http://starter1:9528/"
	expiredTTL := time.Now().Add(-time.Minute).Unix()
	writes := 0
	api := &electionTestAgency{
		read: func(_ context.Context, _ []string, out any) error {
			setElectionResult(t, out, leaderStruct[string]{
				Data: "http://old:8528/",
				TTL:  expiredTTL,
			})
			return nil
		},
		write: func(_ context.Context, tx *Transaction) error {
			writes++
			if len(tx.Ops) != 1 || len(tx.Conds) != 1 {
				t.Fatalf("unexpected leader transaction: %+v", tx)
			}
			cond := tx.Conds[0]
			if !reflect.DeepEqual(cond.GetKey(), []string{"starter", "leader", keyTTL}) ||
				cond.GetName() != "old" || cond.GetValue() != expiredTTL {
				t.Fatalf("expected observed TTL precondition, got key=%v name=%q value=%v",
					cond.GetKey(), cond.GetName(), cond.GetValue())
			}
			return nil
		},
	}

	cell := NewLeaderElectionCell[string]([]string{"starter", "leader"}, 30*time.Second)
	got, leading, _, err := cell.Update(context.Background(), api, ownURL)
	if err != nil {
		t.Fatalf("leader election failed: %v", err)
	}
	if !leading || got != ownURL || writes != 1 {
		t.Fatalf("expected to replace expired leader, got leader=%q leading=%v writes=%d", got, leading, writes)
	}
}

func TestLeaderElectionCreatesMissingLeader(t *testing.T) {
	writes := 0
	api := &electionTestAgency{
		read: func(context.Context, []string, any) error {
			return ErrKeyNotFound
		},
		write: func(_ context.Context, tx *Transaction) error {
			writes++
			if len(tx.Conds) != 1 || tx.Conds[0].GetName() != "oldEmpty" {
				t.Fatalf("expected missing-key precondition, got %+v", tx.Conds)
			}
			return nil
		},
	}

	cell := NewLeaderElectionCell[string]([]string{"starter", "leader"}, 30*time.Second)
	_, leading, _, err := cell.Update(context.Background(), api, "http://starter1:9528/")
	if err != nil || !leading || writes != 1 {
		t.Fatalf("expected missing leader creation, leading=%v writes=%d err=%v", leading, writes, err)
	}
}

func TestLeaderElectionHandlesPreconditionRace(t *testing.T) {
	const winner = "http://starter2:9628/"
	reads := 0
	api := &electionTestAgency{
		read: func(_ context.Context, _ []string, out any) error {
			reads++
			if reads == 1 {
				return ErrKeyNotFound
			}
			setElectionResult(t, out, leaderStruct[string]{
				Data: winner,
				TTL:  time.Now().Add(time.Minute).Unix(),
			})
			return nil
		},
		write: func(context.Context, *Transaction) error {
			return &PreconditionFailedError{message: "precondition failed"}
		},
	}

	cell := NewLeaderElectionCell[string]([]string{"starter", "leader"}, 30*time.Second)
	got, leading, _, err := cell.Update(context.Background(), api, "http://starter1:9528/")
	if err != nil {
		t.Fatalf("leader election failed after race: %v", err)
	}
	if leading || got != winner || reads != 2 {
		t.Fatalf("expected winning peer %q, got leader=%q leading=%v reads=%d", winner, got, leading, reads)
	}
}

func TestLeaderElectionPreservesLastReadError(t *testing.T) {
	readErr := errors.New("agency read returned HTTP 401")
	api := &electionTestAgency{
		read: func(context.Context, []string, any) error {
			return readErr
		},
		write: func(context.Context, *Transaction) error {
			t.Fatal("unexpected agency write")
			return nil
		},
	}

	cell := NewLeaderElectionCell[string]([]string{"starter", "leader"}, 30*time.Second)
	_, _, _, err := cell.update(context.Background(), api, "http://starter1:9528/", 20*time.Millisecond)
	if err == nil || !errors.Is(err, readErr) || !strings.Contains(err.Error(), "HTTP 401") {
		t.Fatalf("expected preserved agency read error, got %v", err)
	}
}

func TestLeaderElectionPreservesReadErrorWhenNextReadHitsDeadline(t *testing.T) {
	readErr := errors.New("agency read returned HTTP 503")
	reads := 0
	api := &electionTestAgency{
		read: func(ctx context.Context, _ []string, _ any) error {
			reads++
			if reads == 1 {
				return readErr
			}
			<-ctx.Done()
			return ctx.Err()
		},
		write: func(context.Context, *Transaction) error {
			t.Fatal("unexpected agency write")
			return nil
		},
	}

	cell := NewLeaderElectionCell[string]([]string{"starter", "leader"}, 30*time.Second)
	_, _, _, err := cell.update(context.Background(), api, "http://starter1:9528/", 750*time.Millisecond)
	if err == nil || !errors.Is(err, readErr) {
		t.Fatalf("expected initial agency read error after later deadline, got %v", err)
	}
	if reads != 2 {
		t.Fatalf("expected deadline on second read, got %d reads", reads)
	}
}

func TestLeaderElectionPreservesReadErrorWhenCanceled(t *testing.T) {
	readErr := errors.New("agency read returned HTTP 503")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	api := &electionTestAgency{
		read: func(context.Context, []string, any) error {
			cancel()
			return readErr
		},
		write: func(context.Context, *Transaction) error {
			t.Fatal("unexpected agency write")
			return nil
		},
	}

	cell := NewLeaderElectionCell[string]([]string{"starter", "leader"}, 30*time.Second)
	_, _, _, err := cell.update(ctx, api, "http://starter1:9528/", time.Second)
	if err == nil || !errors.Is(err, readErr) || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected preserved agency read error after cancel, got %v", err)
	}
}

func TestLeaderElectionPreservesWriteErrorWhenNextWriteHitsDeadline(t *testing.T) {
	writeErr := errors.New("agency write returned HTTP 503")
	writes := 0
	api := &electionTestAgency{
		read: func(_ context.Context, _ []string, out any) error {
			setElectionResult(t, out, leaderStruct[string]{
				Data: "http://old:8528/",
				TTL:  time.Now().Add(-time.Minute).Unix(),
			})
			return nil
		},
		write: func(ctx context.Context, _ *Transaction) error {
			writes++
			if writes == 1 {
				return writeErr
			}
			<-ctx.Done()
			return ctx.Err()
		},
	}

	cell := NewLeaderElectionCell[string]([]string{"starter", "leader"}, 30*time.Second)
	_, _, _, err := cell.update(context.Background(), api, "http://starter1:9528/", 750*time.Millisecond)
	if err == nil || !errors.Is(err, writeErr) {
		t.Fatalf("expected initial agency write error after later deadline, got %v", err)
	}
	if writes != 2 {
		t.Fatalf("expected deadline on second write, got %d writes", writes)
	}
}
