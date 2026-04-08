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

package oauth

import (
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ── Key Infrastructure Tests ─────────────────────────────────────────────────

func TestKeyGeneration(t *testing.T) {
	key, err := GenerateRSAKey()
	if err != nil {
		t.Fatalf("GenerateRSAKey: %v", err)
	}
	if key.N.BitLen() != 2048 {
		t.Errorf("expected 2048-bit key, got %d", key.N.BitLen())
	}
}

func TestKeyLoadSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.key")

	// Generate and save.
	key, err := GenerateRSAKey()
	if err != nil {
		t.Fatalf("GenerateRSAKey: %v", err)
	}
	if err := SaveRSAKey(key, path); err != nil {
		t.Fatalf("SaveRSAKey: %v", err)
	}

	// Verify file exists.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("key file is empty")
	}

	// Load and compare.
	loaded, err := LoadRSAKey(path)
	if err != nil {
		t.Fatalf("LoadRSAKey: %v", err)
	}
	if loaded.N.Cmp(key.N) != 0 {
		t.Error("loaded key modulus does not match original")
	}
	if loaded.E != key.E {
		t.Error("loaded key exponent does not match original")
	}
}

func TestKeyLoadSave_PKCS1Fallback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.key")

	key, err := GenerateRSAKey()
	if err != nil {
		t.Fatalf("GenerateRSAKey: %v", err)
	}

	// Save as PKCS#8 via SaveRSAKey, load back — should work.
	if err := SaveRSAKey(key, path); err != nil {
		t.Fatalf("SaveRSAKey: %v", err)
	}
	loaded, err := LoadRSAKey(path)
	if err != nil {
		t.Fatalf("LoadRSAKey PKCS#8: %v", err)
	}
	if loaded.N.Cmp(key.N) != 0 {
		t.Error("PKCS#8 round-trip failed")
	}
}

// ── JWT Tests ────────────────────────────────────────────────────────────────

func TestJWTSignAndVerify(t *testing.T) {
	key, err := GenerateRSAKey()
	if err != nil {
		t.Fatalf("GenerateRSAKey: %v", err)
	}

	issuer := "https://nexus.example.com"
	tokenStr, err := SignJWT(key, issuer, "chatgpt", "mcp", "default", time.Hour)
	if err != nil {
		t.Fatalf("SignJWT: %v", err)
	}
	if tokenStr == "" {
		t.Fatal("SignJWT returned empty token")
	}

	claims, err := ValidateJWT(tokenStr, &key.PublicKey, issuer)
	if err != nil {
		t.Fatalf("ValidateJWT: %v", err)
	}
	if claims.BFNSource != "default" {
		t.Errorf("expected bfn_source=default, got %s", claims.BFNSource)
	}
	if claims.Scope != "mcp" {
		t.Errorf("expected scope=mcp, got %s", claims.Scope)
	}
	if claims.Subject != "chatgpt" {
		t.Errorf("expected sub=chatgpt, got %s", claims.Subject)
	}
	if claims.Issuer != issuer {
		t.Errorf("expected iss=%s, got %s", issuer, claims.Issuer)
	}
}

func TestJWTExpiredRejected(t *testing.T) {
	key, err := GenerateRSAKey()
	if err != nil {
		t.Fatalf("GenerateRSAKey: %v", err)
	}

	issuer := "https://nexus.example.com"
	// Sign with -1 hour TTL (already expired).
	tokenStr, err := SignJWT(key, issuer, "chatgpt", "mcp", "default", -1*time.Hour)
	if err != nil {
		t.Fatalf("SignJWT: %v", err)
	}

	_, err = ValidateJWT(tokenStr, &key.PublicKey, issuer)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestJWTWrongAudienceRejected(t *testing.T) {
	key, err := GenerateRSAKey()
	if err != nil {
		t.Fatalf("GenerateRSAKey: %v", err)
	}

	// Sign a token with the wrong audience by directly constructing claims.
	issuer := "https://nexus.example.com"
	tokenStr, err := signWithCustomAudience(key, issuer, "wrong-audience")
	if err != nil {
		t.Fatalf("signWithCustomAudience: %v", err)
	}

	_, err = ValidateJWT(tokenStr, &key.PublicKey, issuer)
	if err == nil {
		t.Fatal("expected error for wrong audience")
	}
}

func TestJWTWrongIssuerRejected(t *testing.T) {
	key, err := GenerateRSAKey()
	if err != nil {
		t.Fatalf("GenerateRSAKey: %v", err)
	}

	tokenStr, err := SignJWT(key, "https://evil.example.com", "chatgpt", "mcp", "default", time.Hour)
	if err != nil {
		t.Fatalf("SignJWT: %v", err)
	}

	_, err = ValidateJWT(tokenStr, &key.PublicKey, "https://nexus.example.com")
	if err == nil {
		t.Fatal("expected error for wrong issuer")
	}
}

func TestJWTWrongKeyRejected(t *testing.T) {
	key1, err := GenerateRSAKey()
	if err != nil {
		t.Fatalf("GenerateRSAKey 1: %v", err)
	}
	key2, err := GenerateRSAKey()
	if err != nil {
		t.Fatalf("GenerateRSAKey 2: %v", err)
	}

	issuer := "https://nexus.example.com"
	tokenStr, err := SignJWT(key1, issuer, "chatgpt", "mcp", "default", time.Hour)
	if err != nil {
		t.Fatalf("SignJWT: %v", err)
	}

	// Validate with the wrong public key.
	_, err = ValidateJWT(tokenStr, &key2.PublicKey, issuer)
	if err == nil {
		t.Fatal("expected error for wrong key")
	}
}

// ── Public Key / JWKS Tests ──────────────────────────────────────────────────

func TestPublicKeyToJWK(t *testing.T) {
	key, err := GenerateRSAKey()
	if err != nil {
		t.Fatalf("GenerateRSAKey: %v", err)
	}

	jwk := PublicKeyToJWK(&key.PublicKey)

	if jwk.Kty != "RSA" {
		t.Errorf("expected kty=RSA, got %s", jwk.Kty)
	}
	if jwk.Use != "sig" {
		t.Errorf("expected use=sig, got %s", jwk.Use)
	}
	if jwk.Alg != "RS256" {
		t.Errorf("expected alg=RS256, got %s", jwk.Alg)
	}
	if jwk.Kid != KeyID {
		t.Errorf("expected kid=%s, got %s", KeyID, jwk.Kid)
	}
	if jwk.N == "" {
		t.Error("n (modulus) is empty")
	}
	if jwk.E == "" {
		t.Error("e (exponent) is empty")
	}

	// Verify it marshals to valid JSON.
	data, err := json.Marshal(jwk)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	for _, field := range []string{"kty", "use", "alg", "kid", "n", "e"} {
		if _, ok := parsed[field]; !ok {
			t.Errorf("JWK JSON missing required field: %s", field)
		}
	}
}

func TestMarshalJWKS(t *testing.T) {
	key, err := GenerateRSAKey()
	if err != nil {
		t.Fatalf("GenerateRSAKey: %v", err)
	}

	data, err := MarshalJWKS(&key.PublicKey)
	if err != nil {
		t.Fatalf("MarshalJWKS: %v", err)
	}

	var resp JWKSResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal JWKS: %v", err)
	}
	if len(resp.Keys) != 1 {
		t.Fatalf("expected 1 key in JWKS, got %d", len(resp.Keys))
	}
	if resp.Keys[0].Kid != KeyID {
		t.Errorf("expected kid=%s, got %s", KeyID, resp.Keys[0].Kid)
	}
}

