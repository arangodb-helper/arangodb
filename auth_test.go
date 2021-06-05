//
// DISCLAIMER
//
// Copyright 2021 ArangoDB GmbH, Cologne, Germany
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
// Author Adam Janikowski
//

package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func parseDuration(t *testing.T, in, out string) {
	t.Run(in, func(t *testing.T) {
		v, err := durationParser(in, "s")
		require.NoError(t, err)

		s := v.String()

		require.Equal(t, out, s)
	})
}

func Test_Auth_DurationTest(t *testing.T) {
	parseDuration(t, "5", "5s")
	parseDuration(t, "5s", "5s")
	parseDuration(t, "5m", "5m0s")
	parseDuration(t, "5h", "5h0m0s")
}
