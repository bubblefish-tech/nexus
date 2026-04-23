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

// Package jwtauth implements JWT-based authentication for BubbleFish Nexus
// using JWKS (JSON Web Key Set) validation. This is the "Pattern B: JWT
// Header Mapping (Advanced)" described in Tech Spec Section 6.6.
//
// The middleware extracts a JWT from the Authorization: Bearer header,
// validates it against a cached JWKS endpoint, and maps a configurable
// claim (e.g. "sub") to a Nexus source name.
//
// JWKS is fetched at startup and cached. On validation failure the cache
// is refreshed at most once per minute to handle key rotation.
//
// Reference: Tech Spec Section 6.6, Phase R-20.
package jwtauth

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"log/slog"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Config holds JWT authentication settings.
type Config struct {
	JWKSUrl       string // URL to fetch the JWKS from
	ClaimToSource string // JWT claim name to map to a Nexus source (e.g. "sub")
	Audience      string // Expected "aud" claim (empty = skip audience check)
	Logger        *slog.Logger
}

// Validator validates JWTs against a cached JWKS.
type Validator struct {
	cfg Config

	mu          sync.RWMutex
	keys        map[string]crypto.PublicKey // kid -> public key
	lastRefresh time.Time
	client      *http.Client

	// validationCache caches successful validation results by token hash.
	// Cache hit returns the Result without any crypto or JSON parsing.
	cacheMu sync.RWMutex
	cache   map[[32]byte]cachedValidation
}

type cachedValidation struct {
	result    *Result
	expiresAt time.Time
}

const validationCacheMaxSize = 256
const validationCacheTTL = 60 * time.Second

// New creates a Validator. Call FetchJWKS() to load keys before use.
func New(cfg Config) *Validator {
	return &Validator{
		cfg:    cfg,
		keys:   make(map[string]crypto.PublicKey),
		client: &http.Client{Timeout: 10 * time.Second},
		cache:  make(map[[32]byte]cachedValidation, validationCacheMaxSize),
	}
}

// FetchJWKS fetches the JWKS from the configured URL and caches the keys.
// Returns an error if the fetch or parse fails.
func (v *Validator) FetchJWKS() error {
	resp, err := v.client.Get(v.cfg.JWKSUrl)
	if err != nil {
		return fmt.Errorf("jwtauth: fetch JWKS: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			slog.Default().Debug("close body", "err", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("jwtauth: JWKS endpoint returned %d", resp.StatusCode)
	}

	var jwks jwksResponse
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return fmt.Errorf("jwtauth: decode JWKS: %w", err)
	}

	keys := make(map[string]crypto.PublicKey, len(jwks.Keys))
	for _, k := range jwks.Keys {
		pub, parseErr := parseJWK(k)
		if parseErr != nil {
			v.cfg.Logger.Warn("jwtauth: skipping unparseable JWK",
				"component", "jwtauth",
				"kid", k.Kid,
				"error", parseErr,
			)
			continue
		}
		keys[k.Kid] = pub
	}

	v.mu.Lock()
	v.keys = keys
	v.lastRefresh = time.Now()
	v.mu.Unlock()

	v.cfg.Logger.Info("jwtauth: JWKS loaded",
		"component", "jwtauth",
		"key_count", len(keys),
	)
	return nil
}

// refreshIfNeeded refreshes the JWKS cache if at least 1 minute has elapsed
// since the last refresh. Returns true if a refresh was attempted.
func (v *Validator) refreshIfNeeded() bool {
	v.mu.RLock()
	elapsed := time.Since(v.lastRefresh)
	v.mu.RUnlock()

	if elapsed < time.Minute {
		return false
	}

	if err := v.FetchJWKS(); err != nil {
		v.cfg.Logger.Warn("jwtauth: JWKS refresh failed",
			"component", "jwtauth",
			"error", err,
		)
	}

	// Clear validation cache on key rotation.
	v.cacheMu.Lock()
	v.cache = make(map[[32]byte]cachedValidation, validationCacheMaxSize)
	v.cacheMu.Unlock()

	return true
}

// Result holds the outcome of JWT validation.
type Result struct {
	SourceName string // value of the configured claim
	Claims     map[string]interface{}
}