// ── OAuthServer Tests ────────────────────────────────────────────────────────

func TestFindClient(t *testing.T) {
	srv := NewOAuthServer(OAuthConfig{
		Clients: []OAuthClient{
			{ClientID: "chatgpt", ClientName: "ChatGPT"},
			{ClientID: "cursor", ClientName: "Cursor"},
		},
	}, mustGenKey(t), nil)

	if c := srv.FindClient("chatgpt"); c == nil {
		t.Error("expected to find client chatgpt")
	} else if c.ClientName != "ChatGPT" {
		t.Errorf("expected ClientName=ChatGPT, got %s", c.ClientName)
	}

	if c := srv.FindClient("unknown"); c != nil {
		t.Error("expected nil for unknown client")
	}
}

func TestOAuthServerDefaults(t *testing.T) {
	srv := NewOAuthServer(OAuthConfig{
		IssuerURL: "https://nexus.example.com",
	}, mustGenKey(t), nil)

	if srv.config.AccessTokenTTL != time.Hour {
		t.Errorf("expected default AccessTokenTTL=1h, got %v", srv.config.AccessTokenTTL)
	}
	if srv.config.AuthCodeTTL != 5*time.Minute {
		t.Errorf("expected default AuthCodeTTL=5m, got %v", srv.config.AuthCodeTTL)
	}
	if srv.IssuerURL() != "https://nexus.example.com" {
		t.Errorf("expected IssuerURL=https://nexus.example.com, got %s", srv.IssuerURL())
	}
}

// ── Metadata Endpoint Tests ─────────────────────────────────────────────────

func TestMetadataEndpoint(t *testing.T) {
	srv := NewOAuthServer(OAuthConfig{
		IssuerURL: "https://nexus.example.com",
	}, mustGenKey(t), slog.Default())

	mux := http.NewServeMux()
	srv.RegisterHandlers(mux)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", ct)
	}

	var meta map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &meta); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	if meta["issuer"] != "https://nexus.example.com" {
		t.Errorf("unexpected issuer: %v", meta["issuer"])
	}
}

func TestMetadataFields(t *testing.T) {
	issuer := "https://nexus.example.com"
	srv := NewOAuthServer(OAuthConfig{
		IssuerURL: issuer,
	}, mustGenKey(t), slog.Default())

	mux := http.NewServeMux()
	srv.RegisterHandlers(mux)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var meta serverMetadata
	if err := json.Unmarshal(rec.Body.Bytes(), &meta); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}

	// Required RFC 8414 fields.
	if meta.Issuer != issuer {
		t.Errorf("issuer: got %q, want %q", meta.Issuer, issuer)
	}
	if meta.AuthorizationEndpoint != issuer+"/oauth/authorize" {
		t.Errorf("authorization_endpoint: got %q", meta.AuthorizationEndpoint)
	}
	if meta.TokenEndpoint != issuer+"/oauth/token" {
		t.Errorf("token_endpoint: got %q", meta.TokenEndpoint)
	}
	if meta.JWKSURI != issuer+"/oauth/jwks" {
		t.Errorf("jwks_uri: got %q", meta.JWKSURI)
	}

	// response_types_supported must contain "code".
	if len(meta.ResponseTypesSupported) != 1 || meta.ResponseTypesSupported[0] != "code" {
		t.Errorf("response_types_supported: got %v", meta.ResponseTypesSupported)
	}

	// grant_types_supported must contain "authorization_code".
	if len(meta.GrantTypesSupported) != 1 || meta.GrantTypesSupported[0] != "authorization_code" {
		t.Errorf("grant_types_supported: got %v", meta.GrantTypesSupported)
	}

	// code_challenge_methods_supported must be ["S256"] — NEVER "plain".
	if len(meta.CodeChallengeMethodsSupported) != 1 || meta.CodeChallengeMethodsSupported[0] != "S256" {
		t.Errorf("code_challenge_methods_supported: got %v (must not include plain)", meta.CodeChallengeMethodsSupported)
	}

	// token_endpoint_auth_methods_supported must be ["none"] (PKCE replaces client secret).
	if len(meta.TokenEndpointAuthMethodsSupported) != 1 || meta.TokenEndpointAuthMethodsSupported[0] != "none" {
		t.Errorf("token_endpoint_auth_methods_supported: got %v", meta.TokenEndpointAuthMethodsSupported)
	}
}

