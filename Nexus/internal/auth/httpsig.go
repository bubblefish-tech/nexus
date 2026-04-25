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

// Package auth implements RFC 9421 HTTP Message Signatures for
// agent-to-daemon request authentication. Signing uses Ed25519
// (crypto/ed25519 from the standard library). The signed components are:
// @method, @target-uri, @authority, content-digest, and authorization.
//
// Reference: Tech Spec MT.10.
package auth

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// DefaultMaxAge is the maximum allowed age of a signature before it is
// considered expired. Callers can override via VerifyOptions.MaxAge.
const DefaultMaxAge = 5 * time.Minute

// defaultComponents is the ordered list of HTTP message components that
// are covered by every signature produced by SignRequest.
var defaultComponents = []string{
	"@method",
	"@target-uri",
	"@authority",
	"content-digest",
	"authorization",
}

// SignatureParams holds the metadata embedded in the Signature-Input header.
type SignatureParams struct {
	KeyID     string    // opaque key identifier
	Algorithm string    // always "ed25519" for this implementation
	Created   time.Time // signature creation time
	Expires   time.Time // optional expiration (zero value = no expiry)
}

// VerifyOptions controls verification behaviour.
type VerifyOptions struct {
	// MaxAge is the maximum allowed age of a signature. Zero means
	// DefaultMaxAge. Negative means no age check.
	MaxAge time.Duration

	// Now overrides time.Now for testing. If nil, time.Now is used.
	Now func() time.Time
}

