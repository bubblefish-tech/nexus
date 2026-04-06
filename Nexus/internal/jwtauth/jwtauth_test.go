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
	"crypto/ecdsa"
	"crypto/elliptic"
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
	"strings"
	"testing"
	"time"
)

func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

// signJWT creates a signed JWT using RS256 for testing.
func signJWT(t *testing.T, key *rsa.PrivateKey, kid string, claims map[string]interface{}) string {
	t.Helper()
	header := map[string]string{"alg": "RS256", "kid": kid, "typ": "JWT"}
	headerJSON, _ := json.Marshal(header)
	claimsJSON, _ := json.Marshal(claims)

	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)
	claimsB64 := base64.RawURLEncoding.EncodeToString(claimsJSON)

	sigInput := headerB64 + "." + claimsB64
	h := sha256.Sum256([]byte(sigInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, h[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	sigB64 := base64.RawURLEncoding.EncodeToString(sig)
	return sigInput + "." + sigB64
}

// signJWTEC creates a signed JWT using ES256 for testing.
func signJWTEC(t *testing.T, key *ecdsa.PrivateKey, kid string, claims map[string]interface{}) string {
	t.Helper()
	header := map[string]string{"alg": "ES256", "kid": kid, "typ": "JWT"}
	headerJSON, _ := json.Marshal(header)
	claimsJSON, _ := json.Marshal(claims)

	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)
	claimsB64 := base64.RawURLEncoding.EncodeToString(claimsJSON)

	sigInput := headerB64 + "." + claimsB64
	h := sha256.Sum256([]byte(sigInput))
	r, s, err := ecdsa.Sign(rand.Reader, key, h[:])
	if err != nil {
		t.Fatalf("sign EC: %v", err)
	}

	keySize := (key.Curve.Params().BitSize + 7) / 8
	rBytes := r.Bytes()
	sBytes := s.Bytes()
	// Pad to keySize.
	sigBytes := make([]byte, 2*keySize)
	copy(sigBytes[keySize-len(rBytes):keySize], rBytes)
	copy(sigBytes[2*keySize-len(sBytes):], sBytes)

	sigB64 := base64.RawURLEncoding.EncodeToString(sigBytes)
	return sigInput + "." + sigB64
}

func rsaJWKS(t *testing.T, key *rsa.PublicKey, kid string) []byte {
	t.Helper()
	nB64 := base64.RawURLEncoding.EncodeToString(key.N.Bytes())
	eB64 := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())
	jwks := fmt.Sprintf(`{"keys":[{"kty":"RSA","kid":"%s","alg":"RS256","use":"sig","n":"%s","e":"%s"}]}`,
		kid, nB64, eB64)
	return []byte(jwks)
}

func ecJWKS(t *testing.T, key *ecdsa.PublicKey, kid string) []byte {
	t.Helper()
	xB64 := base64.RawURLEncoding.EncodeToString(key.X.Bytes())
	yB64 := base64.RawURLEncoding.EncodeToString(key.Y.Bytes())
	jwks := fmt.Sprintf(`{"keys":[{"kty":"EC","kid":"%s","alg":"ES256","use":"sig","crv":"P-256","x":"%s","y":"%s"}]}`,
		kid, xB64, yB64)
	return []byte(jwks)
}

func TestValidateRSA(t *testing.T) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(rsaJWKS(t, &key.PublicKey, "test-kid"))
	}))
	defer srv.Close()

	v := New(Config{
		JWKSUrl:       srv.URL,
		ClaimToSource: "sub",
		Logger:        testLogger(t),
	})
	if err := v.FetchJWKS(); err != nil {
		t.Fatalf("FetchJWKS: %v", err)
	}

	token := signJWT(t, key, "test-kid", map[string]interface{}{
		"sub": "claude",
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	})

	result, err := v.Validate(token)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if result.SourceName != "claude" {
		t.Errorf("SourceName = %q, want claude", result.SourceName)
	}
}

func TestValidateEC(t *testing.T) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate EC key: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(ecJWKS(t, &key.PublicKey, "ec-kid"))
	}))
	defer srv.Close()

	v := New(Config{
		JWKSUrl:       srv.URL,
		ClaimToSource: "sub",
		Logger:        testLogger(t),
	})
	if err := v.FetchJWKS(); err != nil {
		t.Fatalf("FetchJWKS: %v", err)
	}

	token := signJWTEC(t, key, "ec-kid", map[string]interface{}{
		"sub": "chatgpt",
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	})

	result, err := v.Validate(token)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if result.SourceName != "chatgpt" {
		t.Errorf("SourceName = %q, want chatgpt", result.SourceName)
	}
}

func TestValidateExpired(t *testing.T) {
	t.Helper()
	key, _ := rsa.GenerateKey(rand.Reader, 2048)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(rsaJWKS(t, &key.PublicKey, "kid1"))
	}))
	defer srv.Close()

	v := New(Config{JWKSUrl: srv.URL, ClaimToSource: "sub", Logger: testLogger(t)})
	v.FetchJWKS()

	token := signJWT(t, key, "kid1", map[string]interface{}{
		"sub": "claude",
		"exp": float64(time.Now().Add(-time.Hour).Unix()),
	})

	_, err := v.Validate(token)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Errorf("error = %q, want 'expired'", err)
	}
}