// Validate parses and validates a raw JWT string. On success it returns the
// mapped source name from the configured claim. On failure it returns an error.
//
// If validation fails due to an unknown key ID, it attempts a JWKS refresh
// (at most once per minute) and retries.
func (v *Validator) Validate(rawToken string) (*Result, error) {
	key := sha256.Sum256([]byte(rawToken))

	// Cache hit path — zero alloc.
	v.cacheMu.RLock()
	if cached, ok := v.cache[key]; ok && time.Now().Before(cached.expiresAt) {
		v.cacheMu.RUnlock()
		return cached.result, nil
	}
	v.cacheMu.RUnlock()

	result, err := v.validateOnce(rawToken)
	if err != nil && errors.Is(err, errUnknownKid) {
		if v.refreshIfNeeded() {
			result, err = v.validateOnce(rawToken)
		}
	}
	if err != nil {
		return nil, err
	}

	// Cache successful validations only.
	ttl := validationCacheTTL
	if claims := result.Claims; claims != nil {
		if exp, ok := claims["exp"].(float64); ok {
			jwtExp := time.Unix(int64(exp), 0)
			if until := time.Until(jwtExp); until < ttl {
				ttl = until
			}
		}
	}
	v.cacheMu.Lock()
	if len(v.cache) >= validationCacheMaxSize {
		// Evict expired entries; if still full, clear all (simple LRU).
		now := time.Now()
		for k, c := range v.cache {
			if now.After(c.expiresAt) {
				delete(v.cache, k)
			}
		}
		if len(v.cache) >= validationCacheMaxSize {
			v.cache = make(map[[32]byte]cachedValidation, validationCacheMaxSize)
		}
	}
	v.cache[key] = cachedValidation{result: result, expiresAt: time.Now().Add(ttl)}
	v.cacheMu.Unlock()

	return result, nil
}

var errUnknownKid = errors.New("unknown key ID")

func (v *Validator) validateOnce(rawToken string) (*Result, error) {
	parts := strings.SplitN(rawToken, ".", 3)
	if len(parts) != 3 {
		return nil, fmt.Errorf("jwtauth: malformed token: expected 3 parts, got %d", len(parts))
	}

	// Decode header.
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("jwtauth: decode header: %w", err)
	}
	var header jwtHeader
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return nil, fmt.Errorf("jwtauth: parse header: %w", err)
	}

	// Look up key.
	v.mu.RLock()
	key, ok := v.keys[header.Kid]
	v.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("jwtauth: %w: %q", errUnknownKid, header.Kid)
	}

	// Verify signature.
	sigInput := parts[0] + "." + parts[1]
	sigBytes, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("jwtauth: decode signature: %w", err)
	}

	if err := verifySignature(header.Alg, key, []byte(sigInput), sigBytes); err != nil {
		return nil, fmt.Errorf("jwtauth: signature verification failed: %w", err)
	}

	// Decode claims.
	claimBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("jwtauth: decode claims: %w", err)
	}
	var claims map[string]interface{}
	if err := json.Unmarshal(claimBytes, &claims); err != nil {
		return nil, fmt.Errorf("jwtauth: parse claims: %w", err)
	}

	// Check expiration.
	if exp, ok := claims["exp"].(float64); ok {
		if time.Now().Unix() > int64(exp) {
			return nil, fmt.Errorf("jwtauth: token expired")
		}
	}

	// Check audience if configured.
	if v.cfg.Audience != "" {
		if !audienceMatches(claims, v.cfg.Audience) {
			return nil, fmt.Errorf("jwtauth: audience mismatch")
		}
	}

	// Extract source claim.
	claimKey := v.cfg.ClaimToSource
	if claimKey == "" {
		claimKey = "sub"
	}
	sourceVal, ok := claims[claimKey]
	if !ok {
		return nil, fmt.Errorf("jwtauth: claim %q not found in token", claimKey)
	}
	sourceName, ok := sourceVal.(string)
	if !ok || sourceName == "" {
		return nil, fmt.Errorf("jwtauth: claim %q is not a non-empty string", claimKey)
	}

	return &Result{
		SourceName: sourceName,
		Claims:     claims,
	}, nil
}

