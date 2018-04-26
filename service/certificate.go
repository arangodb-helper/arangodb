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
	"crypto/tls"
	"crypto/x509/pkix"
	"io/ioutil"
	"strings"
	"time"

	certificates "github.com/arangodb-helper/go-certificates"
)

// CreateCertificateOptions configures how to create a certificate.
type CreateCertificateOptions struct {
	Hosts        []string // Host names and/or IP addresses
	ValidFor     time.Duration
	Organization string
}

const (
	defaultValidFor = time.Hour * 24 * 365 // 1year
	defaultCurve    = "P256"
)

// CreateCertificate creates a self-signed certificate according to the given configuration.
// The resulting certificate + private key will be written into a single file in the given folder.
// The path of that single file is returned.
func CreateCertificate(options CreateCertificateOptions, folder string) (string, error) {
	if options.ValidFor == 0 {
		options.ValidFor = defaultValidFor
	}
	certOpts := certificates.CreateCertificateOptions{
		Hosts: options.Hosts,
		Subject: &pkix.Name{
			Organization: []string{options.Organization},
		},
		ValidFrom:  time.Now(),
		ValidFor:   options.ValidFor,
		ECDSACurve: defaultCurve,
	}

	// Create self-signed certificate
	cert, priv, err := certificates.CreateCertificate(certOpts, nil)
	if err != nil {
		return "", maskAny(err)
	}

	// Write the certificate to disk
	f, err := ioutil.TempFile(folder, "key-")
	if err != nil {
		return "", maskAny(err)
	}
	defer f.Close()
	content := strings.TrimSpace(cert) + "\n" + priv
	if _, err := f.WriteString(content); err != nil {
		return "", maskAny(err)
	}

	return f.Name(), nil
}

// LoadKeyFile loads a SSL keyfile formatted for the arangod server.
func LoadKeyFile(keyFile string) (tls.Certificate, error) {
	result, err := certificates.LoadKeyFile(keyFile)
	if err != nil {
		return tls.Certificate{}, maskAny(err)
	}
	return result, nil
}
