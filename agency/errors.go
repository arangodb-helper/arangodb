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
	"errors"
	"strings"
)

// ErrKeyNotFound is returned when a requested key does not exist in the agency
var ErrKeyNotFound = errors.New("key not found")

// ErrLockAlreadyHeld indicates lock acquisition failed because another holder owns a valid lock.
var ErrLockAlreadyHeld = errors.New("lock already held")

// ErrRedirectNotFollowed indicates a request hit an agency redirect response, but redirect handling is disabled.
var ErrRedirectNotFollowed = errors.New("redirect not followed")

func IsKeyNotFound(err error) bool {
	return err == ErrKeyNotFound || (err != nil && strings.Contains(err.Error(), "key not found"))
}

// IsLockAlreadyHeld returns true if the error indicates lock contention.
func IsLockAlreadyHeld(err error) bool {
	return errors.Is(err, ErrLockAlreadyHeld) || (err != nil && strings.Contains(err.Error(), "lock already held"))
}

// IsRedirectNotFollowed returns true if the error indicates agency redirect handling is required.
func IsRedirectNotFollowed(err error) bool {
	return errors.Is(err, ErrRedirectNotFollowed) || (err != nil && strings.Contains(err.Error(), "redirect not followed"))
}

// PreconditionFailedError is returned when an agency write precondition check fails.
type PreconditionFailedError struct {
	message string
}

func (e *PreconditionFailedError) Error() string {
	return e.message
}

// IsPreconditionFailed returns true if the given error indicates an agency precondition failure.
func IsPreconditionFailed(err error) bool {
	var pfe *PreconditionFailedError
	if errors.As(err, &pfe) {
		return true
	}
	return err != nil && strings.Contains(err.Error(), "precondition failed")
}
