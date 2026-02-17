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
	"reflect"
	"time"
)

const (
	keyData = "data"
	keyTTL  = "ttl"
)

// LeaderElectionCell maintains leadership for a given key
type LeaderElectionCell[T comparable] struct {
	lastTTL int64
	leading bool
	key     []string
	ttl     time.Duration
}

type leaderStruct[T comparable] struct {
	Data T     `json:"data,omitempty"`
	TTL  int64 `json:"ttl,omitempty"`
}

func NewLeaderElectionCell[T comparable](key []string, ttl time.Duration) *LeaderElectionCell[T] {
	return &LeaderElectionCell[T]{
		lastTTL: 0,
		leading: false,
		key:     key,
		ttl:     ttl,
	}
}

// GetLeaderCondition returns condition that ensures the value equals current
func (l *LeaderElectionCell[T]) GetLeaderCondition(dataValue T) WriteCondition {
	return IfEqualTo(append(l.key, keyData), dataValue)
}

func (l *LeaderElectionCell[T]) tryBecomeLeader(ctx context.Context, cli Agency, value T, assumeEmpty bool) error {
	newTTL := time.Now().Add(l.ttl).Unix()
	trx := &Transaction{
		Ops: []KeyChanger{
			SetKey(l.key, leaderStruct[T]{Data: value, TTL: newTTL}),
		},
	}

	if assumeEmpty {
		trx.Conds = []WriteCondition{
			KeyMissing(l.key),
		}
	} else {
		trx.Conds = []WriteCondition{
			KeyEquals(l.key, keyTTL, l.lastTTL),
		}
	}

	if err := cli.Write(ctx, trx); err != nil {
		return err
	}

	l.lastTTL = newTTL
	l.leading = true
	return nil
}

func (l *LeaderElectionCell[T]) readCell(ctx context.Context, cli Agency) (leaderStruct[T], error) {
	var result leaderStruct[T]
	if err := cli.ReadKey(ctx, l.key, &result); err != nil {
		return result, err
	}
	return result, nil
}

func (l *LeaderElectionCell[T]) Read(ctx context.Context, cli Agency) (T, error) {
	result, err := l.readCell(ctx, cli)
	if err != nil {
		var def T
		if IsKeyNotFound(err) {
			return def, nil
		}
		return def, err
	}
	return result.Data, nil
}

// Update checks leader status and tries to become leader
func (l *LeaderElectionCell[T]) Update(ctx context.Context, cli Agency, value T) (T, bool, time.Duration, error) {
	const minUpdateDelay = 500 * time.Millisecond

	var result leaderStruct[T]
	var err error
	var zeroValue T

	for {
		assumeEmpty := false
		result, err = l.readCell(ctx, cli)
		if err != nil {
			if IsKeyNotFound(err) {
				assumeEmpty = true
				result = leaderStruct[T]{} // default empty
			} else {
				return zeroValue, false, 0, err
			}
		}

		now := time.Now()
		needTryLeader := result.TTL < now.Unix() || (result.TTL == l.lastTTL && l.leading)

		if result.TTL > now.Unix() && !l.leading && l.lastTTL == 0 {
			l.lastTTL = result.TTL
			l.leading = reflect.DeepEqual(result.Data, value)
		}

		if needTryLeader {
			if err := l.tryBecomeLeader(ctx, cli, value, assumeEmpty); err == nil {
				return value, true, l.ttl / 2, nil
			} else if ctx.Err() != nil {
				return zeroValue, false, 0, err
			} else {
				time.Sleep(minUpdateDelay)
				continue
			}
		}

		l.lastTTL = result.TTL
		l.leading = false
		delay := time.Unix(l.lastTTL, 0).Sub(now)
		if delay < minUpdateDelay {
			delay = minUpdateDelay
		}

		return result.Data, false, delay, nil
	}
}

// Resign leadership
func (l *LeaderElectionCell[T]) Resign(ctx context.Context, cli Agency) error {
	if !l.leading {
		return nil
	}
	l.leading = false
	trx := &Transaction{
		Ops: []KeyChanger{RemoveKey(l.key)},
		Conds: []WriteCondition{
			KeyEquals(l.key, keyTTL, l.lastTTL),
		},
	}
	return cli.Write(ctx, trx)
}
