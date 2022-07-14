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
// Author Adam Janikowski
//

//go:build !windows
// +build !windows

package service

import (
	"syscall"
)

func (p *process) Terminate() error {
	if proc := p.p; proc != nil {
		if err := proc.Signal(syscall.SIGTERM); err != nil {
			if err.Error() == "os: process already finished" {
				// Race condition on OSX
				return nil
			}
			return maskAny(err)
		}
	}
	return nil
}

func getSysProcAttr() *syscall.SysProcAttr {
	return nil
}