func TestMetadataMethodNotAllowed(t *testing.T) {
	srv := NewOAuthServer(OAuthConfig{
		IssuerURL: "https://nexus.example.com",
	}, mustGenKey(t), slog.Default())

	mux := http.NewServeMux()
	srv.RegisterHandlers(mux)

	req := httptest.NewRequest(http.MethodPost, "/.well-known/oauth-authorization-server", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for POST, got %d", rec.Code)
	}
}

// ── Protected Resource Metadata Tests (RFC 9728) ────────────────────────────

func TestProtectedResourceEndpoint(t *testing.T) {
	srv := NewOAuthServer(OAuthConfig{
		IssuerURL: "https://nexus.example.com",
	}, mustGenKey(t), slog.Default())

	mux := http.NewServeMux()
	srv.RegisterHandlers(mux)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", ct)
	}

	var meta map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &meta); err != nil {
		t.Fatalf("unmarshal protected resource metadata: %v", err)
	}
	if meta["resource"] != "https://nexus.example.com" {
		t.Errorf("unexpected resource: %v", meta["resource"])
	}
}

func TestProtectedResourceFields(t *testing.T) {
	issuer := "https://nexus.example.com"
	srv := NewOAuthServer(OAuthConfig{
		IssuerURL: issuer,
	}, mustGenKey(t), slog.Default())

	mux := http.NewServeMux()
	srv.RegisterHandlers(mux)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var meta protectedResourceMetadata
	if err := json.Unmarshal(rec.Body.Bytes(), &meta); err != nil {
		t.Fatalf("unmarshal protected resource metadata: %v", err)
	}

	if meta.Resource != issuer {
		t.Errorf("resource: got %q, want %q", meta.Resource, issuer)
	}
	if len(meta.AuthorizationServers) != 1 || meta.AuthorizationServers[0] != issuer {
		t.Errorf("authorization_servers: got %v, want [%q]", meta.AuthorizationServers, issuer)
	}
	if len(meta.BearerMethodsSupported) != 1 || meta.BearerMethodsSupported[0] != "header" {
		t.Errorf("bearer_methods_supported: got %v, want [\"header\"]", meta.BearerMethodsSupported)
	}
	if meta.ResourceDocumentation != "https://github.com/bubblefish-tech/nexus" {
		t.Errorf("resource_documentation: got %q", meta.ResourceDocumentation)
	}
}

func TestProtectedResourceMethodNotAllowed(t *testing.T) {
	srv := NewOAuthServer(OAuthConfig{
		IssuerURL: "https://nexus.example.com",
	}, mustGenKey(t), slog.Default())

	mux := http.NewServeMux()
	srv.RegisterHandlers(mux)

	req := httptest.NewRequest(http.MethodPost, "/.well-known/oauth-protected-resource", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for POST, got %d", rec.Code)
	}
}

// ── Authorization Endpoint Tests ────────────────────────────────────────────

func testServer(t *testing.T) (*OAuthServer, *http.ServeMux) {
	t.Helper()
	srv := NewOAuthServer(OAuthConfig{
		IssuerURL:   "https://nexus.example.com",
		AuthCodeTTL: 5 * time.Minute,
		Clients: []OAuthClient{
			{
				ClientID:        "chatgpt",
				ClientName:      "ChatGPT",
				RedirectURIs:    []string{"https://chatgpt.com/callback"},
				OAuthSourceName: "default",
				AllowedScopes:   []string{"openid", "mcp"},
			},
		},
	}, mustGenKey(t), slog.Default())

	mux := http.NewServeMux()
	srv.RegisterHandlers(mux)
	return srv, mux
}

func authorizeURL(clientID, redirectURI, challenge, state string) string {
	v := url.Values{}
	v.Set("response_type", "code")
	v.Set("client_id", clientID)
	v.Set("redirect_uri", redirectURI)
	v.Set("code_challenge", challenge)
	v.Set("code_challenge_method", "S256")
	v.Set("state", state)
	return "/oauth/authorize?" + v.Encode()
}

