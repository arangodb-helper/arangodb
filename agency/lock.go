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
	"time"
)

// Lock represents a distributed agency-backed lock.
// Same pattern as master: lock is created with key/id/ttl; agency is passed at acquire time (Lock(ctx, api)).
type Lock interface {
	Acquire(ctx context.Context, api Agency) error
	Renew(ctx context.Context) error
	Release(ctx context.Context) error
	IsLeader() bool
}

type lock struct {
	key      []string
	id       string
	ttl      time.Duration
	agency   Agency // set on successful Acquire for Release/Renew
	isLeader bool
}

// lockValue is stored in the agency
type lockValue struct {
	Holder  string    `json:"holder"`
	Expires time.Time `json:"expires"`
}

// NewLock creates a new agency-backed lock (key, id, ttl only; agency passed at Acquire(ctx, api) like master Lock(ctx, api)).
func NewLock(key []string, id string, ttl time.Duration) (Lock, error) {
	if len(key) == 0 {
		return nil, errors.New("lock key is empty")
	}
	if ttl <= 0 {
		return nil, errors.New("invalid ttl")
	}
	return &lock{key: key, id: id, ttl: ttl}, nil
}

// Acquire tries to acquire leadership using agency write preconditions so that
// only one contender can succeed (atomic compare-and-swap style).
// api is the agency to use (same as master's lock.Lock(ctx, api)).
func (l *lock) Acquire(ctx context.Context, api Agency) error {
	if api == nil {
		return errors.New("agency is nil")
	}
	// Read current lock for fast-fail and for expired-case CAS
	var raw json.RawMessage
	err := api.ReadKey(ctx, l.key, &raw)
	var current lockValue
	var currentValid bool
	var currentRaw any
	var currentRawValid bool
	now := time.Now()
	if err == nil {
		if jsonErr := json.Unmarshal(raw, &currentRaw); jsonErr == nil {
			currentRawValid = true
		}
		if jsonErr := json.Unmarshal(raw, &current); jsonErr == nil {
			currentValid = true
			// If holder is empty and expires is zero, this may be a non-lock payload
			// (or bootstrap sentinel). Treat as unknown so we can use whole-value CAS.
			if current.Holder == "" && current.Expires.IsZero() {
				currentValid = false
			}
		} else {
			return errors.New("lock value is invalid")
		}
	} else if !IsKeyNotFound(err) {
		return err
	}

	now = time.Now()
	expires := now.Add(l.ttl)
	newVal := lockValue{Holder: l.id, Expires: expires}

	if currentValid && current.Expires.After(now) && current.Holder != l.id {
		return ErrLockAlreadyHeld
	}

	// 1) We already hold: only we can pass holder == l.id
	tx := &Transaction{
		Ops: []KeyChanger{
			SetKey(l.key, newVal),
		},
		Conds: []WriteCondition{
			KeyEquals(l.key, "holder", l.id),
		},
	}
	if err := api.Write(ctx, tx); err == nil {
		l.agency = api
		l.isLeader = true
		return nil
	}
	if err != nil && !IsPreconditionFailed(err) && !IsKeyNotFound(err) {
		return err
	}

	// 1b) Unknown/legacy payload at this key: CAS on whole key value.
	// Use raw JSON from read when available so precondition matches agency's exact encoding.
	if currentRawValid && !currentValid {
		condVal := any(currentRaw)
		if len(raw) > 0 {
			condVal = json.RawMessage(raw)
		}
		tx = &Transaction{
			Ops: []KeyChanger{
				SetKey(l.key, newVal),
			},
			Conds: []WriteCondition{
				IfEqualTo(l.key, condVal),
			},
		}
		if err := api.Write(ctx, tx); err == nil {
			l.agency = api
			l.isLeader = true
			return nil
		}
		if err != nil && !IsPreconditionFailed(err) && !IsKeyNotFound(err) {
			return err
		}
	}

	// 2) Key missing bootstrap: initialize with an empty sentinel using a missing-key
	// precondition so this step remains atomic. Try both null and empty-array forms
	// because agency variants can differ on absent-key encoding.
	if !currentValid {
		sentinel := lockValue{Holder: "", Expires: time.Time{}}

		bootstrapTx := &Transaction{
			Ops: []KeyChanger{SetKey(l.key, sentinel)},
			Conds: []WriteCondition{
				KeyMissing(l.key),
			},
		}
		if err := api.Write(ctx, bootstrapTx); err == nil {
			currentValid = true
			current = sentinel
			currentRaw = sentinel
			currentRawValid = true
		} else if !IsPreconditionFailed(err) && !IsKeyNotFound(err) {
			return err
		} else {
			bootstrapTx = &Transaction{
				Ops: []KeyChanger{SetKey(l.key, sentinel)},
				Conds: []WriteCondition{
					KeyMissingEmpty(l.key),
				},
			}
			if err := api.Write(ctx, bootstrapTx); err == nil {
				currentValid = true
				current = sentinel
				currentRaw = sentinel
				currentRawValid = true
			} else if !IsPreconditionFailed(err) && !IsKeyNotFound(err) {
				return err
			} else {
				if readErr := api.ReadKey(ctx, l.key, &raw); readErr == nil {
					currentRawValid = false
					if jsonErr := json.Unmarshal(raw, &currentRaw); jsonErr == nil {
						currentRawValid = true
					}
					if jsonErr := json.Unmarshal(raw, &current); jsonErr == nil {
						currentValid = true
						if current.Holder == "" && current.Expires.IsZero() {
							currentValid = false
						}
					}
				} else if !IsKeyNotFound(readErr) {
					return readErr
				}
			}
		}
	}

	// 2b) Key exists with sentinel (re-read after bootstrap failed): CAS using exact JSON from agency.
	if !currentValid && currentRawValid {
		condVal := any(currentRaw)
		if len(raw) > 0 {
			condVal = json.RawMessage(raw)
		}
		tx = &Transaction{
			Ops: []KeyChanger{
				SetKey(l.key, newVal),
			},
			Conds: []WriteCondition{
				IfEqualTo(l.key, condVal),
			},
		}
		if err := api.Write(ctx, tx); err == nil {
			l.agency = api
			l.isLeader = true
			return nil
		}
		if err != nil && !IsPreconditionFailed(err) && !IsKeyNotFound(err) {
			return err
		}
	}

	now = time.Now()
	if currentValid && current.Expires.After(now) && current.Holder != l.id {
		return ErrLockAlreadyHeld
	}

	// 3) Key exists but expired: CAS on expires so only one wins
	if currentValid && !current.Expires.After(now) {
		tx = &Transaction{
			Ops: []KeyChanger{
				SetKey(l.key, newVal),
			},
			Conds: []WriteCondition{
				KeyEquals(l.key, "expires", current.Expires),
			},
		}
		if err := api.Write(ctx, tx); err == nil {
			l.agency = api
			l.isLeader = true
			return nil
		}
		if err != nil && !IsPreconditionFailed(err) && !IsKeyNotFound(err) {
			return err
		}

		// Fallback for expired lock payloads where field-level CAS may not match
		// due JSON shape differences; use exact raw JSON when available.
		if currentRawValid {
			condVal := any(currentRaw)
			if len(raw) > 0 {
				condVal = json.RawMessage(raw)
			}
			tx = &Transaction{
				Ops: []KeyChanger{
					SetKey(l.key, newVal),
				},
				Conds: []WriteCondition{
					IfEqualTo(l.key, condVal),
				},
			}
			if err := api.Write(ctx, tx); err == nil {
				l.agency = api
				l.isLeader = true
				return nil
			}
			if err != nil && !IsPreconditionFailed(err) && !IsKeyNotFound(err) {
				return err
			}
		}
	}

	return ErrLockAlreadyHeld
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

	if err := l.agency.Write(ctx, tx); err != nil {
		return err
	}
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
