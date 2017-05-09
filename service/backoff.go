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

package service

import (
	"time"

	"github.com/cenkalti/backoff"
	"github.com/pkg/errors"
)

type PermanentError struct {
	Err error
}

func (e *PermanentError) Error() string {
	return e.Err.Error()
}

func retry(op func() error, timeout time.Duration) error {
	var failure error
	wrappedOp := func() error {
		if err := op(); err == nil {
			return nil
		} else {
			if pe, ok := errors.Cause(err).(*PermanentError); ok {
				// Detected permanent error
				failure = pe.Err
				return nil
			} else {
				return err
			}
		}
	}
	b := backoff.NewExponentialBackOff()
	b.MaxElapsedTime = timeout
	b.MaxInterval = timeout / 3
	if err := backoff.Retry(wrappedOp, b); err != nil {
		return maskAny(err)
	}
	if failure != nil {
		return maskAny(failure)
	}
	return nil
}
