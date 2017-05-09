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
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io/ioutil"
	"math/big"
	"net"
	"strings"
	"time"
)

// CreateCertificateOptions configures how to create a certificate.
type CreateCertificateOptions struct {
	Hosts        []string // Host names and/or IP addresses
	ValidFor     time.Duration
	RSABits      int
	Organization string
}

const (
	defaultValidFor = time.Hour * 24 * 365 // 1year
)

func publicKey(priv interface{}) interface{} {
	switch k := priv.(type) {
	case *rsa.PrivateKey:
		return &k.PublicKey
	default:
		return nil
	}
}

func pemBlockForKey(priv interface{}) *pem.Block {
	switch k := priv.(type) {
	case *rsa.PrivateKey:
		return &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)}
	default:
		return nil
	}
}

// Attempt to parse the given private key DER block. OpenSSL 0.9.8 generates
// PKCS#1 private keys by default, while OpenSSL 1.0.0 generates PKCS#8 keys.
// OpenSSL ecparam generates SEC1 EC private keys for ECDSA. We try all three.
func parsePrivateKey(der []byte) (crypto.PrivateKey, error) {
	if key, err := x509.ParsePKCS1PrivateKey(der); err == nil {
		return key, nil
	}
	if key, err := x509.ParsePKCS8PrivateKey(der); err == nil {
		switch key := key.(type) {
		case *rsa.PrivateKey, *ecdsa.PrivateKey:
			return key, nil
		default:
			return nil, maskAny(errors.New("tls: found unknown private key type in PKCS#8 wrapping"))
		}
	}
	if key, err := x509.ParseECPrivateKey(der); err == nil {
		return key, nil
	}

	return nil, maskAny(errors.New("tls: failed to parse private key"))
}

// CreateCertificate creates a self-signed certificate according to the given configuration.
// The resulting certificate + private key will be written into a single file in the given folder.
// The path of that single file is returned.
func CreateCertificate(options CreateCertificateOptions, folder string) (string, error) {
	priv, err := rsa.GenerateKey(rand.Reader, options.RSABits)
	if err != nil {
		return "", maskAny(err)
	}

	notBefore := time.Now()
	if options.ValidFor == 0 {
		options.ValidFor = defaultValidFor
	}
	notAfter := notBefore.Add(options.ValidFor)

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return "", maskAny(fmt.Errorf("failed to generate serial number: %v", err))
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{options.Organization},
		},
		NotBefore: notBefore,
		NotAfter:  notAfter,

		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	for _, h := range options.Hosts {
		if ip := net.ParseIP(h); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
		} else {
			template.DNSNames = append(template.DNSNames, h)
		}
	}

	// Create the certificate
	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, publicKey(priv), priv)
	if err != nil {
		return "", maskAny(fmt.Errorf("Failed to create certificate: %v", err))
	}

	// Write the certificate to disk
	f, err := ioutil.TempFile(folder, "key-")
	if err != nil {
		return "", maskAny(err)
	}
	defer f.Close()
	// Public key
	pem.Encode(f, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	// Private key
	pem.Encode(f, pemBlockForKey(priv))

	return f.Name(), nil
}

// LoadKeyFile loads a SSL keyfile formatted for the arangod server.
func LoadKeyFile(keyFile string) (tls.Certificate, error) {
	raw, err := ioutil.ReadFile(keyFile)
	if err != nil {
		return tls.Certificate{}, maskAny(err)
	}

	result := tls.Certificate{}
	for {
		var derBlock *pem.Block
		derBlock, raw = pem.Decode(raw)
		if derBlock == nil {
			break
		}
		if derBlock.Type == "CERTIFICATE" {
			result.Certificate = append(result.Certificate, derBlock.Bytes)
		} else if derBlock.Type == "PRIVATE KEY" || strings.HasSuffix(derBlock.Type, " PRIVATE KEY") {
			if result.PrivateKey == nil {
				result.PrivateKey, err = parsePrivateKey(derBlock.Bytes)
				if err != nil {
					return tls.Certificate{}, maskAny(err)
				}
			}
		}
	}

	if len(result.Certificate) == 0 {
		return tls.Certificate{}, maskAny(fmt.Errorf("No certificates found in %s", keyFile))
	}
	if result.PrivateKey == nil {
		return tls.Certificate{}, maskAny(fmt.Errorf("No private key found in %s", keyFile))
	}

	return result, nil
}
