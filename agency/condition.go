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

// WriteCondition describes a precondition for an agency write operation.
type WriteCondition interface {
	// GetName returns the condition type name for the agency API (e.g. "old", "oldEmpty").
	GetName() string
	// GetValue returns the condition value.
	GetValue() any
	// GetKey returns the full key path for the condition.
	GetKey() []string
}

// keyWriteCondition is a WriteCondition backed by a specific condition name.
type keyWriteCondition struct {
	key   []string
	name  string
	value any
}

func (w *keyWriteCondition) GetName() string  { return w.name }
func (w *keyWriteCondition) GetValue() any    { return w.value }
func (w *keyWriteCondition) GetKey() []string { return w.key }

// IfEqualTo returns a precondition that the value at key equals value.
func IfEqualTo(key []string, value any) WriteCondition {
	return &keyWriteCondition{key: key, name: "old", value: value}
}

// KeyEquals returns a precondition that key+field equals value.
func KeyEquals(key []string, field string, value any) WriteCondition {
	fullKey := make([]string, 0, len(key)+1)
	fullKey = append(fullKey, key...)
	fullKey = append(fullKey, field)
	return &keyWriteCondition{key: fullKey, name: "old", value: value}
}

// KeyMissing returns a precondition that the key path does not exist.
// Uses the agency "oldEmpty" precondition type (matching go-driver v1 behaviour).
func KeyMissing(key []string) WriteCondition {
	return &keyWriteCondition{key: key, name: "oldEmpty", value: true}
}

// KeyMissingEmpty is an alias for KeyMissing (kept for backward compatibility).
func KeyMissingEmpty(key []string) WriteCondition {
	return KeyMissing(key)
}