func TestAuthorizeValidRequest(t *testing.T) {
	_, mux := testServer(t)

	u := authorizeURL("chatgpt", "https://chatgpt.com/callback", "test-challenge", "test-state")
	req := httptest.NewRequest(http.MethodGet, u, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !containsAll(body, "<meta", "viewport", "Allow", "Deny", "ChatGPT", "test-state") {
		t.Error("consent page missing required elements")
	}
	// No external resources.
	if contains(body, "<link") || contains(body, "<script src") {
		t.Error("consent page must not have external resources")
	}
}

func TestAuthorizeInvalidClientID(t *testing.T) {
	_, mux := testServer(t)

	u := authorizeURL("unknown", "https://chatgpt.com/callback", "test-challenge", "test-state")
	req := httptest.NewRequest(http.MethodGet, u, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	// Must NOT redirect.
	if rec.Header().Get("Location") != "" {
		t.Error("must not redirect on invalid client_id")
	}
}

func TestAuthorizeRedirectURIMismatch(t *testing.T) {
	_, mux := testServer(t)

	u := authorizeURL("chatgpt", "https://evil.com/callback", "test-challenge", "test-state")
	req := httptest.NewRequest(http.MethodGet, u, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	if rec.Header().Get("Location") != "" {
		t.Error("must not redirect on redirect_uri mismatch")
	}
}

func TestAuthorizeMissingCodeChallenge(t *testing.T) {
	_, mux := testServer(t)

	v := url.Values{}
	v.Set("response_type", "code")
	v.Set("client_id", "chatgpt")
	v.Set("redirect_uri", "https://chatgpt.com/callback")
	v.Set("state", "test-state")
	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+v.Encode(), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected 302 redirect, got %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !contains(loc, "error=invalid_request") {
		t.Errorf("expected error=invalid_request in redirect, got Location: %s", loc)
	}
}

func TestAuthorizePlainMethodRejected(t *testing.T) {
	_, mux := testServer(t)

	v := url.Values{}
	v.Set("response_type", "code")
	v.Set("client_id", "chatgpt")
	v.Set("redirect_uri", "https://chatgpt.com/callback")
	v.Set("code_challenge", "test-challenge")
	v.Set("code_challenge_method", "plain")
	v.Set("state", "test-state")
	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+v.Encode(), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected 302 redirect, got %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !contains(loc, "error=invalid_request") {
		t.Errorf("expected error=invalid_request in redirect, got Location: %s", loc)
	}
}

func TestAuthorizeMissingState(t *testing.T) {
	_, mux := testServer(t)

	v := url.Values{}
	v.Set("response_type", "code")
	v.Set("client_id", "chatgpt")
	v.Set("redirect_uri", "https://chatgpt.com/callback")
	v.Set("code_challenge", "test-challenge")
	v.Set("code_challenge_method", "S256")
	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+v.Encode(), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected 302 redirect, got %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !contains(loc, "error=invalid_request") {
		t.Errorf("expected error=invalid_request in redirect, got Location: %s", loc)
	}
}

func TestAllowFlow(t *testing.T) {
	srv, mux := testServer(t)
	defer srv.codeStore.Stop()

	form := url.Values{}
	form.Set("client_id", "chatgpt")
	form.Set("redirect_uri", "https://chatgpt.com/callback")
	form.Set("code_challenge", "test-challenge")
	form.Set("scope", "mcp")
	form.Set("state", "test-state-123")

	req := httptest.NewRequest(http.MethodPost, "/oauth/authorize/allow", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d: %s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	locURL, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	code := locURL.Query().Get("code")
	if code == "" {
		t.Error("expected code in redirect")
	}
	if len(code) != 64 {
		t.Errorf("expected 64-char hex code, got %d chars", len(code))
	}
	if locURL.Query().Get("state") != "test-state-123" {
		t.Errorf("expected state=test-state-123, got %s", locURL.Query().Get("state"))
	}

	// Verify code is in store.
	ac := srv.codeStore.Consume(code)
	if ac == nil {
		t.Fatal("code not found in store")
	}
	if ac.ClientID != "chatgpt" {
		t.Errorf("expected clientID=chatgpt, got %s", ac.ClientID)
	}
	if ac.CodeChallenge != "test-challenge" {
		t.Errorf("expected codeChallenge=test-challenge, got %s", ac.CodeChallenge)
	}
}

func TestDenyFlow(t *testing.T) {
	_, mux := testServer(t)

	form := url.Values{}
	form.Set("client_id", "chatgpt")
	form.Set("redirect_uri", "https://chatgpt.com/callback")
	form.Set("state", "test-state-deny")

	req := httptest.NewRequest(http.MethodPost, "/oauth/authorize/deny", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	locURL, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if locURL.Query().Get("error") != "access_denied" {
		t.Errorf("expected error=access_denied, got %s", locURL.Query().Get("error"))
	}
	if locURL.Query().Get("state") != "test-state-deny" {
		t.Errorf("expected state=test-state-deny, got %s", locURL.Query().Get("state"))
	}
}

// ── Token Endpoint Tests ───────────────────────────────────────────────────

// pkceChallenge computes the S256 code_challenge for a given verifier.
func pkceChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// issueCode stores an auth code in the server's code store and returns it.
func issueCode(t *testing.T, srv *OAuthServer, clientID, redirectURI, challenge, scope string) string {
	t.Helper()
	code, err := GenerateCode()
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}
	now := time.Now().UTC()
	srv.codeStore.Store(&authCode{
		Code:          code,
		ClientID:      clientID,
		RedirectURI:   redirectURI,
		CodeChallenge: challenge,
		Scope:         scope,
		SourceName:    "default",
		IssuedAt:      now,
		ExpiresAt:     now.Add(srv.config.AuthCodeTTL),
	})
	return code
}

func TestTokenValidFlow(t *testing.T) {
	srv, mux := testServer(t)
	defer srv.codeStore.Stop()

	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	challenge := pkceChallenge(verifier)
	code := issueCode(t, srv, "chatgpt", "https://chatgpt.com/callback", challenge, "mcp")

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", "chatgpt")
	form.Set("redirect_uri", "https://chatgpt.com/callback")
	form.Set("code", code)
	form.Set("code_verifier", verifier)

	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", ct)
	}

	var resp tokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal token response: %v", err)
	}
	if resp.AccessToken == "" {
		t.Error("access_token is empty")
	}
	if resp.TokenType != "Bearer" {
		t.Errorf("expected token_type=Bearer, got %s", resp.TokenType)
	}
	if resp.ExpiresIn != 3600 {
		t.Errorf("expected expires_in=3600, got %d", resp.ExpiresIn)
	}

	// Verify the JWT is valid.
	claims, err := ValidateJWT(resp.AccessToken, srv.PublicKey(), srv.IssuerURL())
	if err != nil {
		t.Fatalf("ValidateJWT: %v", err)
	}
	if claims.Subject != "chatgpt" {
		t.Errorf("expected sub=chatgpt, got %s", claims.Subject)
	}
}

func TestTokenWrongVerifier(t *testing.T) {
	srv, mux := testServer(t)
	defer srv.codeStore.Stop()

	verifier := "correct-verifier-value"
	challenge := pkceChallenge(verifier)
	code := issueCode(t, srv, "chatgpt", "https://chatgpt.com/callback", challenge, "mcp")

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", "chatgpt")
	form.Set("redirect_uri", "https://chatgpt.com/callback")
	form.Set("code", code)
	form.Set("code_verifier", "wrong-verifier-value")

	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	assertTokenError(t, rec, "invalid_grant")
}

func TestTokenExpiredCode(t *testing.T) {
	srv, mux := testServer(t)
	defer srv.codeStore.Stop()

	verifier := "test-verifier"
	challenge := pkceChallenge(verifier)
	code, err := GenerateCode()
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}
	// Store with already-expired timestamp.
	srv.codeStore.Store(&authCode{
		Code:          code,
		ClientID:      "chatgpt",
		RedirectURI:   "https://chatgpt.com/callback",
		CodeChallenge: challenge,
		Scope:         "mcp",
		SourceName:    "default",
		IssuedAt:      time.Now().Add(-10 * time.Minute),
		ExpiresAt:     time.Now().Add(-5 * time.Minute),
	})

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", "chatgpt")
	form.Set("redirect_uri", "https://chatgpt.com/callback")
	form.Set("code", code)
	form.Set("code_verifier", verifier)

	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	assertTokenError(t, rec, "invalid_grant")
}

func TestTokenCodeSingleUse(t *testing.T) {
	srv, mux := testServer(t)
	defer srv.codeStore.Stop()

	verifier := "single-use-verifier"
	challenge := pkceChallenge(verifier)
	code := issueCode(t, srv, "chatgpt", "https://chatgpt.com/callback", challenge, "mcp")

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", "chatgpt")
	form.Set("redirect_uri", "https://chatgpt.com/callback")
	form.Set("code", code)
	form.Set("code_verifier", verifier)

	// First use — should succeed.
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first use: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Second use — must fail.
	req = httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("second use: expected 400, got %d", rec.Code)
	}
	assertTokenError(t, rec, "invalid_grant")
}

