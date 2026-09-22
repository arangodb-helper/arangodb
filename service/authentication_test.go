package service

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/arangodb-helper/arangodb/pkg/definitions"
)

// TestCreateJwtTokenSigning verifies signatures and claims for shared secrets
// and supported EC key encodings, including tokens attached by addJwtHeader.
func TestCreateJwtTokenSigning(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	sec1, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	pkcs8, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	public, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	publicPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public}))
	privatePEM := string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: sec1}))
	for _, tc := range []struct {
		name, secret, algorithm string
		verificationKey         interface{}
	}{
		{"shared secret", "0123456789abcdef0123456789abcdef", "HS256", []byte("0123456789abcdef0123456789abcdef")},
		{"SEC1", privatePEM, "ES256", &key.PublicKey},
		{"PKCS8", string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8})), "ES256", &key.PublicKey},
		{"public then private", publicPEM + privatePEM, "ES256", &key.PublicKey},
	} {
		t.Run(tc.name, func(t *testing.T) {
			signed, err := CreateJwtToken(tc.secret, "alice", "server", []string{"/_api/version"}, time.Hour, jwt.MapClaims{"custom": "value"})
			require.NoError(t, err)
			token, err := jwt.Parse(signed, func(token *jwt.Token) (interface{}, error) {
				return tc.verificationKey, nil
			}, jwt.WithValidMethods([]string{tc.algorithm}), jwt.WithIssuer("arangodb"), jwt.WithExpirationRequired())
			require.NoError(t, err)
			require.True(t, token.Valid)
			claims := token.Claims.(jwt.MapClaims)
			require.Equal(t, "server", claims["server_id"])
			require.Equal(t, "alice", claims["preferred_username"])
			require.Equal(t, []interface{}{"/_api/version"}, claims["allowed_paths"])
			require.Equal(t, "value", claims["custom"])
			require.InDelta(t, time.Now().Unix(), claims["iat"], 2)
			require.InDelta(t, time.Now().Add(time.Hour).Unix(), claims["exp"], 2)

			req := httptest.NewRequest("GET", "/_api/version", nil)
			require.NoError(t, addJwtHeader(req, tc.secret))
			token, err = jwt.Parse(strings.TrimPrefix(req.Header.Get(AuthorizationHeader), BearerPrefix), func(token *jwt.Token) (interface{}, error) {
				return tc.verificationKey, nil
			}, jwt.WithValidMethods([]string{tc.algorithm}))
			require.NoError(t, err)
			require.Equal(t, "foo", token.Claims.(jwt.MapClaims)["server_id"])

			header, err := CreateJwtAuthorizationHeader(tc.secret, "starter")
			require.NoError(t, err)
			require.True(t, strings.HasPrefix(header, BearerPrefix))
			token, err = jwt.Parse(strings.TrimPrefix(header, BearerPrefix), func(token *jwt.Token) (interface{}, error) {
				return tc.verificationKey, nil
			}, jwt.WithValidMethods([]string{tc.algorithm}), jwt.WithIssuer("arangodb"))
			require.NoError(t, err)
			require.Equal(t, "starter", token.Claims.(jwt.MapClaims)["server_id"])
		})
	}
}

// TestCreateJwtTokenInvalidPEM ensures malformed, public-only, and unsupported
// curve inputs fail without returning a token or setting an Authorization header.
func TestCreateJwtTokenInvalidPEM(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	require.NoError(t, err)
	private, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	public, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	for _, secret := range []string{
		"-----BEGIN PRIVATE KEY-----\ninvalid\n-----END PRIVATE KEY-----",
		"-----BEGIN EC PRIVATE KEY-----",
		string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public})),
		string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: private})),
	} {
		signed, err := CreateJwtToken(secret, "", "", nil, 0, nil)
		require.Error(t, err)
		require.Empty(t, signed)
		req := httptest.NewRequest("GET", "/", nil)
		require.Error(t, addJwtHeader(req, secret))
		require.Empty(t, req.Header.Get(AuthorizationHeader))
		header, err := CreateJwtAuthorizationHeader(secret, "starter")
		require.Error(t, err)
		require.Empty(t, header)
		s := &Service{jwtSecret: secret}
		require.Error(t, s.PrepareDatabaseServerRequestFunc()(req))
	}
}

