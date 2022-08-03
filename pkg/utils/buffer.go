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

import "sync"

// LimitedBuffer stores only last 'limit' bytes written to it
type LimitedBuffer struct {
	buf        []byte
	limit      int
	writeMutex sync.Mutex
}

// NewLimitedBuffer returns new LimitedBuffer
func NewLimitedBuffer(limit int) *LimitedBuffer {
	return &LimitedBuffer{
		buf:   make([]byte, 0, limit),
		limit: limit,
	}
}

// String returns string representation of buffer.
func (b *LimitedBuffer) String() string {
	return string(b.buf)
}

// Write writes len(p) bytes from p to the buffer,
// pushing out previously written bytes if they don't fit into buffer.
// Always returns len(p).
func (b *LimitedBuffer) Write(p []byte) (n int, err error) {
	b.writeMutex.Lock()
	defer b.writeMutex.Unlock()

	gotLen := len(p)
	if gotLen >= b.limit {
		b.buf = p[gotLen-b.limit:]
	} else if gotLen > 0 {
		newLength := len(b.buf) + gotLen
		if newLength <= b.limit {
			b.buf = append(b.buf, p...)
		} else {
			truncateIndex := newLength - b.limit
			b.buf = append(b.buf[truncateIndex:], p...)
		}
	}
	return gotLen, nil
}