func TestTokenWrongClientID(t *testing.T) {
	srv, mux := testServer(t)
	defer srv.codeStore.Stop()

	verifier := "client-id-test"
	challenge := pkceChallenge(verifier)
	code := issueCode(t, srv, "chatgpt", "https://chatgpt.com/callback", challenge, "mcp")

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", "wrong-client")
	form.Set("redirect_uri", "https://chatgpt.com/callback")
	form.Set("code", code)
	form.Set("code_verifier", verifier)

	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	assertTokenError(t, rec, "invalid_client")
}

func TestTokenWrongRedirectURI(t *testing.T) {
	srv, mux := testServer(t)
	defer srv.codeStore.Stop()

	verifier := "redirect-test"
	challenge := pkceChallenge(verifier)
	code := issueCode(t, srv, "chatgpt", "https://chatgpt.com/callback", challenge, "mcp")

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", "chatgpt")
	form.Set("redirect_uri", "https://evil.com/callback")
	form.Set("code", code)
	form.Set("code_verifier", verifier)

	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	assertTokenError(t, rec, "invalid_grant")
}

func TestTokenJWTClaims(t *testing.T) {
	srv, mux := testServer(t)
	defer srv.codeStore.Stop()

	verifier := "claims-test-verifier"
	challenge := pkceChallenge(verifier)
	code := issueCode(t, srv, "chatgpt", "https://chatgpt.com/callback", challenge, "mcp")

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", "chatgpt")
	form.Set("redirect_uri", "https://chatgpt.com/callback")
	form.Set("code", code)
	form.Set("code_verifier", verifier)

	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp tokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	claims, err := ValidateJWT(resp.AccessToken, srv.PublicKey(), srv.IssuerURL())
	if err != nil {
		t.Fatalf("ValidateJWT: %v", err)
	}

	if claims.Issuer != "https://nexus.example.com" {
		t.Errorf("iss: got %q, want https://nexus.example.com", claims.Issuer)
	}
	if claims.Subject != "chatgpt" {
		t.Errorf("sub: got %q, want chatgpt", claims.Subject)
	}
	aud := claims.Audience
	if len(aud) != 1 || aud[0] != Audience {
		t.Errorf("aud: got %v, want [%s]", aud, Audience)
	}
	if claims.ID == "" {
		t.Error("jti is empty")
	}
	if claims.Scope != "mcp" {
		t.Errorf("scope: got %q, want mcp", claims.Scope)
	}
	if claims.BFNSource != "default" {
		t.Errorf("bfn_source: got %q, want default", claims.BFNSource)
	}
	if claims.ExpiresAt == nil {
		t.Fatal("exp is nil")
	}
	if claims.IssuedAt == nil {
		t.Fatal("iat is nil")
	}
}

