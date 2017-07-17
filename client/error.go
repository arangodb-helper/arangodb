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

package client

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/pkg/errors"
)

var (
	maskAny                 = errors.WithStack
	ServiceUnavailableError = StatusError{StatusCode: http.StatusServiceUnavailable, message: "service unavailable"}
	BadRequestError         = StatusError{StatusCode: http.StatusBadRequest, message: "bad request"}
	PreconditionFailedError = StatusError{StatusCode: http.StatusPreconditionFailed, message: "precondition failed"}
	InternalServerError     = StatusError{StatusCode: http.StatusInternalServerError, message: "internal server error"}
)

type StatusError struct {
	StatusCode int
	message    string
}

func (e StatusError) Error() string {
	if e.message != "" {
		return e.message
	}
	return fmt.Sprintf("Status %d", e.StatusCode)
}

// IsStatusError returns the status code and true
// if the given error is caused by a StatusError.
func IsStatusError(err error) (int, bool) {
	err = errors.Cause(err)
	if serr, ok := err.(StatusError); ok {
		return serr.StatusCode, true
	}
	return 0, false
}

// IsStatusErrorWithCode returns true if the given error is caused
// by a StatusError with given code.
func IsStatusErrorWithCode(err error, code int) bool {
	err = errors.Cause(err)
	if serr, ok := err.(StatusError); ok {
		return serr.StatusCode == code
	}
	return false
}

type ErrorResponse struct {
	Error string
}

func IsServiceUnavailable(err error) bool {
	return IsStatusErrorWithCode(err, http.StatusServiceUnavailable)
}

func IsBadRequest(err error) bool {
	return IsStatusErrorWithCode(err, http.StatusBadRequest)
}

func IsPreconditionFailed(err error) bool {
	return IsStatusErrorWithCode(err, http.StatusPreconditionFailed)
}

func InternalServer(err error) bool {
	return IsStatusErrorWithCode(err, http.StatusInternalServerError)
}

// ParseResponseError returns an error from given response.
// It tries to parse the body (if given body is nil, will be read from response)
// for ErrorResponse.
func ParseResponseError(r *http.Response, body []byte) error {
	// Read body (if needed)
	if body == nil {
		defer r.Body.Close()
		body, _ = ioutil.ReadAll(r.Body)
	}
	// Parse body (if available)
	if len(body) > 0 {
		var errRes ErrorResponse
		if err := json.Unmarshal(body, &errRes); err == nil {
			// Found ErrorResponse
			return StatusError{StatusCode: r.StatusCode, message: errRes.Error}
		}
	}

	// No ErrorResponse found, fallback to default message
	return StatusError{StatusCode: r.StatusCode}
}
