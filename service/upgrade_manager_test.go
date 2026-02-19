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
package service

import (
	"testing"

	"github.com/arangodb/go-driver"
	"github.com/stretchr/testify/require"
)

func TestIsAllowedCrossMajorUpgrade(t *testing.T) {
	testCases := []struct {
		name     string
		from     driver.Version
		to       driver.Version
		expected bool
	}{
		{
			name:     "allows 3.12.7 to 4.0.0",
			from:     "3.12.7",
			to:       "4.0.0",
			expected: true,
		},
		{
			name:     "allows 3.12.12 to 4.0.3",
			from:     "3.12.12",
			to:       "4.0.3",
			expected: true,
		},
		{
			name:     "rejects 3.12.6 to 4.0.0",
			from:     "3.12.6",
			to:       "4.0.0",
			expected: false,
		},
		{
			name:     "rejects 3.11.x to 4.0.0",
			from:     "3.11.10",
			to:       "4.0.0",
			expected: false,
		},
		{
			name:     "rejects 3.12.7 to 5.0.0",
			from:     "3.12.7",
			to:       "5.0.0",
			expected: false,
		},
		{
			name:     "rejects same major path",
			from:     "3.12.8",
			to:       "3.13.0",
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual := IsAllowedCrossMajorUpgrade(tc.from, tc.to)
			require.Equal(t, tc.expected, actual)
		})
	}
}
