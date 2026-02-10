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
	"encoding/json"
	"errors"
	"log"
	"time"
)

// Lock represents a distributed agency-backed lock
type Lock interface {
	Acquire(ctx context.Context) error
	Renew(ctx context.Context) error
	Release(ctx context.Context) error
	IsLeader() bool
}

type lock struct {
	agency   Agency
	key      []string
	id       string
	ttl      time.Duration
	isLeader bool
}

// lockValue is stored in the agency
type lockValue struct {
	Holder  string    `json:"holder"`
	Expires time.Time `json:"expires"`
}

// NewLock creates a new agency-backed lock
func NewLock(
	agency Agency,
	key []string,
	id string,
	ttl time.Duration,
) (Lock, error) {
	if agency == nil {
		return nil, errors.New("agency client is nil")
	}
	if len(key) == 0 {
		return nil, errors.New("lock key is empty")
	}
	if ttl <= 0 {
		return nil, errors.New("invalid ttl")
	}

	return &lock{
		agency: agency,
		key:    key,
		id:     id,
		ttl:    ttl,
	}, nil
}

// Acquire tries to acquire leadership
func (l *lock) Acquire(ctx context.Context) error {
	now := time.Now()
	expires := now.Add(l.ttl)

	log.Printf("[LOCK] Acquire key=%v holder=%s", l.key, l.id)

	// Try to read current lock state for optimistic check
	var raw json.RawMessage
	err := l.agency.ReadKey(ctx, l.key, &raw)

	if err == nil {
		var current lockValue
		if err := json.Unmarshal(raw, &current); err == nil {
			// If lock is held by someone else and not expired, fail early
			if current.Expires.After(now) && current.Holder != l.id {
				return errors.New("lock already held")
			}
			// If lock is expired or we're the holder, proceed to atomic write
		} else {
			// old-format lock → MUST remove it first
			_ = l.agency.RemoveKey(ctx, l.key)
		}
	} else if !IsKeyNotFound(err) {
		return err
	}

	// Atomic write: try to acquire lock
	// Note: We've already checked if lock is held and not expired above
	// If we get here, either lock doesn't exist, is expired, or we're the holder
	tx := &Transaction{
		Ops: []KeyChanger{
			RemoveKey(l.key),
			SetKey(l.key, lockValue{Holder: l.id, Expires: expires}),
		},
	}

	if err := l.agency.Write(ctx, tx); err != nil {
		log.Printf("[LOCK] Acquire FAILED: %v", err)
		return err
	}

	log.Printf("[LOCK] Acquire SUCCESS")
	l.isLeader = true
	return nil
}

// Release releases the lock if we are leader
func (l *lock) Release(ctx context.Context) error {
	if !l.isLeader {
		return nil
	}

	tx := &Transaction{
		Ops: []KeyChanger{
			RemoveKey(l.key),
		},
		Conds: []WriteCondition{
			KeyEquals(l.key, "holder", l.id),
		},
	}

	_ = l.agency.Write(ctx, tx)
	l.isLeader = false
	return nil
}

// Renew extends the lock TTL if we are leader
func (l *lock) Renew(ctx context.Context) error {
	if !l.isLeader {
		return errors.New("not leader")
	}

	expires := time.Now().Add(l.ttl)

	tx := &Transaction{
		Ops: []KeyChanger{
			SetKey(
				l.key,
				lockValue{
					Holder:  l.id,
					Expires: expires,
				},
			),
		},
		Conds: []WriteCondition{
			KeyEquals(l.key, "holder", l.id),
		},
	}

	return l.agency.Write(ctx, tx)
}

// IsLeader returns true if this instance currently holds the lock
func (l *lock) IsLeader() bool {
	return l.isLeader
}