func TestTokenJWTExpiry(t *testing.T) {
	srv, mux := testServer(t)
	defer srv.codeStore.Stop()

	verifier := "expiry-test-verifier"
	challenge := pkceChallenge(verifier)
	code := issueCode(t, srv, "chatgpt", "https://chatgpt.com/callback", challenge, "mcp")

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", "chatgpt")
	form.Set("redirect_uri", "https://chatgpt.com/callback")
	form.Set("code", code)
	form.Set("code_verifier", verifier)

	before := time.Now().UTC()

	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	after := time.Now().UTC()

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp tokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	claims, err := ValidateJWT(resp.AccessToken, srv.PublicKey(), srv.IssuerURL())
	if err != nil {
		t.Fatalf("ValidateJWT: %v", err)
	}

	// exp should be ~now + 3600s (1 hour).
	// JWT NumericDate truncates to seconds, so truncate bounds too.
	expectedMin := before.Truncate(time.Second).Add(time.Hour)
	expectedMax := after.Add(time.Hour).Add(time.Second)
	exp := claims.ExpiresAt.Time.UTC()
	if exp.Before(expectedMin) || exp.After(expectedMax) {
		t.Errorf("exp=%v, expected between %v and %v", exp, expectedMin, expectedMax)
	}
}

func TestOAuthFullFlow(t *testing.T) {
	srv, mux := testServer(t)
	defer srv.codeStore.Stop()

	verifier := "full-flow-verifier-string-abc123"
	challenge := pkceChallenge(verifier)

	// Step 1: GET /oauth/authorize — renders consent page.
	authURL := authorizeURL("chatgpt", "https://chatgpt.com/callback", challenge, "full-flow-state")
	req := httptest.NewRequest(http.MethodGet, authURL, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("authorize: expected 200, got %d", rec.Code)
	}

	// Step 2: POST /oauth/authorize/allow — issues code.
	form := url.Values{}
	form.Set("client_id", "chatgpt")
	form.Set("redirect_uri", "https://chatgpt.com/callback")
	form.Set("code_challenge", challenge)
	form.Set("scope", "mcp")
	form.Set("state", "full-flow-state")
	req = httptest.NewRequest(http.MethodPost, "/oauth/authorize/allow", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("allow: expected 302, got %d", rec.Code)
	}
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse redirect: %v", err)
	}
	code := loc.Query().Get("code")
	if code == "" {
		t.Fatal("allow: no code in redirect")
	}
	if loc.Query().Get("state") != "full-flow-state" {
		t.Error("allow: state mismatch in redirect")
	}

	// Step 3: POST /oauth/token — exchange code for JWT.
	tokenForm := url.Values{}
	tokenForm.Set("grant_type", "authorization_code")
	tokenForm.Set("client_id", "chatgpt")
	tokenForm.Set("redirect_uri", "https://chatgpt.com/callback")
	tokenForm.Set("code", code)
	tokenForm.Set("code_verifier", verifier)
	req = httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(tokenForm.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("token: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp tokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal token: %v", err)
	}
	if resp.AccessToken == "" {
		t.Fatal("token: access_token is empty")
	}

	// Step 4: Validate the JWT.
	claims, err := ValidateJWT(resp.AccessToken, srv.PublicKey(), srv.IssuerURL())
	if err != nil {
		t.Fatalf("ValidateJWT: %v", err)
	}
	if claims.Subject != "chatgpt" {
		t.Errorf("expected sub=chatgpt, got %s", claims.Subject)
	}
	if claims.BFNSource != "default" {
		t.Errorf("expected bfn_source=default, got %s", claims.BFNSource)
	}
	if claims.Scope != "mcp" {
		t.Errorf("expected scope=mcp, got %s", claims.Scope)
	}
}

// assertTokenError checks that a token endpoint response contains the expected error code.
func assertTokenError(t *testing.T, rec *httptest.ResponseRecorder, expectedError string) {
	t.Helper()
	var errResp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}
	if errResp["error"] != expectedError {
		t.Errorf("expected error=%s, got %s", expectedError, errResp["error"])
	}
}

// ── CodeStore Tests ─────────────────────────────────────────────────────────

func TestCodeStoreExpiry(t *testing.T) {
	cs := NewCodeStore()
	defer cs.Stop()

	ac := &authCode{
		Code:      "expired-code",
		ClientID:  "chatgpt",
		ExpiresAt: time.Now().Add(-1 * time.Minute), // already expired
	}
	cs.Store(ac)

	result := cs.Consume("expired-code")
	if result != nil {
		t.Error("expected nil for expired code")
	}
}

func TestCodeStorePurge(t *testing.T) {
	cs := NewCodeStore()
	defer cs.Stop()

	// Store an expired code.
	ac := &authCode{
		Code:      "purge-me",
		ClientID:  "chatgpt",
		ExpiresAt: time.Now().Add(-1 * time.Minute),
	}
	cs.Store(ac)

	if cs.Len() != 1 {
		t.Fatalf("expected 1 code before purge, got %d", cs.Len())
	}

	cs.purge()

	if cs.Len() != 0 {
		t.Errorf("expected 0 codes after purge, got %d", cs.Len())
	}
}

