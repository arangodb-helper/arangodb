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

	jwt "github.com/dgrijalva/jwt-go"
)

// addJwtHeader calculates a JWT authorization header based on the given secret
// and adds it to the given request.
// If the secret is empty, nothing is done.
func addJwtHeader(req *http.Request, jwtSecret string) error {
	if jwtSecret == "" {
		return nil
	}
	// Create a new token object, specifying signing method and the claims
	// you would like it to contain.
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss":       "arangodb",
		"server_id": "foo",
	})

	// Sign and get the complete encoded token as a string using the secret
	signedToken, err := token.SignedString([]byte(jwtSecret))
	if err != nil {
		return maskAny(err)
	}

	req.Header.Set("Authorization", "bearer "+signedToken)
	return nil
}