// TestCreateJwtTokenCollapsedPEM rejects a valid signing key after its whitespace
// has been collapsed, as happens with an unquoted shell echo of a PEM variable.
func TestCreateJwtTokenCollapsedPEM(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	secret := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))

	// Establish that the original key is usable before simulating newline loss.
	token, err := CreateJwtToken(secret, "", "starter", nil, 0, nil)
	require.NoError(t, err)
	require.NotEmpty(t, token)
	collapsed := strings.Join(strings.Fields(secret), " ") + "\n"
	token, err = CreateJwtToken(collapsed, "", "starter", nil, 0, nil)
	require.EqualError(t, err, "JWT PEM secret must contain an unencrypted EC private key")
	require.Empty(t, token)

	header, err := CreateJwtAuthorizationHeader(collapsed, "starter")
	require.Error(t, err)
	require.Empty(t, header)
	req := httptest.NewRequest("GET", "/_api/version", nil)
	s := &Service{jwtSecret: collapsed}
	require.Error(t, s.PrepareDatabaseServerRequestFunc()(req))
	require.Empty(t, req.Header.Get(AuthorizationHeader))
}

// TestCreateJwtTokenEmptySecret preserves the no-authentication behavior when
// no secret is configured: no token is generated and no header is added.
func TestCreateJwtTokenEmptySecret(t *testing.T) {
	signed, err := CreateJwtToken("", "", "", nil, 0, nil)
	require.NoError(t, err)
	require.Empty(t, signed)
	req := httptest.NewRequest("GET", "/", nil)
	require.NoError(t, addJwtHeader(req, ""))
	require.Empty(t, req.Header.Get(AuthorizationHeader))
	header, err := CreateJwtAuthorizationHeader("", "starter")
	require.NoError(t, err)
	require.Empty(t, header)
}

// TestServiceJwtClients verifies the Authorization headers actually sent by
// database, Agency, and coordinator clients for both shared and EC secrets.
func TestServiceJwtClients(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	for _, tc := range []struct {
		algorithm, secret string
		verificationKey   interface{}
	}{
		{"HS256", "0123456789abcdef0123456789abcdef", []byte("0123456789abcdef0123456789abcdef")},
		{"ES256", string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})), &key.PublicKey},
	} {
		t.Run(tc.algorithm, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				token, err := jwt.Parse(strings.TrimPrefix(r.Header.Get(AuthorizationHeader), BearerPrefix), func(token *jwt.Token) (interface{}, error) {
					return tc.verificationKey, nil
				}, jwt.WithValidMethods([]string{tc.algorithm}), jwt.WithIssuer("arangodb"))
				if err != nil || token.Claims.(jwt.MapClaims)["server_id"] != "starter" {
					http.Error(w, "invalid JWT", http.StatusUnauthorized)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/_api/agency/read" {
					fmt.Fprint(w, `[{"test":"ok"}]`)
				} else {
					fmt.Fprint(w, `{"server":"arango","version":"3.12.11"}`)
				}
			}))
			defer server.Close()
			s := &Service{jwtSecret: tc.secret}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			for _, connectionType := range []ConnectionType{ConnectionTypeDatabase, ConnectionTypeAgency} {
				client, err := s.CreateClient([]string{server.URL}, connectionType, definitions.ServerTypeUnknown)
				require.NoError(t, err)
				_, err = client.Version(ctx)
				require.NoError(t, err)
			}
			address := server.Listener.Addr().(*net.TCPAddr)
			agencyCluster := ClusterConfig{AllPeers: []Peer{{
				Address:     address.IP.String(),
				Port:        address.Port - definitions.ServerType(definitions.ServerTypeAgent).PortOffset(),
				peerServers: peerServers{HasAgentFlag: true},
			}}}
			agency, err := agencyCluster.CreateAgencyAPI(s)
			require.NoError(t, err)
			var value string
			require.NoError(t, agency.ReadKey(ctx, []string{"test"}, &value))
			require.Equal(t, "ok", value)

			cluster := ClusterConfig{AllPeers: []Peer{{
				Address: address.IP.String(),
				Port:    address.Port - definitions.ServerType(definitions.ServerTypeCoordinator).PortOffset(),
			}}}
			client, err := cluster.CreateCoordinatorsClient(tc.secret)
			require.NoError(t, err)
			_, err = client.Version(ctx)
			require.NoError(t, err)
		})
	}
}
