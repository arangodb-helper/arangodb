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

type WriteCondition interface {
	GetName() string
	GetValue() any
	GetKey() []string // Get the full key path for the condition
}

type writeCondition struct {
	Key   []string
	Value any
}

func (w writeCondition) GetName() string {
	if len(w.Key) == 0 {
		return ""
	}
	return w.Key[0]
}
func (w writeCondition) GetValue() any    { return w.Value }
func (w writeCondition) GetKey() []string { return w.Key }

func IfEqualTo(key []string, value any) WriteCondition {
	return writeCondition{Key: key, Value: value}
}

// KeyEquals checks a field equals a value
func KeyEquals(key []string, field string, value any) WriteCondition {
	fullKey := make([]string, 0, len(key)+1)
	fullKey = append(fullKey, key...)
	fullKey = append(fullKey, field)

	return writeCondition{
		Key:   fullKey,
		Value: value,
	}
}

// KeyMissing is a precondition that the key path does not exist (empty/absent).
// The agency treats "old": null as matching when the key is missing.
func KeyMissing(key []string) WriteCondition {
	return writeCondition{
		Key:   key,
		Value: nil,
	}
}

// KeyMissingEmpty is like KeyMissing but sends "old": [] for agencies that expect
// an empty array for absent keys instead of null.
func KeyMissingEmpty(key []string) WriteCondition {
	return writeCondition{
		Key:   key,
		Value: []any{},
	}
}
