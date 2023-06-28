//
// DISCLAIMER
//
// Copyright 2018-2023 ArangoDB GmbH, Cologne, Germany
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

package election

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/arangodb/go-driver"
	"github.com/arangodb/go-driver/agency"
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

// Logger abstracts a logger.
type Logger interface {
	Errorf(msg string, args ...interface{})
}

// NewLock creates a new lock on the given key.
func NewLock(log Logger, api agency.Agency, key []string, id string, ttl time.Duration) (Lock, error) {
	if ttl < minLockTTL {
		ttl = minLockTTL
	}
	if id == "" {
		randBytes := make([]byte, 16)
		_, err := rand.Read(randBytes)
		if err != nil {
			return nil, err
		}
		id = hex.EncodeToString(randBytes)
	}
	return &lock{
		log:  log,
		id:   id,
		cell: NewLeaderElectionCell[string](api, key, ttl),
	}, nil
}

type lock struct {
	mutex sync.Mutex
	log   Logger

	cell *LeaderElectionCell[string]

	id            string
	cancelRenewal func()
}

// Lock tries to lock the lock.
// If it is not possible to lock, an error is returned.
// If the lock is already held by me, an error is returned.
func (l *lock) Lock(ctx context.Context) error {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	if l.cell.leading {
		return driver.WithStack(AlreadyLockedError)
	}

	_, isLocked, nextUpdateIn, err := l.cell.Update(ctx, l.id)
	if err != nil {
		return err
	}

	if !isLocked {
		// locked by someone
		return driver.WithStack(AlreadyLockedError)
	}

	// Keep renewing
	renewCtx, renewCancel := context.WithCancel(context.Background())
	go l.renewLock(renewCtx, nextUpdateIn)
	l.cancelRenewal = renewCancel

	return nil
}

// Unlock tries to unlock the lock.
// If it is not possible to unlock, an error is returned.
// If the lock is not held by me, an error is returned.
func (l *lock) Unlock(ctx context.Context) error {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	if !l.cell.leading {
		return driver.WithStack(NotLockedError)
	}

	err := l.cell.Resign(ctx)
	if err != nil {
		return err
	}

	defer func() {
		if l.cancelRenewal != nil {
			l.cancelRenewal()
			l.cancelRenewal = nil
		}
	}()

	return nil
}

// IsLocked return true if the lock is held by me.
func (l *lock) IsLocked() bool {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	return l.cell.leading
}

// renewLock keeps renewing the lock until the given context is canceled.
func (l *lock) renewLock(ctx context.Context, delay time.Duration) {
	op := func() (bool, time.Duration, error) {
		l.mutex.Lock()
		defer l.mutex.Unlock()

		if !l.cell.leading {
			return true, 0, nil
		}

		_, stillLeading, newDelay, err := l.cell.Update(ctx, l.id)
		return stillLeading, newDelay, err
	}
	for {
		var leading bool
		var err error
		leading, delay, err = op()
		if err != nil {
			if l.log != nil && driver.Cause(err) != context.Canceled {
				l.log.Errorf("Failed to renew lock %s. %v", l.cell.key, err)
			}
			delay = time.Second
		}
		if !leading || driver.Cause(err) == context.Canceled {
			return
		}

		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
			// Delay over, just continue
		case <-ctx.Done():
			// We're asked to stop
			if !timer.Stop() {
				<-timer.C
			}
			return
		}
	}
}
