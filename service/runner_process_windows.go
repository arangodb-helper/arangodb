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

//go:build windows
// +build windows

package service

import (
	"fmt"
	"syscall"

	"github.com/pkg/errors"
)

var (
	libkernel32              = syscall.MustLoadDLL("kernel32")
	generateConsoleCtrlEvent = libkernel32.MustFindProc("GenerateConsoleCtrlEvent")
)

func (p *process) Terminate() error {
	p.log.Info().Int("process", p.ProcessID()).Msgf("Sending windows CTRL_BREAK_EVENT event")
	r, _, err := generateConsoleCtrlEvent.Call(syscall.CTRL_BREAK_EVENT, uintptr(p.ProcessID()))

	if r == 0 {
		return errors.WithMessage(err, fmt.Sprintf("Exited with code %d", r))
	}

	p.log.Info().Int("process", p.ProcessID()).Msgf("Send windows CTRL_BREAK_EVENT event")

	return nil
}

func getSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}
