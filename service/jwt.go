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
