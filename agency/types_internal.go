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
	"strings"
)

// KeyChanger represents a single agency key operation
type KeyChanger interface {
	GetKey() string
	GetOperation() string
	GetNew() any
	Apply(ops map[string]map[string]any) // <-- required for Write
}

// keyChange is the concrete implementation of KeyChanger
type keyChange struct {
	KeyField  []string
	Operation string
	NewValue  any
}

func (k keyChange) GetKey() string {
	return strings.Join(k.KeyField, "/")
}

func (k keyChange) GetOperation() string {
	return k.Operation
}

func (k keyChange) GetNew() any {
	return k.NewValue
}

// Apply adds the operation to the ops map for agency write
func (k keyChange) Apply(ops map[string]map[string]any) {
	if ops[k.Operation] == nil {
		ops[k.Operation] = map[string]any{}
	}

	if k.Operation == "set" {
		ops[k.Operation][strings.Join(k.KeyField, "/")] = k.NewValue
	} else if k.Operation == "delete" {
		ops[k.Operation][strings.Join(k.KeyField, "/")] = nil
	}
}

func SetKey(key []string, value any) KeyChanger {
	return keyChange{
		KeyField:  key,
		Operation: "set",
		NewValue:  value,
	}
}

func RemoveKey(key []string) KeyChanger {
	return keyChange{
		KeyField:  key,
		Operation: "delete",
	}
}
