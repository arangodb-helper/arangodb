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

package arangod

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	logging "github.com/op/go-logging"
)

const (
	minLockTTL = time.Second * 5
)

// Lock is an agency backed exclusive lock.
type Lock interface {
	// Lock tries to lock the lock.
	// If it is not possible to lock, an error is returned.
	// If the lock is already held by me, an error is returned.
	Lock(ctx context.Context) error

	// Unlock tries to unlock the lock.
	// If it is not possible to unlock, an error is returned.
	// If the lock is not held by me, an error is returned.
	Unlock(ctx context.Context) error

	// IsLocked return true if the lock is held by me.
	IsLocked() bool
}

// NewLock creates a new lock on the given key.
func NewLock(log *logging.Logger, api AgencyAPI, key []string, id string, ttl time.Duration) (Lock, error) {
	if ttl < minLockTTL {
		ttl = minLockTTL
	}
	if id == "" {
		randBytes := make([]byte, 16)
		rand.Read(randBytes)
		id = hex.EncodeToString(randBytes)
	}
	return &lock{
		log: log,
		api: api,
		key: key,
		id:  id,
		ttl: ttl,
	}, nil
}

type lock struct {
	mutex         sync.Mutex
	log           *logging.Logger
	api           AgencyAPI
	key           []string
	id            string
	ttl           time.Duration
	locked        bool
	cancelRenewal func()
}

// Lock tries to lock the lock.
// If it is not possible to lock, an error is returned.
// If the lock is already held by me, an error is returned.
func (l *lock) Lock(ctx context.Context) error {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	if l.locked {
		return maskAny(AlreadyLockedError)
	}

	// Try to claim lock
	ctx, cancel := context.WithTimeout(ctx, time.Second*10)
	defer cancel()
	if err := l.api.WriteKeyIfEmpty(ctx, l.key, l.id, l.ttl); err != nil {
		if IsPreconditionFailed(err) {
			return maskAny(AlreadyLockedError)
		}
		return maskAny(err)
	}

	// Success
	l.locked = true

	// Keep renewing
	renewCtx, renewCancel := context.WithCancel(context.Background())
	go l.renewLock(renewCtx)
	l.cancelRenewal = renewCancel

	return nil
}

// Unlock tries to unlock the lock.
// If it is not possible to unlock, an error is returned.
// If the lock is not held by me, an error is returned.
func (l *lock) Unlock(ctx context.Context) error {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	if !l.locked {
		return maskAny(NotLockedError)
	}

	// Release the lock
	ctx, cancel := context.WithTimeout(ctx, time.Second*10)
	defer cancel()
	if err := l.api.RemoveKeyIfEqualTo(ctx, l.key, l.id); err != nil {
		return maskAny(err)
	}

	// Cleanup
	l.locked = false
	if l.cancelRenewal != nil {
		l.cancelRenewal()
		l.cancelRenewal = nil
	}

	return nil
}

// IsLocked return true if the lock is held by me.
func (l *lock) IsLocked() bool {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	return l.locked
}

// renewLock keeps renewing the lock until the given context is canceled.
func (l *lock) renewLock(ctx context.Context) {
	// op performs a renewal once.
	// returns stop, error
	op := func() (bool, error) {
		l.mutex.Lock()
		defer l.mutex.Unlock()

		if !l.locked {
			return true, maskAny(NotLockedError)
		}

		// Update key in agency
		ctx, cancel := context.WithTimeout(ctx, time.Second*10)
		defer cancel()
		if err := l.api.WriteKeyIfEqualTo(ctx, l.key, l.id, l.id, l.ttl); err != nil {
			if IsConditionFailed(err) {
				// We're not longer the leader
				l.locked = false
				l.cancelRenewal = nil
				return true, maskAny(err)
			}
			return false, maskAny(err)
		}
		return false, nil
	}
	for {
		delay := l.ttl / 2
		stop, err := op()
		if err != nil {
			l.log.Errorf("Failed to renew lock %s. %v", l.key, err)
			delay = time.Second
		}
		if stop {
			return
		}

		select {
		case <-ctx.Done():
			// we're done
			return
		case <-time.After(delay):
			// Try to renew
		}
	}
}
