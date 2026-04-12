// Copyright © 2026 BubbleFish Technologies, Inc.
//
// This file is part of BubbleFish Nexus.
//
// BubbleFish Nexus is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// BubbleFish Nexus is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with BubbleFish Nexus. If not, see <https://www.gnu.org/licenses/>.

package jwtauth

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// benchSignJWT creates a signed RS256 JWT for benchmark setup.
func benchSignJWT(key *rsa.PrivateKey, kid string, claims map[string]interface{}) (string, error) {
	header := map[string]string{"alg": "RS256", "kid": kid, "typ": "JWT"}
	headerJSON, _ := json.Marshal(header)
	claimsJSON, _ := json.Marshal(claims)

	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)
	claimsB64 := base64.RawURLEncoding.EncodeToString(claimsJSON)

	sigInput := headerB64 + "." + claimsB64
	h := sha256.Sum256([]byte(sigInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, h[:])
	if err != nil {
		return "", fmt.Errorf("sign: %w", err)
	}
	return sigInput + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// benchRSAJWKS generates a JWKS JSON response for an RSA public key.
func benchRSAJWKS(key *rsa.PublicKey, kid string) []byte {
	nB64 := base64.RawURLEncoding.EncodeToString(key.N.Bytes())
	eB64 := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())
	jwks := fmt.Sprintf(`{"keys":[{"kty":"RSA","kid":"%s","alg":"RS256","use":"sig","n":"%s","e":"%s"}]}`,
		kid, nB64, eB64)
	return []byte(jwks)
}

func benchSetup(b *testing.B) (v *Validator, validToken, expiredToken string) {
	b.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		b.Fatalf("generate RSA key: %v", err)
	}

	kid := "bench-kid-1"
	jwksData := benchRSAJWKS(&key.PublicKey, kid)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(jwksData)
	}))
	b.Cleanup(srv.Close)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	v = New(Config{
		JWKSUrl:       srv.URL,
		ClaimToSource: "sub",
		Audience:      "nexus",
		Logger:        logger,
	})
	if err := v.FetchJWKS(); err != nil {
		b.Fatalf("fetch JWKS: %v", err)
	}

	now := time.Now()
	validClaims := map[string]interface{}{
		"sub": "bench-source",
		"aud": "nexus",
		"iss": "bench-issuer",
		"iat": now.Unix(),
		"exp": now.Add(time.Hour).Unix(),
	}
	validToken, err = benchSignJWT(key, kid, validClaims)
	if err != nil {
		b.Fatalf("sign valid token: %v", err)
	}

	expiredClaims := map[string]interface{}{
		"sub": "bench-source",
		"aud": "nexus",
		"iss": "bench-issuer",
		"iat": now.Add(-2 * time.Hour).Unix(),
		"exp": now.Add(-1 * time.Hour).Unix(),
	}
	expiredToken, err = benchSignJWT(key, kid, expiredClaims)
	if err != nil {
		b.Fatalf("sign expired token: %v", err)
	}

	return v, validToken, expiredToken
}

// BenchmarkJWT_Validate_ValidToken measures the cost of validating a known-good
// JWT token. This is on the hot path of every authenticated MCP request.
func BenchmarkJWT_Validate_ValidToken(b *testing.B) {
	b.ReportAllocs()
	v, validToken, _ := benchSetup(b)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := v.Validate(validToken)
		if err != nil {
			b.Fatalf("validate: %v", err)
		}
		if result == nil {
			b.Fatal("expected non-nil result")
		}
	}
}

// BenchmarkJWT_Validate_ExpiredToken measures the early-rejection path for
// expired tokens.
func BenchmarkJWT_Validate_ExpiredToken(b *testing.B) {
	b.ReportAllocs()
	v, _, expiredToken := benchSetup(b)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := v.Validate(expiredToken)
		if err == nil {
			b.Fatal("expected error for expired token")
		}
	}
}