func TestValidateAudienceMismatch(t *testing.T) {
	t.Helper()
	key, _ := rsa.GenerateKey(rand.Reader, 2048)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(rsaJWKS(t, &key.PublicKey, "kid1"))
	}))
	defer srv.Close()

	v := New(Config{
		JWKSUrl:       srv.URL,
		ClaimToSource: "sub",
		Audience:      "nexus-api",
		Logger:        testLogger(t),
	})
	v.FetchJWKS()

	token := signJWT(t, key, "kid1", map[string]interface{}{
		"sub": "claude",
		"aud": "wrong-audience",
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	})

	_, err := v.Validate(token)
	if err == nil {
		t.Fatal("expected error for audience mismatch")
	}
	if !strings.Contains(err.Error(), "audience") {
		t.Errorf("error = %q, want 'audience'", err)
	}
}

func TestValidateAudienceMatch(t *testing.T) {
	t.Helper()
	key, _ := rsa.GenerateKey(rand.Reader, 2048)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(rsaJWKS(t, &key.PublicKey, "kid1"))
	}))
	defer srv.Close()

	v := New(Config{
		JWKSUrl:       srv.URL,
		ClaimToSource: "sub",
		Audience:      "nexus-api",
		Logger:        testLogger(t),
	})
	v.FetchJWKS()

	// String audience.
	token := signJWT(t, key, "kid1", map[string]interface{}{
		"sub": "claude",
		"aud": "nexus-api",
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	})
	result, err := v.Validate(token)
	if err != nil {
		t.Fatalf("Validate string aud: %v", err)
	}
	if result.SourceName != "claude" {
		t.Errorf("SourceName = %q, want claude", result.SourceName)
	}

	// Array audience.
	token2 := signJWT(t, key, "kid1", map[string]interface{}{
		"sub": "claude",
		"aud": []string{"other", "nexus-api"},
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	})
	result2, err := v.Validate(token2)
	if err != nil {
		t.Fatalf("Validate array aud: %v", err)
	}
	if result2.SourceName != "claude" {
		t.Errorf("SourceName = %q, want claude", result2.SourceName)
	}
}

func TestValidateMalformedToken(t *testing.T) {
	t.Helper()
	v := New(Config{Logger: testLogger(t)})

	tests := []struct {
		name  string
		token string
	}{
		{"empty", ""},
		{"one part", "abc"},
		{"two parts", "abc.def"},
		{"bad base64 header", "!!!.def.ghi"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := v.Validate(tt.token)
			if err == nil {
				t.Error("expected error")
			}
		})
	}
}

func TestValidateUnknownKidTriggersRefresh(t *testing.T) {
	t.Helper()
	key, _ := rsa.GenerateKey(rand.Reader, 2048)

	fetchCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCount++
		if fetchCount == 1 {
			// First fetch: return a key with a different kid.
			w.Write(rsaJWKS(t, &key.PublicKey, "old-kid"))
		} else {
			// Subsequent fetches: return the rotated key.
			w.Write(rsaJWKS(t, &key.PublicKey, "rotated-kid"))
		}
	}))
	defer srv.Close()

	v := New(Config{JWKSUrl: srv.URL, ClaimToSource: "sub", Logger: testLogger(t)})
	v.FetchJWKS()

	// Force lastRefresh to be old enough to allow refresh.
	v.mu.Lock()
	v.lastRefresh = time.Now().Add(-2 * time.Minute)
	v.mu.Unlock()

	// Token signed with rotated-kid which is not in cache yet.
	token := signJWT(t, key, "rotated-kid", map[string]interface{}{
		"sub": "claude",
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	})

	result, err := v.Validate(token)
	if err != nil {
		t.Fatalf("Validate after rotation: %v", err)
	}
	if result.SourceName != "claude" {
		t.Errorf("SourceName = %q, want claude", result.SourceName)
	}
	if fetchCount < 2 {
		t.Errorf("expected at least 2 JWKS fetches (initial + refresh), got %d", fetchCount)
	}
}

func TestValidateMissingClaim(t *testing.T) {
	t.Helper()
	key, _ := rsa.GenerateKey(rand.Reader, 2048)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(rsaJWKS(t, &key.PublicKey, "kid1"))
	}))
	defer srv.Close()

	v := New(Config{JWKSUrl: srv.URL, ClaimToSource: "group", Logger: testLogger(t)})
	v.FetchJWKS()

	token := signJWT(t, key, "kid1", map[string]interface{}{
		"sub": "claude",
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	})

	_, err := v.Validate(token)
	if err == nil {
		t.Fatal("expected error for missing claim")
	}
	if !strings.Contains(err.Error(), "group") {
		t.Errorf("error = %q, want mention of 'group'", err)
	}
}

func TestFetchJWKSError(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	v := New(Config{JWKSUrl: srv.URL, Logger: testLogger(t)})
	err := v.FetchJWKS()
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}
