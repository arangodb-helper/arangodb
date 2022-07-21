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

// LimitedBuffer stores only last 'limit' bytes written to it
type LimitedBuffer struct {
	buf   []byte
	limit int
}

func NewLimitedBuffer(limit int) *LimitedBuffer {
	return &LimitedBuffer{
		buf:   make([]byte, 0, limit),
		limit: limit,
	}
}

func (b *LimitedBuffer) String() string {
	return string(b.buf)
}

func (b *LimitedBuffer) Write(p []byte) (n int, err error) {
	gotLen := len(p)
	if gotLen >= b.limit {
		b.buf = p[gotLen-b.limit:]
	} else if gotLen > 0 {
		newLength := len(b.buf) + gotLen
		if newLength <= b.limit {
			b.buf = append(b.buf, p...)
		} else {
			truncateIndex := newLength - b.limit - 1
			b.buf = append(b.buf[truncateIndex-1:], p...)
		}
	}
	return gotLen, nil
}
