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
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/pem"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	AuthorizationHeader = "Authorization"
	BearerPrefix        = "bearer "
)

// CreateJwtToken signs an ArangoDB JWT using HS256 for a shared secret or ES256
// for a PEM-encoded P-256 private key. ES256 requires ArangoDB 3.12.8 or later.
// Invalid or unsupported PEM keys return an error; they are never used as
// HMAC secrets. An empty secret returns no token.
//
// An empty serverId defaults to "foo". User and path restrictions are included
// when supplied, and a positive exp adds issued-at and expiration timestamps.
// fieldsOverride is applied last and can replace any generated claim.
func CreateJwtToken(jwtSecret, user string, serverId string, paths []string, exp time.Duration, fieldsOverride jwt.MapClaims) (string, error) {
	if jwtSecret == "" {
		return "", nil
	}
	if serverId == "" {
		serverId = "foo"
	}

	// Build the default server claims before applying optional restrictions.
	claims := jwt.MapClaims{
		"iss":       "arangodb",
		"server_id": serverId,
	}
	if user != "" {
		claims["preferred_username"] = user
	}
	if paths != nil {
		claims["allowed_paths"] = paths
	}
	if exp > 0 {
		t := time.Now().UTC()
		claims["iat"] = t.Unix()
		claims["exp"] = t.Add(exp).Unix()
	}
	for k, v := range fieldsOverride {
		claims[k] = v
	}
	// Match signing to the key format while retaining shared-secret support.
	// Treat a PEM marker as key input even if decoding fails, so malformed keys
	// cannot silently produce HS256 tokens from their PEM text.
	var method jwt.SigningMethod = jwt.SigningMethodHS256
	var key interface{} = []byte(jwtSecret)
	if strings.Contains(jwtSecret, "-----BEGIN") {
		privateKey, err := jwtECPrivateKey(jwtSecret)
		if err != nil {
			return "", maskAny(err)
		}
		method, key = jwt.SigningMethodES256, privateKey
	}
	token := jwt.NewWithClaims(method, claims)

	// Sign with the shared-secret bytes or the parsed EC private key.
	signedToken, err := token.SignedString(key)
	if err != nil {
		return "", maskAny(err)
	}

	return signedToken, nil
}

// jwtECPrivateKey finds the first EC PRIVATE KEY (SEC1) or PRIVATE KEY (PKCS8)
// block in a PEM bundle and parses it as an unencrypted P-256 private key for
// ES256 signing. Other blocks are skipped so a public key may precede it.
// It returns an error if no supported private-key block exists, the selected
// block cannot be parsed as an EC private key, or its curve is not P-256.
func jwtECPrivateKey(secret string) (*ecdsa.PrivateKey, error) {
	remaining := []byte(secret)
	for len(remaining) > 0 {
		block, rest := pem.Decode(remaining)
		if block == nil {
			break
		}
		remaining = rest
		if block.Type != "EC PRIVATE KEY" && block.Type != "PRIVATE KEY" {
			continue
		}
		key, err := jwt.ParseECPrivateKeyFromPEM(pem.EncodeToMemory(block))
		if err != nil {
			return nil, fmt.Errorf("invalid JWT EC private key: %w", err)
		}
		if key.Curve != elliptic.P256() {
			return nil, fmt.Errorf("JWT ES256 signing requires a P-256 private key")
		}
		return key, nil
	}
	return nil, fmt.Errorf("JWT PEM secret must contain an unencrypted EC private key")
}

// CreateJwtAuthorizationHeader returns a bearer Authorization header value for
// an ArangoDB server, using the key selection and validation in CreateJwtToken.
// An empty secret returns an empty value; an empty serverId defaults to "foo".
// Invalid signing keys return an error without a header value.
func CreateJwtAuthorizationHeader(jwtSecret, serverId string) (string, error) {
	token, err := CreateJwtToken(jwtSecret, "", serverId, nil, 0, nil)
	if err != nil {
		return "", maskAny(err)
	}
	if token == "" {
		return "", nil
	}
	return BearerPrefix + token, nil
}

// addJwtHeader calculates a JWT authorization header based on the given secret
// and adds it to the given request.
// If the secret is empty, nothing is done.
func addJwtHeader(req *http.Request, jwtSecret string) error {
	if jwtSecret == "" {
		return nil
	}
	header, err := CreateJwtAuthorizationHeader(jwtSecret, "")
	if err != nil {
		return maskAny(err)
	}

	req.Header.Set(AuthorizationHeader, header)
	return nil
}

// addBearerTokenHeader adds an authorization header based on the given bearer token
// to the given request.
// If the given token is empty, nothing is done.
func addBearerTokenHeader(req *http.Request, bearerToken string) error {
	if bearerToken == "" {
		return nil
	}

	req.Header.Set(AuthorizationHeader, BearerPrefix+bearerToken)
	return nil
}
