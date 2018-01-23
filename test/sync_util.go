//
// DISCLAIMER
//
// Copyright 2018 ArangoDB GmbH, Cologne, Germany
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
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type syncCertificates struct {
	Dir string // Directory of all files
	TLS struct {
		CACertificate string // Path of TLS CA certificate file
		CAKey         string // Path of TLS CA key file
		DCA           struct {
			Keyfile string // Path of datacenter A TLS Keyfile
		}
		DCB struct {
			Keyfile string // Path of datacenter B TLS Keyfile
		}
	}
	ClientAuth struct {
		CACertificate string // Path of client authentication CA certificate file
		CAKey         string // Path of client authentication CA key file
	}
	ClusterSecret string
}

// TestDockerClusterSync runs 3 arangodb starters in docker with arangosync enabled.
func createSyncCertificates(t *testing.T, ip string) syncCertificates {
	image := os.Getenv("ARANGODB")
	if image == "" {
		t.Fatal("Need ARANGODB envvar with name of ArangoDB docker image")
	}

	dir, err := ioutil.TempDir("", "starter-test")
	if err != nil {
		t.Fatalf("TempDir failed: %s", err)
	}

	// Create certificates
	cmdLines := []string{
		"/usr/sbin/arangosync create tls ca --cert=/data/tls-ca.crt --key=/data/tls-ca.key",
		"/usr/sbin/arangosync create tls keyfile --cacert=/data/tls-ca.crt --cakey=/data/tls-ca.key --keyfile=/data/tls-a.keyfile --host=" + ip,
		"/usr/sbin/arangosync create tls keyfile --cacert=/data/tls-ca.crt --cakey=/data/tls-ca.key --keyfile=/data/tls-b.keyfile --host=" + ip,
		"/usr/sbin/arangosync create client-auth ca --cert=/data/client-auth-ca.crt --key=/data/client-auth-ca.key",
	}

	for i, cmdLine := range cmdLines {
		cid := createDockerID(fmt.Sprintf("starter-test-cluster-sync-util-%d", i))
		dockerRun := Spawn(t, strings.Join([]string{
			"docker run -i",
			"--label starter-test=true",
			"--name=" + cid,
			"--rm",
			fmt.Sprintf("-v %s:/data", dir),
			image,
			cmdLine,
		}, " "))
		defer dockerRun.Close()
		defer removeDockerContainer(t, cid)

		if err := dockerRun.WaitTimeout(time.Minute); err != nil {
			t.Fatalf("Failed to run '%s': %s", cmdLine, err)
		}
	}

	// Create cluster secret file
	secret := make([]byte, 32)
	secretFile := filepath.Join(dir, "cluster-secret")
	rand.Read(secret)
	secretEncoded := hex.EncodeToString(secret)
	if err := ioutil.WriteFile(secretFile, []byte(secretEncoded), 0644); err != nil {
		t.Fatalf("Failed to create secret file: %s", err)
	}

	result := syncCertificates{}
	result.Dir = dir
	result.TLS.CACertificate = filepath.Join(dir, "tls-ca.crt")
	result.TLS.CAKey = filepath.Join(dir, "tls-ca.key")
	result.TLS.DCA.Keyfile = filepath.Join(dir, "tls-a.keyfile")
	result.TLS.DCB.Keyfile = filepath.Join(dir, "tls-b.keyfile")
	result.ClientAuth.CACertificate = filepath.Join(dir, "client-auth-ca.crt")
	result.ClientAuth.CAKey = filepath.Join(dir, "client-auth-ca.key")
	result.ClusterSecret = secretFile

	return result
}