// audienceMatches checks if the "aud" claim contains the expected audience.
func audienceMatches(claims map[string]interface{}, expected string) bool {
	aud, ok := claims["aud"]
	if !ok {
		return false
	}
	switch v := aud.(type) {
	case string:
		return v == expected
	case []interface{}:
		for _, a := range v {
			if s, ok := a.(string); ok && s == expected {
				return true
			}
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// JWKS types and parsing
// ---------------------------------------------------------------------------

type jwksResponse struct {
	Keys []jwkKey `json:"keys"`
}

type jwkKey struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	N   string `json:"n"`   // RSA modulus
	E   string `json:"e"`   // RSA exponent
	Crv string `json:"crv"` // EC curve
	X   string `json:"x"`   // EC x coordinate
	Y   string `json:"y"`   // EC y coordinate
}

type jwtHeader struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	Typ string `json:"typ"`
}

func parseJWK(k jwkKey) (crypto.PublicKey, error) {
	switch k.Kty {
	case "RSA":
		return parseRSAJWK(k)
	case "EC":
		return parseECJWK(k)
	default:
		return nil, fmt.Errorf("unsupported key type %q", k.Kty)
	}
}

func parseRSAJWK(k jwkKey) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, fmt.Errorf("decode RSA n: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, fmt.Errorf("decode RSA e: %w", err)
	}

	n := new(big.Int).SetBytes(nBytes)
	e := new(big.Int).SetBytes(eBytes)

	return &rsa.PublicKey{
		N: n,
		E: int(e.Int64()),
	}, nil
}

func parseECJWK(k jwkKey) (*ecdsa.PublicKey, error) {
	xBytes, err := base64.RawURLEncoding.DecodeString(k.X)
	if err != nil {
		return nil, fmt.Errorf("decode EC x: %w", err)
	}
	yBytes, err := base64.RawURLEncoding.DecodeString(k.Y)
	if err != nil {
		return nil, fmt.Errorf("decode EC y: %w", err)
	}

	var curve elliptic.Curve
	switch k.Crv {
	case "P-256":
		curve = elliptic.P256()
	case "P-384":
		curve = elliptic.P384()
	case "P-521":
		curve = elliptic.P521()
	default:
		return nil, fmt.Errorf("unsupported EC curve %q", k.Crv)
	}

	x := new(big.Int).SetBytes(xBytes)
	y := new(big.Int).SetBytes(yBytes)

	return &ecdsa.PublicKey{
		Curve: curve,
		X:     x,
		Y:     y,
	}, nil
}

// ---------------------------------------------------------------------------
// Signature verification
// ---------------------------------------------------------------------------

func verifySignature(alg string, key crypto.PublicKey, input, sig []byte) error {
	switch alg {
	case "RS256":
		return verifyRSA(key, crypto.SHA256, input, sig)
	case "RS384":
		return verifyRSA(key, crypto.SHA384, input, sig)
	case "RS512":
		return verifyRSA(key, crypto.SHA512, input, sig)
	case "ES256":
		return verifyECDSA(key, sha256.New(), input, sig)
	case "ES384":
		return verifyECDSA(key, sha512.New384(), input, sig)
	case "ES512":
		return verifyECDSA(key, sha512.New(), input, sig)
	default:
		return fmt.Errorf("unsupported algorithm %q", alg)
	}
}

func verifyRSA(key crypto.PublicKey, hash crypto.Hash, input, sig []byte) error {
	rsaKey, ok := key.(*rsa.PublicKey)
	if !ok {
		return fmt.Errorf("key is not RSA")
	}
	h := hash.New()
	h.Write(input)
	return rsa.VerifyPKCS1v15(rsaKey, hash, h.Sum(nil), sig)
}

func verifyECDSA(key crypto.PublicKey, h hash.Hash, input, sig []byte) error {
	ecKey, ok := key.(*ecdsa.PublicKey)
	if !ok {
		return fmt.Errorf("key is not ECDSA")
	}
	h.Write(input)
	digest := h.Sum(nil)

	// ECDSA signature is r || s, each of key size.
	keySize := (ecKey.Curve.Params().BitSize + 7) / 8
	if len(sig) != 2*keySize {
		return fmt.Errorf("invalid ECDSA signature length: got %d, want %d", len(sig), 2*keySize)
	}
	r := new(big.Int).SetBytes(sig[:keySize])
	s := new(big.Int).SetBytes(sig[keySize:])

	if !ecdsa.Verify(ecKey, digest, r, s) {
		return fmt.Errorf("ECDSA signature verification failed")
	}
	return nil
}
