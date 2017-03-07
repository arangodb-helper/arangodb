package service

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"math/big"
	"net"
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
