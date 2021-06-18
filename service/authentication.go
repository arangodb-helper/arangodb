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
	"net/http"
	"time"

	jwt "github.com/dgrijalva/jwt-go"
)

const (
	AuthorizationHeader = "Authorization"
	BearerPrefix        = "bearer "
)

// CreateJwtToken calculates a JWT authorization token based on the given secret.
// If the secret is empty, an empty token is returned.
func CreateJwtToken(jwtSecret, user string, serverId string, paths []string, exp time.Duration, fieldsOverride jwt.MapClaims) (string, error) {
	if jwtSecret == "" {
		return "", nil
	}
	if serverId == "" {
		serverId = "foo"
	}

	// Create a new token object, specifying signing method and the claims
	// you would like it to contain.
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
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	// Sign and get the complete encoded token as a string using the secret
	signedToken, err := token.SignedString([]byte(jwtSecret))
	if err != nil {
		return "", maskAny(err)
	}

	return signedToken, nil
}

// addJwtHeader calculates a JWT authorization header based on the given secret
// and adds it to the given request.
// If the secret is empty, nothing is done.
func addJwtHeader(req *http.Request, jwtSecret string) error {
	if jwtSecret == "" {
		return nil
	}
	signedToken, err := CreateJwtToken(jwtSecret, "", "", nil, 0, nil)
	if err != nil {
		return maskAny(err)
	}

	req.Header.Set(AuthorizationHeader, BearerPrefix+signedToken)
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
