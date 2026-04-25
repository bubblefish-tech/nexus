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
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func setupCacheTest(t *testing.T) (*Validator, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	kid := "cache-test-kid"
	jwksData := benchRSAJWKS(&key.PublicKey, kid)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(jwksData)
	}))
	t.Cleanup(srv.Close)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	v := New(Config{JWKSUrl: srv.URL, ClaimToSource: "sub", Audience: "nexus", Logger: logger})
	if err := v.FetchJWKS(); err != nil {
		t.Fatal(err)
	}

	claims := map[string]interface{}{
		"sub": "test-source",
		"aud": "nexus",
		"exp": time.Now().Add(time.Hour).Unix(),
	}
	headerJSON, _ := json.Marshal(map[string]string{"alg": "RS256", "kid": kid, "typ": "JWT"})
	token, err := benchSignJWT(key, kid, claims)
	if err != nil {
		t.Fatal(err)
	}
	_ = headerJSON
	return v, token
}

func TestCacheHit_ReturnsSameClaims(t *testing.T) {
	t.Helper()
	v, token := setupCacheTest(t)

	r1, err := v.Validate(token)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := v.Validate(token)
	if err != nil {
		t.Fatal(err)
	}
	if r1.SourceName != r2.SourceName {
		t.Errorf("cached result source %q != %q", r1.SourceName, r2.SourceName)
	}
}

func TestCacheMiss_TriggersFullValidation(t *testing.T) {
	t.Helper()
	v, token := setupCacheTest(t)

	r, err := v.Validate(token)
	if err != nil {
		t.Fatal(err)
	}
	if r.SourceName != "test-source" {
		t.Errorf("source = %q, want test-source", r.SourceName)
	}
}

func TestInvalidToken_NotCached(t *testing.T) {
	t.Helper()
	v, _ := setupCacheTest(t)

	_, err := v.Validate("bad.token.here")
	if err == nil {
		t.Fatal("expected error")
	}

	v.cacheMu.RLock()
	count := len(v.cache)
	v.cacheMu.RUnlock()
	if count != 0 {
		t.Errorf("cache should be empty after invalid token, got %d entries", count)
	}
}

func TestExpiredToken_NotCached(t *testing.T) {
	t.Helper()
	v, _ := setupCacheTest(t)

	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	kid := "cache-test-kid"
	expiredClaims := map[string]interface{}{
		"sub": "test-source",
		"aud": "nexus",
		"exp": time.Now().Add(-time.Hour).Unix(),
	}
	expiredToken, _ := benchSignJWT(key, kid, expiredClaims)

	_, err := v.Validate(expiredToken)
	if err == nil {
		t.Fatal("expected error for expired token")
	}

	v.cacheMu.RLock()
	count := len(v.cache)
	v.cacheMu.RUnlock()
	if count != 0 {
		t.Errorf("expired token should not be cached, got %d entries", count)
	}
}