func TestCodeStoreConcurrent(t *testing.T) {
	cs := NewCodeStore()
	defer cs.Stop()

	const n = 100
	var wg sync.WaitGroup
	wg.Add(n * 2)

	// Concurrent writes.
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			code := fmt.Sprintf("code-%d", i)
			cs.Store(&authCode{
				Code:      code,
				ClientID:  "chatgpt",
				ExpiresAt: time.Now().Add(5 * time.Minute),
			})
		}(i)
	}

	// Concurrent reads.
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			code := fmt.Sprintf("code-%d", i)
			cs.Consume(code) // may or may not find it — race-free is the point
		}(i)
	}

	wg.Wait()
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

func containsAll(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}

func mustGenKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := GenerateRSAKey()
	if err != nil {
		t.Fatalf("GenerateRSAKey: %v", err)
	}
	return key
}

// ── JWKS Endpoint Tests (Phase OA-7) ───────────────────────────────────────

func TestJWKSEndpoint(t *testing.T) {
	srv := NewOAuthServer(OAuthConfig{
		IssuerURL: "https://nexus.example.com",
	}, mustGenKey(t), slog.Default())

	mux := http.NewServeMux()
	srv.RegisterHandlers(mux)

	req := httptest.NewRequest(http.MethodGet, "/oauth/jwks", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", ct)
	}

	var resp JWKSResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal JWKS: %v", err)
	}
	if len(resp.Keys) != 1 {
		t.Fatalf("expected 1 key in JWKS, got %d", len(resp.Keys))
	}
}

func TestJWKSFields(t *testing.T) {
	key := mustGenKey(t)
	srv := NewOAuthServer(OAuthConfig{
		IssuerURL: "https://nexus.example.com",
	}, key, slog.Default())

	mux := http.NewServeMux()
	srv.RegisterHandlers(mux)

	req := httptest.NewRequest(http.MethodGet, "/oauth/jwks", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var resp JWKSResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal JWKS: %v", err)
	}

	jwk := resp.Keys[0]
	if jwk.Kty != "RSA" {
		t.Errorf("kty: got %q, want RSA", jwk.Kty)
	}
	if jwk.Use != "sig" {
		t.Errorf("use: got %q, want sig", jwk.Use)
	}
	if jwk.Alg != "RS256" {
		t.Errorf("alg: got %q, want RS256", jwk.Alg)
	}
	if jwk.Kid != KeyID {
		t.Errorf("kid: got %q, want %s", jwk.Kid, KeyID)
	}
	if jwk.N == "" {
		t.Error("n (modulus) is empty")
	}
	if jwk.E == "" {
		t.Error("e (exponent) is empty")
	}

	// Verify base64url encoding without padding.
	for _, field := range []struct {
		name, val string
	}{{"n", jwk.N}, {"e", jwk.E}} {
		if strings.ContainsRune(field.val, '=') {
			t.Errorf("%s contains padding '=' characters", field.name)
		}
		if strings.ContainsRune(field.val, '+') || strings.ContainsRune(field.val, '/') {
			t.Errorf("%s uses base64 instead of base64url encoding", field.name)
		}
	}
}

func TestJWKSMethodNotAllowed(t *testing.T) {
	srv := NewOAuthServer(OAuthConfig{
		IssuerURL: "https://nexus.example.com",
	}, mustGenKey(t), slog.Default())

	mux := http.NewServeMux()
	srv.RegisterHandlers(mux)

	req := httptest.NewRequest(http.MethodPost, "/oauth/jwks", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for POST, got %d", rec.Code)
	}
}

// ── Backward Compatibility + Concurrent Session Tests (Phase OA-7) ─────────

func TestMCPAuthenticate_OAuthDisabled_JWTRejected(t *testing.T) {
	// When OAuthServer is nil (OAuth disabled), ValidateAccessToken is unreachable.
	// This test verifies a server with a different issuer rejects tokens not matching.
	key := mustGenKey(t)
	srv := NewOAuthServer(OAuthConfig{
		Enabled:   true,
		IssuerURL: "https://nexus.example.com",
	}, key, slog.Default())

	// Sign a valid token but for a different issuer — simulates "OAuth disabled" scenario
	// where the token doesn't match the server's expected issuer.
	otherKey := mustGenKey(t)
	tokenStr, err := SignJWT(otherKey, "https://other.example.com", "chatgpt", "mcp", "default", time.Hour)
	if err != nil {
		t.Fatalf("SignJWT: %v", err)
	}

	if srv.ValidateAccessToken(tokenStr) {
		t.Fatal("expected ValidateAccessToken to reject JWT from different key/issuer")
	}
}

func TestBackwardCompatibility_StaticKey(t *testing.T) {
	// Verify that the OAuth server's existence does not interfere with
	// the static bfn_mcp_ key path. This is a unit test of ValidateAccessToken:
	// a bfn_mcp_ prefixed string must NOT validate as a JWT.
	key := mustGenKey(t)
	srv := NewOAuthServer(OAuthConfig{
		Enabled:   true,
		IssuerURL: "https://nexus.example.com",
	}, key, slog.Default())

	// bfn_mcp_ tokens are not JWTs — ValidateAccessToken must return false.
	if srv.ValidateAccessToken("bfn_mcp_473a2b20795677cab8015b0cf384e7d5c40a48bc47cf276bed586c27db18a057") {
		t.Fatal("ValidateAccessToken must not accept bfn_mcp_ tokens as JWTs")
	}
}

