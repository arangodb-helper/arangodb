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

package logging

import (
	"io"
	"os"
	"sync"
)

type rotatingWriter struct {
	mutex sync.RWMutex
	path  string
	f     *os.File
}

// newRotatingWriter creates a new rotating writer.
func newRotatingWriter(path string) (*rotatingWriter, error) {
	r := &rotatingWriter{path: path}
	if err := r.Rotate(); err != nil {
		return nil, maskAny(err)
	}
	return r, nil
}

var (
	_ io.Writer = &rotatingWriter{}
)

// Close the writer
func (w *rotatingWriter) Close() error {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	if w.f != nil {
		if err := w.f.Close(); err != nil {
			return maskAny(err)
		}
		w.f = nil
	}
	return nil
}

// Write to the writer
func (w *rotatingWriter) Write(p []byte) (n int, err error) {
	w.mutex.RLock()
	defer w.mutex.RUnlock()

	if w.f != nil {
		return w.f.Write(p)
	}
	return 0, nil
}

// Rotate closes the writer and re-opens it.
func (w *rotatingWriter) Rotate() error {
	if err := w.Close(); err != nil {
		return maskAny(err)
	}

	// Claim lock
	w.mutex.Lock()
	defer w.mutex.Unlock()

	newF, err := os.OpenFile(w.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return maskAny(err)
	}
	w.f = newF
	return nil
}
