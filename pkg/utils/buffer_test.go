//
// DISCLAIMER
//
// Copyright 2017-2022 ArangoDB GmbH, Cologne, Germany
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

package utils

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCaptureWriter(t *testing.T) {
	t.Run("simple-test", func(t *testing.T) {
		cw := NewLimitedBuffer(100)

		_, _ = cw.Write([]byte(""))
		assert.Equal(t, "", cw.String())

		_, _ = cw.Write([]byte("0123456789"))
		assert.Equal(t, "0123456789", cw.String())

		_, _ = cw.Write([]byte(strings.Repeat("0123456789", 9)))
		assert.Equal(t, strings.Repeat("0123456789", 10), cw.String())
	})

	t.Run("full-copy", func(t *testing.T) {
		cw := NewLimitedBuffer(100)

		_, _ = cw.Write([]byte(strings.Repeat("0123456789", 10)))
		assert.Equal(t, strings.Repeat("0123456789", 10), cw.String())
	})

	t.Run("overflow", func(t *testing.T) {
		cw := NewLimitedBuffer(10)

		_, _ = cw.Write([]byte(strings.Repeat("0123456789", 2)))
		assert.Equal(t, strings.Repeat("0123456789", 1), cw.String())
	})

	t.Run("one-byte-overflow", func(t *testing.T) {
		cw := NewLimitedBuffer(10)

		_, _ = cw.Write([]byte("01234567890"))
		assert.Equal(t, "1234567890", cw.String())
	})

	t.Run("one-byte-overflow-1", func(t *testing.T) {
		cw := NewLimitedBuffer(10)

		_, _ = cw.Write([]byte(strings.Repeat("001234", 1)))
		_, _ = cw.Write([]byte(strings.Repeat("56789", 1)))
		assert.Equal(t, strings.Repeat("0123456789", 1), cw.String())
	})

	t.Run("one-byte-overflow-2", func(t *testing.T) {
		cw := NewLimitedBuffer(10)

		_, _ = cw.Write([]byte(strings.Repeat("00123", 1)))
		_, _ = cw.Write([]byte(strings.Repeat("456789", 1)))
		assert.Equal(t, strings.Repeat("0123456789", 1), cw.String())
	})

	t.Run("overflow-many", func(t *testing.T) {
		cw := NewLimitedBuffer(100)

		_, _ = cw.Write([]byte(strings.Repeat("0123456789", 20)))
		assert.Equal(t, strings.Repeat("0123456789", 10), cw.String())
	})
}