func TestConcurrentSessions(t *testing.T) {
	srv, mux := testServer(t)
	defer srv.codeStore.Stop()

	const n = 10
	var wg sync.WaitGroup
	wg.Add(n)
	errs := make(chan string, n)

	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()

			verifier := fmt.Sprintf("concurrent-verifier-%d", i)
			challenge := pkceChallenge(verifier)

			// Issue code.
			code := issueCode(t, srv, "chatgpt", "https://chatgpt.com/callback", challenge, "mcp")

			// Exchange code for token.
			form := url.Values{}
			form.Set("grant_type", "authorization_code")
			form.Set("client_id", "chatgpt")
			form.Set("redirect_uri", "https://chatgpt.com/callback")
			form.Set("code", code)
			form.Set("code_verifier", verifier)

			req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				errs <- fmt.Sprintf("session %d: expected 200, got %d: %s", i, rec.Code, rec.Body.String())
				return
			}

			var resp tokenResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				errs <- fmt.Sprintf("session %d: unmarshal: %v", i, err)
				return
			}

			// Validate JWT.
			if !srv.ValidateAccessToken(resp.AccessToken) {
				errs <- fmt.Sprintf("session %d: JWT validation failed", i)
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for e := range errs {
		t.Error(e)
	}
}

// ── ValidateAccessToken Tests (Phase OA-5) ──────────────────────────────────

func TestMCPAuthenticate_ValidJWT(t *testing.T) {
	key := mustGenerateKey(t)
	issuer := "https://nexus.example.com"
	srv := NewOAuthServer(OAuthConfig{
		Enabled:        true,
		IssuerURL:      issuer,
		AccessTokenTTL: time.Hour,
		AuthCodeTTL:    5 * time.Minute,
	}, key, slog.Default())

	tokenStr, err := SignJWT(key, issuer, "chatgpt", "mcp", "default", time.Hour)
	if err != nil {
		t.Fatalf("SignJWT: %v", err)
	}

	if !srv.ValidateAccessToken(tokenStr) {
		t.Fatal("expected ValidateAccessToken to return true for valid JWT")
	}
}

func TestMCPAuthenticate_ExpiredJWT(t *testing.T) {
	key := mustGenerateKey(t)
	issuer := "https://nexus.example.com"
	srv := NewOAuthServer(OAuthConfig{
		Enabled:   true,
		IssuerURL: issuer,
	}, key, slog.Default())

	tokenStr, err := SignJWT(key, issuer, "chatgpt", "mcp", "default", -1*time.Hour)
	if err != nil {
		t.Fatalf("SignJWT: %v", err)
	}

	if srv.ValidateAccessToken(tokenStr) {
		t.Fatal("expected ValidateAccessToken to reject expired JWT")
	}
}

func TestMCPAuthenticate_WrongAudJWT(t *testing.T) {
	key := mustGenerateKey(t)
	issuer := "https://nexus.example.com"
	srv := NewOAuthServer(OAuthConfig{
		Enabled:   true,
		IssuerURL: issuer,
	}, key, slog.Default())

	tokenStr, err := signWithCustomAudience(key, issuer, "wrong-audience")
	if err != nil {
		t.Fatalf("signWithCustomAudience: %v", err)
	}

	if srv.ValidateAccessToken(tokenStr) {
		t.Fatal("expected ValidateAccessToken to reject JWT with wrong audience")
	}
}

func TestMCPAuthenticate_WrongIssuJWT(t *testing.T) {
	key := mustGenerateKey(t)
	srv := NewOAuthServer(OAuthConfig{
		Enabled:   true,
		IssuerURL: "https://nexus.example.com",
	}, key, slog.Default())

	tokenStr, err := SignJWT(key, "https://evil.example.com", "chatgpt", "mcp", "default", time.Hour)
	if err != nil {
		t.Fatalf("SignJWT: %v", err)
	}

	if srv.ValidateAccessToken(tokenStr) {
		t.Fatal("expected ValidateAccessToken to reject JWT with wrong issuer")
	}
}

func TestMCPAuthenticate_WrongKeyJWT(t *testing.T) {
	key1 := mustGenerateKey(t)
	key2 := mustGenerateKey(t)
	issuer := "https://nexus.example.com"
	srv := NewOAuthServer(OAuthConfig{
		Enabled:   true,
		IssuerURL: issuer,
	}, key1, slog.Default())

	// Sign with key2, validate with server that has key1.
	tokenStr, err := SignJWT(key2, issuer, "chatgpt", "mcp", "default", time.Hour)
	if err != nil {
		t.Fatalf("SignJWT: %v", err)
	}

	if srv.ValidateAccessToken(tokenStr) {
		t.Fatal("expected ValidateAccessToken to reject JWT signed with wrong key")
	}
}

// mustGenerateKey generates an RSA key pair or fails the test.
func mustGenerateKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := GenerateRSAKey()
	if err != nil {
		t.Fatalf("GenerateRSAKey: %v", err)
	}
	return key
}

// signWithCustomAudience signs a JWT with a non-standard audience for testing.
func signWithCustomAudience(key *rsa.PrivateKey, issuer, audience string) (string, error) {
	return signCustomClaims(key, issuer, audience, time.Hour)
}

func signCustomClaims(key *rsa.PrivateKey, issuer, audience string, ttl time.Duration) (string, error) {
	now := time.Now().UTC()
	claims := nexusClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   "test",
			Audience:  jwt.ClaimStrings{audience},
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        "test-jti",
		},
		Scope:     "mcp",
		BFNSource: "default",
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = KeyID
	return token.SignedString(key)
}