func (o *VerifyOptions) now() time.Time {
	if o != nil && o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

func (o *VerifyOptions) maxAge() time.Duration {
	if o == nil || o.MaxAge == 0 {
		return DefaultMaxAge
	}
	return o.MaxAge
}

// Errors returned by verification.
var (
	ErrNoSignature      = errors.New("httpsig: no Signature header")
	ErrNoSignatureInput = errors.New("httpsig: no Signature-Input header")
	ErrBadSignature     = errors.New("httpsig: signature verification failed")
	ErrExpired          = errors.New("httpsig: signature expired")
	ErrMissingComponent = errors.New("httpsig: required component missing from request")
	ErrBadKeyID         = errors.New("httpsig: key ID mismatch")
)

// ---------------------------------------------------------------------------
// Signature-base construction (RFC 9421 Section 2.5)
// ---------------------------------------------------------------------------

// buildSignatureBase constructs the signature base string from the request
// and the given components. The base string is what gets signed/verified.
func buildSignatureBase(r *http.Request, components []string, params SignatureParams) (string, error) {
	var b strings.Builder

	for _, comp := range components {
		val, err := extractComponent(r, comp)
		if err != nil {
			return "", fmt.Errorf("httpsig: component %q: %w", comp, err)
		}
		b.WriteString(fmt.Sprintf("%q: %s\n", comp, val))
	}

	// Append the @signature-params line (no trailing newline).
	b.WriteString("\"@signature-params\": ")
	b.WriteString(formatSignatureParams(components, params))

	return b.String(), nil
}

// extractComponent returns the canonical value for a named HTTP message
// component per RFC 9421 Section 2.
func extractComponent(r *http.Request, name string) (string, error) {
	switch name {
	case "@method":
		return r.Method, nil
	case "@target-uri":
		return targetURI(r), nil
	case "@authority":
		return r.Host, nil
	default:
		// Regular header field (lowercased per RFC 9421 Section 2.1).
		v := r.Header.Get(name)
		if v == "" {
			return "", ErrMissingComponent
		}
		return v, nil
	}
}

// targetURI reconstructs the target URI from the request.
func targetURI(r *http.Request) string {
	if r.URL.IsAbs() {
		return r.URL.String()
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host + r.URL.RequestURI()
}

// formatSignatureParams serialises signature parameters in the structured
// field value syntax defined by RFC 9421 Section 2.3.
func formatSignatureParams(components []string, p SignatureParams) string {
	var b strings.Builder
	b.WriteByte('(')
	for i, c := range components {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(fmt.Sprintf("%q", c))
	}
	b.WriteByte(')')
	b.WriteString(fmt.Sprintf(";created=%d", p.Created.Unix()))
	if !p.Expires.IsZero() {
		b.WriteString(fmt.Sprintf(";expires=%d", p.Expires.Unix()))
	}
	b.WriteString(fmt.Sprintf(";keyid=%q", p.KeyID))
	b.WriteString(";alg=\"ed25519\"")
	return b.String()
}

// ---------------------------------------------------------------------------
// Content-Digest (RFC 9530)
// ---------------------------------------------------------------------------

// SetContentDigest computes SHA-256 over body and sets the Content-Digest
// header on the request. If body is nil or empty the header is set to the
// digest of an empty byte slice.
func SetContentDigest(r *http.Request, body []byte) {
	h := sha256.Sum256(body)
	r.Header.Set("Content-Digest", "sha-256=:"+base64.StdEncoding.EncodeToString(h[:])+":") //nolint:lll
}

// verifyContentDigest checks that the Content-Digest header matches the
// supplied body. Returns nil on match or an error.
func verifyContentDigest(r *http.Request, body []byte) error {
	cd := r.Header.Get("Content-Digest")
	if cd == "" {
		// If there's no content-digest header, treat body as empty and skip.
		if len(body) == 0 {
			return nil
		}
		return fmt.Errorf("httpsig: Content-Digest header missing but body is non-empty")
	}

	// Parse "sha-256=:<base64>:" format.
	prefix := "sha-256=:"
	suffix := ":"
	if !strings.HasPrefix(cd, prefix) || !strings.HasSuffix(cd, suffix) {
		return fmt.Errorf("httpsig: unsupported Content-Digest format")
	}
	encoded := cd[len(prefix) : len(cd)-len(suffix)]
	expected, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return fmt.Errorf("httpsig: decode Content-Digest: %w", err)
	}

	actual := sha256.Sum256(body)
	if subtle.ConstantTimeCompare(expected, actual[:]) != 1 {
		return fmt.Errorf("httpsig: Content-Digest mismatch")
	}
	return nil
}

// ---------------------------------------------------------------------------
// SignRequest
// ---------------------------------------------------------------------------

// SignRequest signs an HTTP request per RFC 9421 using Ed25519. It sets
// the Signature and Signature-Input headers. If a body is provided, the
// Content-Digest header is also set (SHA-256).
//
// The signed components are: @method, @target-uri, @authority,
// content-digest, and authorization.
func SignRequest(r *http.Request, keyID string, privKey ed25519.PrivateKey, body []byte) error {
	if len(privKey) != ed25519.PrivateKeySize {
		return fmt.Errorf("httpsig: invalid Ed25519 private key size: %d", len(privKey))
	}

	// Set Content-Digest if body is provided.
	SetContentDigest(r, body)

	now := time.Now()
	params := SignatureParams{
		KeyID:     keyID,
		Algorithm: "ed25519",
		Created:   now,
		Expires:   now.Add(DefaultMaxAge),
	}

	base, err := buildSignatureBase(r, defaultComponents, params)
	if err != nil {
		return err
	}

	sig := ed25519.Sign(privKey, []byte(base))
	encoded := base64.StdEncoding.EncodeToString(sig)

	sigInput := formatSignatureParams(defaultComponents, params)
	r.Header.Set("Signature-Input", "sig1="+sigInput)
	r.Header.Set("Signature", "sig1=:"+encoded+":")

	return nil
}

// ---------------------------------------------------------------------------
// VerifyRequest
// ---------------------------------------------------------------------------

// VerifyRequest verifies an RFC 9421 HTTP message signature on the
// request using the provided Ed25519 public key. body must match the
// request body for Content-Digest verification.
//
// The expectedKeyID, if non-empty, is checked against the signature's
// keyid parameter using constant-time comparison.
func VerifyRequest(r *http.Request, pubKey ed25519.PublicKey, body []byte, expectedKeyID string, opts *VerifyOptions) error {
	if len(pubKey) != ed25519.PublicKeySize {
		return fmt.Errorf("httpsig: invalid Ed25519 public key size: %d", len(pubKey))
	}

	// Parse Signature-Input.
	sigInputRaw := r.Header.Get("Signature-Input")
	if sigInputRaw == "" {
		return ErrNoSignatureInput
	}

	// Parse Signature.
	sigRaw := r.Header.Get("Signature")
	if sigRaw == "" {
		return ErrNoSignature
	}

	components, params, err := parseSignatureInput(sigInputRaw)
	if err != nil {
		return fmt.Errorf("httpsig: parse Signature-Input: %w", err)
	}

	sigBytes, err := parseSignatureValue(sigRaw)
	if err != nil {
		return fmt.Errorf("httpsig: parse Signature: %w", err)
	}

	// Check key ID if expected.
	if expectedKeyID != "" {
		if subtle.ConstantTimeCompare([]byte(expectedKeyID), []byte(params.KeyID)) != 1 {
			return ErrBadKeyID
		}
	}

	// Check expiration.
	now := opts.now()
	maxAge := opts.maxAge()
	if maxAge > 0 {
		if !params.Expires.IsZero() && now.After(params.Expires) {
			return ErrExpired
		}
		if now.Sub(params.Created) > maxAge {
			return ErrExpired
		}
	}

	// Verify Content-Digest.
	if err := verifyContentDigest(r, body); err != nil {
		return err
	}

	// Rebuild signature base and verify.
	base, err := buildSignatureBase(r, components, params)
	if err != nil {
		return err
	}

	if !ed25519.Verify(pubKey, []byte(base), sigBytes) {
		return ErrBadSignature
	}

	return nil
}

// ---------------------------------------------------------------------------
// Parsing helpers
// ---------------------------------------------------------------------------

// parseSignatureInput parses a Signature-Input header of the form:
//
//	sig1=("@method" "@target-uri" ...);created=T;keyid="K";alg="ed25519"
//
// Returns the component list and the parsed parameters.
func parseSignatureInput(raw string) ([]string, SignatureParams, error) {
	// Strip the label prefix "sig1=".
	raw = strings.TrimSpace(raw)
	eqIdx := strings.Index(raw, "=")
	if eqIdx < 0 {
		return nil, SignatureParams{}, fmt.Errorf("missing label in Signature-Input")
	}
	raw = raw[eqIdx+1:]

	// Parse component list inside parentheses.
	if len(raw) == 0 || raw[0] != '(' {
		return nil, SignatureParams{}, fmt.Errorf("expected '(' in Signature-Input")
	}
	closeIdx := strings.Index(raw, ")")
	if closeIdx < 0 {
		return nil, SignatureParams{}, fmt.Errorf("unterminated component list")
	}
	compStr := raw[1:closeIdx]
	rest := raw[closeIdx+1:]

	// Parse quoted component names.
	var components []string
	for _, part := range splitQuoted(compStr) {
		components = append(components, part)
	}

	// Parse semicolon-delimited parameters.
	var params SignatureParams
	for _, seg := range strings.Split(rest, ";") {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		kv := strings.SplitN(seg, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		val := strings.TrimSpace(kv[1])
		val = strings.Trim(val, "\"")

		switch key {
		case "created":
			ts, err := strconv.ParseInt(val, 10, 64)
			if err != nil {
				return nil, SignatureParams{}, fmt.Errorf("bad created timestamp: %w", err)
			}
			params.Created = time.Unix(ts, 0)
		case "expires":
			ts, err := strconv.ParseInt(val, 10, 64)
			if err != nil {
				return nil, SignatureParams{}, fmt.Errorf("bad expires timestamp: %w", err)
			}
			params.Expires = time.Unix(ts, 0)
		case "keyid":
			params.KeyID = val
		case "alg":
			params.Algorithm = val
		}
	}

	return components, params, nil
}

// parseSignatureValue extracts the binary signature from a header of the
// form: sig1=:<base64>:
func parseSignatureValue(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	eqIdx := strings.Index(raw, "=:")
	if eqIdx < 0 {
		return nil, fmt.Errorf("bad Signature format")
	}
	encoded := raw[eqIdx+2:]
	if !strings.HasSuffix(encoded, ":") {
		return nil, fmt.Errorf("bad Signature format: missing trailing colon")
	}
	encoded = encoded[:len(encoded)-1]
	return base64.StdEncoding.DecodeString(encoded)
}

// splitQuoted splits a space-separated list of double-quoted strings.
// E.g. `"@method" "@target-uri"` -> ["@method", "@target-uri"].
func splitQuoted(s string) []string {
	var result []string
	s = strings.TrimSpace(s)
	for len(s) > 0 {
		if s[0] != '"' {
			// skip whitespace
			s = strings.TrimLeft(s, " ")
			continue
		}
		closeIdx := strings.Index(s[1:], "\"")
		if closeIdx < 0 {
			break
		}
		result = append(result, s[1:1+closeIdx])
		s = s[2+closeIdx:]
	}
	return result
}
