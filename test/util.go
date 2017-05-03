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

package test

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"testing"

	shell "github.com/kballard/go-shellquote"
	"github.com/pkg/errors"
	"github.com/shavac/gexpect"
)

const (
	ctrlC = "\u0003"
)

// Spawn a command an return its process.
func Spawn(command string) (*gexpect.SubProcess, error) {
	args, err := shell.Split(os.ExpandEnv(command))
	if err != nil {
		return nil, errors.WithStack(err)
	}
	fmt.Println(args, len(args))
	p, err := gexpect.NewSubProcess(args[0], args[1:]...)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	if err := p.Start(); err != nil {
		p.Close()
		return nil, errors.WithStack(err)
	}
	return p, nil
}

// SetUniqueDataDir creates a temp dir and sets the DATA_DIR environment variable to it.
func SetUniqueDataDir(t *testing.T) string {
	dataDir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatal(describe(err))
	}
	os.Setenv("DATA_DIR", dataDir)
	return dataDir
}

// describe returns a string description of the given error.
func describe(err error) string {
	if err == nil {
		return "nil"
	}
	cause := errors.Cause(err)
	c, _ := json.Marshal(cause)
	cStr := fmt.Sprintf("%#v (%s)", cause, string(c))
	if cause.Error() != err.Error() {
		return fmt.Sprintf("%v caused by %v", err, cStr)
	} else {
		return cStr
	}
}
