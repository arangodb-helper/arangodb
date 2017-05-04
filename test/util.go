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
	"regexp"
	"sync"
	"testing"
	"time"

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

// WaitUntilStarterReady waits until all given starter processes have reached the "Your cluster is ready state"
func WaitUntilStarterReady(t *testing.T, starters ...*gexpect.SubProcess) bool {
	g := sync.WaitGroup{}
	result := true
	for _, starter := range starters {
		starter := starter // Used in nested function
		g.Add(1)
		go func() {
			defer g.Done()
			if _, err := starter.ExpectTimeout(time.Minute, regexp.MustCompile("Your cluster can now be accessed with a browser at")); err != nil {
				result = false
				t.Errorf("Starter is not ready in time: %s", describe(err))
			}
		}()
	}
	g.Wait()
	return result
}

// StopStarter stops all all given starter processes by sending a Ctrl-C into it.
// It then waits until the process has terminated.
func StopStarter(t *testing.T, starters ...*gexpect.SubProcess) bool {
	g := sync.WaitGroup{}
	result := true
	for _, starter := range starters {
		starter := starter // Used in nested function
		g.Add(1)
		go func() {
			defer g.Done()
			if err := starter.WaitTimeout(time.Second * 30); err != nil {
				result = false
				t.Errorf("Starter is not stopped in time: %s", describe(err))
			}
		}()
	}
	time.Sleep(time.Second)
	for _, starter := range starters {
		starter.Term.SendIntr()
		//starter.Send(ctrlC)
	}
	g.Wait()
	return result
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
