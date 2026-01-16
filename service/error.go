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
	"fmt"

	"github.com/pkg/errors"

	driver_shared "github.com/arangodb/go-driver/v2/arangodb/shared"
)

var (
	maskAny = errors.WithStack
)

type RedirectError struct {
	Location string
}

func (e RedirectError) Error() string {
	return fmt.Sprintf("Redirect to %s", e.Location)
}

func IsRedirect(err error) (string, bool) {
	if rerr, ok := errors.Cause(err).(RedirectError); ok {
		return rerr.Location, true
	}
	return "", false
}

// IsArangoErrorWithCodeAndNum returns true when the given raw content
// contains a valid arangodb error encoded as json with the given
// code and errorNum values.
func IsArangoErrorWithCodeAndNum(aerr driver_shared.ArangoError, code, errorNum int) bool {
	return aerr.HasError && aerr.Code == code && aerr.ErrorNum == errorNum
}

// IsLeadershipChallengeOngoingError returns true when given response content
// contains an arango error indicating an ongoing leadership challenge.
func IsLeadershipChallengeOngoingError(d driver_shared.ArangoError) bool {
	return IsArangoErrorWithCodeAndNum(d, 503, 1495)
}

// IsNoLeaderError returns true when given response content
// contains an arango error indicating an "I'm not the leader" error.
func IsNoLeaderError(d driver_shared.ArangoError) bool {
	return IsArangoErrorWithCodeAndNum(d, 503, 1496)
}
