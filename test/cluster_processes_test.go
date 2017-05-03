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

// +build localprocess

package test

import (
	"fmt"
	"os"
	"regexp"
	"testing"
	"time"

	"golang.org/x/sync/errgroup"
)

// TestClusterProcesses starts a master starter, followed by 2 slave starters.
func TestClusterProcesses(t *testing.T) {
	dataDirMaster := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDirMaster)
	master, err := Spawn("${STARTER}")
	if err != nil {
		t.Fatalf("Failed to launch master: %s", describe(err))
	}
	defer master.Close()

	dataDirSlave1 := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDirSlave1)
	slave1, err := Spawn("${STARTER} --join 127.0.0.1")
	if err != nil {
		t.Fatalf("Failed to launch slave 1: %s", describe(err))
	}
	defer slave1.Close()

	dataDirSlave2 := SetUniqueDataDir(t)
	defer os.RemoveAll(dataDirSlave2)
	slave2, err := Spawn("${STARTER} --join 127.0.0.1")
	if err != nil {
		t.Fatalf("Failed to launch slave 2: %s", describe(err))
	}
	defer slave2.Close()

	fmt.Println("Waiting for cluster ready")

	if _, err := master.ExpectTimeout(time.Minute, regexp.MustCompile("Your cluster can now be accessed with a browser at")); err != nil {
		t.Error(describe(err))
	}

	if _, err := slave1.ExpectTimeout(time.Second*10, regexp.MustCompile("Your cluster can now be accessed with a browser at")); err != nil {
		t.Error(describe(err))
	}
	if _, err := slave2.ExpectTimeout(time.Second*10, regexp.MustCompile("Your cluster can now be accessed with a browser at")); err != nil {
		t.Error(describe(err))
	}

	g := errgroup.Group{}
	g.Go(func() error { return master.WaitTimeout(time.Second * 20) })
	g.Go(func() error { return slave1.WaitTimeout(time.Second * 20) })
	g.Go(func() error { return slave2.WaitTimeout(time.Second * 20) })

	master.Send(ctrlC)
	slave1.Send(ctrlC)
	slave2.Send(ctrlC)

	fmt.Println("Waiting for termination")

	g.Wait()
}
