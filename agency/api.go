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
	"time"
)

// Starter-scoped API (NOT v1 compatible)
type Agency interface {
	Read(ctx context.Context, key []string, out any) error
	ReadKey(ctx context.Context, key []string, out any) error

	Write(ctx context.Context, tx *Transaction) error
	WriteKey(ctx context.Context, key []string, value any, ttl time.Duration, conditions ...WriteCondition) error
	RemoveKey(ctx context.Context, key []string, conditions ...WriteCondition) error
	RemoveKeyIfEqualTo(ctx context.Context, key []string, oldValue interface{}) error
}
