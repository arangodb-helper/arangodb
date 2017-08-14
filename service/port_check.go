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
	"net"
	"time"
)

// IsPortOpen checks if a TCP port is free to listen on.
func IsPortOpen(port int) bool {
	l, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return false
	}
	l.Close()
	return true
}

// WaitUntilPortAvailable waits until a TCP port is free to listen on
// or a timeout occurs.
// Returns true when port is free to listen on.
func WaitUntilPortAvailable(port int, timeout time.Duration) bool {
	start := time.Now()
	for {
		if IsPortOpen(port) {
			return true
		}
		if time.Since(start) >= timeout {
			return false
		}
		time.Sleep(time.Millisecond * 10)
	}
}
