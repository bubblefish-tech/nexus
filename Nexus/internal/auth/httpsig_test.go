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

package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"net/http"
	"strings"
	"testing"
	"time"
)

// newTestKeyPair generates a fresh Ed25519 key pair for testing.
func newTestKeyPair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return pub, priv
}

// newTestRequest builds a minimal HTTP request for signing tests.
func newTestRequest(t *testing.T, method, urlStr, body string) *http.Request {
	t.Helper()
	var bodyReader *strings.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	} else {
		bodyReader = strings.NewReader("")
	}
	r, err := http.NewRequest(method, urlStr, bodyReader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	r.Header.Set("Authorization", "Bearer test-token-placeholder")
	return r
}

func TestRoundTrip_SignAndVerify(t *testing.T) {
	pub, priv := newTestKeyPair(t)

	tests := []struct {
		name   string
		method string
		url    string
		body   string
		keyID  string
	}{
		{
			name:   "GET with empty body",
			method: "GET",
			url:    "https://nexus.local/api/v1/memories",
			body:   "",
			keyID:  "agent-1",
		},
		{
			name:   "POST with JSON body",
			method: "POST",
			url:    "https://nexus.local/api/v1/memories",
			body:   `{"key":"value","nested":{"a":1}}`,
			keyID:  "agent-2",
		},
		{
			name:   "PUT with large body",
			method: "PUT",
			url:    "https://nexus.local/api/v1/memories/abc123",
			body:   strings.Repeat("x", 4096),
			keyID:  "agent-3",
		},
		{
			name:   "DELETE request",
			method: "DELETE",
			url:    "https://nexus.local/api/v1/memories/abc123",
			body:   "",
			keyID:  "agent-4",
		},
		{
			name:   "PATCH with body",
			method: "PATCH",
			url:    "https://nexus.local/api/v1/agents/a1",
			body:   `{"status":"active"}`,
			keyID:  "agent-5",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newTestRequest(t, tc.method, tc.url, tc.body)
			body := []byte(tc.body)

			if err := SignRequest(r, tc.keyID, priv, body); err != nil {
				t.Fatalf("SignRequest: %v", err)
			}

			// Verify headers were set.
			if r.Header.Get("Signature") == "" {
				t.Fatal("Signature header not set")
			}
			if r.Header.Get("Signature-Input") == "" {
				t.Fatal("Signature-Input header not set")
			}
			if r.Header.Get("Content-Digest") == "" {
				t.Fatal("Content-Digest header not set")
			}

			if err := VerifyRequest(r, pub, body, tc.keyID, nil); err != nil {
				t.Fatalf("VerifyRequest: %v", err)
			}
		})
	}
}

func TestVerify_TamperedBody(t *testing.T) {
	pub, priv := newTestKeyPair(t)
	body := []byte(`{"important":"data"}`)

	r := newTestRequest(t, "POST", "https://nexus.local/api/v1/memories", string(body))
	if err := SignRequest(r, "agent-1", priv, body); err != nil {
		t.Fatalf("SignRequest: %v", err)
	}

	// Tamper with the body.
	tampered := []byte(`{"important":"TAMPERED"}`)
	err := VerifyRequest(r, pub, tampered, "agent-1", nil)
	if err == nil {
		t.Fatal("expected error for tampered body, got nil")
	}
	if !strings.Contains(err.Error(), "Content-Digest mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestVerify_MissingSignatureHeader(t *testing.T) {
	pub, _ := newTestKeyPair(t)
	r := newTestRequest(t, "GET", "https://nexus.local/api/v1/memories", "")

	// No signature headers at all.
	err := VerifyRequest(r, pub, nil, "", nil)
	if err != ErrNoSignatureInput {
		t.Fatalf("expected ErrNoSignatureInput, got: %v", err)
	}
}

func TestVerify_MissingSignatureValue(t *testing.T) {
	pub, priv := newTestKeyPair(t)
	body := []byte("")
	r := newTestRequest(t, "GET", "https://nexus.local/api/v1/memories", "")

	if err := SignRequest(r, "agent-1", priv, body); err != nil {
		t.Fatalf("SignRequest: %v", err)
	}

	// Remove just the Signature header (keep Signature-Input).
	r.Header.Del("Signature")

	err := VerifyRequest(r, pub, body, "agent-1", nil)
	if err != ErrNoSignature {
		t.Fatalf("expected ErrNoSignature, got: %v", err)
	}
}

func TestVerify_ExpiredSignature(t *testing.T) {
	pub, priv := newTestKeyPair(t)
	body := []byte("")
	r := newTestRequest(t, "GET", "https://nexus.local/api/v1/memories", "")

	if err := SignRequest(r, "agent-1", priv, body); err != nil {
		t.Fatalf("SignRequest: %v", err)
	}

	// Use a time far in the future so the signature is expired.
	future := time.Now().Add(24 * time.Hour)
	opts := &VerifyOptions{
		Now: func() time.Time { return future },
	}

	err := VerifyRequest(r, pub, body, "agent-1", opts)
	if err != ErrExpired {
		t.Fatalf("expected ErrExpired, got: %v", err)
	}
}

func TestVerify_WrongKey(t *testing.T) {
	_, priv := newTestKeyPair(t)
	otherPub, _ := newTestKeyPair(t) // different key pair
	body := []byte(`{"data":"test"}`)

	r := newTestRequest(t, "POST", "https://nexus.local/api/v1/memories", string(body))
	if err := SignRequest(r, "agent-1", priv, body); err != nil {
		t.Fatalf("SignRequest: %v", err)
	}

	err := VerifyRequest(r, otherPub, body, "agent-1", nil)
	if err != ErrBadSignature {
		t.Fatalf("expected ErrBadSignature, got: %v", err)
	}
}

func TestVerify_WrongKeyID(t *testing.T) {
	pub, priv := newTestKeyPair(t)
	body := []byte("")
	r := newTestRequest(t, "GET", "https://nexus.local/api/v1/memories", "")

	if err := SignRequest(r, "agent-1", priv, body); err != nil {
		t.Fatalf("SignRequest: %v", err)
	}

	err := VerifyRequest(r, pub, body, "wrong-agent", nil)
	if err != ErrBadKeyID {
		t.Fatalf("expected ErrBadKeyID, got: %v", err)
	}
}

func TestVerify_NoKeyIDCheck(t *testing.T) {
	pub, priv := newTestKeyPair(t)
	body := []byte("")
	r := newTestRequest(t, "GET", "https://nexus.local/api/v1/memories", "")

	if err := SignRequest(r, "agent-1", priv, body); err != nil {
		t.Fatalf("SignRequest: %v", err)
	}

	// Pass empty expectedKeyID — should skip the key ID check.
	if err := VerifyRequest(r, pub, body, "", nil); err != nil {
		t.Fatalf("VerifyRequest with empty keyID check: %v", err)
	}
}

func TestVerify_TamperedSignature(t *testing.T) {
	pub, priv := newTestKeyPair(t)
	body := []byte(`{"data":"test"}`)

	r := newTestRequest(t, "POST", "https://nexus.local/api/v1/memories", string(body))
	if err := SignRequest(r, "agent-1", priv, body); err != nil {
		t.Fatalf("SignRequest: %v", err)
	}

	// Tamper with the Signature header by flipping a character.
	sig := r.Header.Get("Signature")
	// Replace a character in the base64 portion.
	tampered := strings.Replace(sig, "sig1=:", "sig1=:A", 1)
	if tampered == sig {
		// If no change (unlikely), just prepend an A to the encoded portion.
		tampered = strings.Replace(sig, "=:", "=:A", 1)
	}
	r.Header.Set("Signature", tampered)

	err := VerifyRequest(r, pub, body, "agent-1", nil)
	if err == nil {
		t.Fatal("expected error for tampered signature, got nil")
	}
}

func TestVerify_TamperedMethod(t *testing.T) {
	pub, priv := newTestKeyPair(t)
	body := []byte("")

	r := newTestRequest(t, "GET", "https://nexus.local/api/v1/memories", "")
	if err := SignRequest(r, "agent-1", priv, body); err != nil {
		t.Fatalf("SignRequest: %v", err)
	}

	// Change the method after signing.
	r.Method = "DELETE"

	err := VerifyRequest(r, pub, body, "agent-1", nil)
	if err != ErrBadSignature {
		t.Fatalf("expected ErrBadSignature for method tampering, got: %v", err)
	}
}

func TestVerify_TamperedAuthorizationHeader(t *testing.T) {
	pub, priv := newTestKeyPair(t)
	body := []byte("")

	r := newTestRequest(t, "GET", "https://nexus.local/api/v1/memories", "")
	if err := SignRequest(r, "agent-1", priv, body); err != nil {
		t.Fatalf("SignRequest: %v", err)
	}

	// Change the Authorization header after signing.
	r.Header.Set("Authorization", "Bearer stolen-token")

	err := VerifyRequest(r, pub, body, "agent-1", nil)
	if err != ErrBadSignature {
		t.Fatalf("expected ErrBadSignature for auth header tampering, got: %v", err)
	}
}

func TestVerify_TamperedHost(t *testing.T) {
	pub, priv := newTestKeyPair(t)
	body := []byte("")

	r := newTestRequest(t, "GET", "https://nexus.local/api/v1/memories", "")
	if err := SignRequest(r, "agent-1", priv, body); err != nil {
		t.Fatalf("SignRequest: %v", err)
	}

	// Change the Host after signing.
	r.Host = "evil.example.com"

	err := VerifyRequest(r, pub, body, "agent-1", nil)
	if err != ErrBadSignature {
		t.Fatalf("expected ErrBadSignature for host tampering, got: %v", err)
	}
}

func TestVerify_TamperedPath(t *testing.T) {
	pub, priv := newTestKeyPair(t)
	body := []byte("")

	r := newTestRequest(t, "GET", "https://nexus.local/api/v1/memories", "")
	if err := SignRequest(r, "agent-1", priv, body); err != nil {
		t.Fatalf("SignRequest: %v", err)
	}

	// Change the path after signing.
	r.URL.Path = "/api/v1/admin/delete-all"

	err := VerifyRequest(r, pub, body, "agent-1", nil)
	if err != ErrBadSignature {
		t.Fatalf("expected ErrBadSignature for path tampering, got: %v", err)
	}
}

func TestSignRequest_InvalidKeySize(t *testing.T) {
	r := newTestRequest(t, "GET", "https://nexus.local/api/v1/memories", "")
	err := SignRequest(r, "agent-1", []byte("too-short"), nil)
	if err == nil {
		t.Fatal("expected error for invalid key size, got nil")
	}
	if !strings.Contains(err.Error(), "invalid Ed25519 private key size") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestVerifyRequest_InvalidKeySize(t *testing.T) {
	_, priv := newTestKeyPair(t)
	body := []byte("")
	r := newTestRequest(t, "GET", "https://nexus.local/api/v1/memories", "")

	if err := SignRequest(r, "agent-1", priv, body); err != nil {
		t.Fatalf("SignRequest: %v", err)
	}

	err := VerifyRequest(r, []byte("too-short"), body, "agent-1", nil)
	if err == nil {
		t.Fatal("expected error for invalid key size, got nil")
	}
	if !strings.Contains(err.Error(), "invalid Ed25519 public key size") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestVerify_MaxAgeNegative_SkipsAgeCheck(t *testing.T) {
	pub, priv := newTestKeyPair(t)
	body := []byte("")
	r := newTestRequest(t, "GET", "https://nexus.local/api/v1/memories", "")

	if err := SignRequest(r, "agent-1", priv, body); err != nil {
		t.Fatalf("SignRequest: %v", err)
	}

	// Negative MaxAge disables age checking, even with a far-future Now.
	opts := &VerifyOptions{
		MaxAge: -1,
		Now:    func() time.Time { return time.Now().Add(24 * time.Hour) },
	}

	if err := VerifyRequest(r, pub, body, "agent-1", opts); err != nil {
		t.Fatalf("expected no error with negative MaxAge, got: %v", err)
	}
}

func TestVerify_CustomMaxAge(t *testing.T) {
	pub, priv := newTestKeyPair(t)
	body := []byte("")
	r := newTestRequest(t, "GET", "https://nexus.local/api/v1/memories", "")

	if err := SignRequest(r, "agent-1", priv, body); err != nil {
		t.Fatalf("SignRequest: %v", err)
	}

	// Custom MaxAge of 1 second — signature is valid at creation time.
	opts := &VerifyOptions{
		MaxAge: 1 * time.Second,
		Now:    func() time.Time { return time.Now() },
	}
	if err := VerifyRequest(r, pub, body, "agent-1", opts); err != nil {
		t.Fatalf("expected valid with 1s MaxAge at now, got: %v", err)
	}

	// Same 1s MaxAge but 10 seconds later — should expire.
	opts.Now = func() time.Time { return time.Now().Add(10 * time.Second) }
	err := VerifyRequest(r, pub, body, "agent-1", opts)
	if err != ErrExpired {
		t.Fatalf("expected ErrExpired with 1s MaxAge 10s later, got: %v", err)
	}
}

func TestContentDigest_SetAndVerify(t *testing.T) {
	tests := []struct {
		name string
		body []byte
	}{
		{name: "empty body", body: []byte{}},
		{name: "nil body", body: nil},
		{name: "small body", body: []byte("hello")},
		{name: "JSON body", body: []byte(`{"key":"value"}`)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newTestRequest(t, "POST", "https://nexus.local/api/v1/memories", "")
			SetContentDigest(r, tc.body)

			if r.Header.Get("Content-Digest") == "" {
				t.Fatal("Content-Digest header not set")
			}

			if err := verifyContentDigest(r, tc.body); err != nil {
				t.Fatalf("verifyContentDigest: %v", err)
			}
		})
	}
}

func TestContentDigest_Mismatch(t *testing.T) {
	r := newTestRequest(t, "POST", "https://nexus.local/api/v1/memories", "")
	SetContentDigest(r, []byte("original"))

	err := verifyContentDigest(r, []byte("different"))
	if err == nil {
		t.Fatal("expected error for Content-Digest mismatch, got nil")
	}
}

func TestParseSignatureInput(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantComps  int
		wantKeyID  string
		wantErr    bool
	}{
		{
			name:      "valid input",
			input:     `sig1=("@method" "@target-uri" "@authority");created=1700000000;keyid="agent-1";alg="ed25519"`,
			wantComps: 3,
			wantKeyID: "agent-1",
		},
		{
			name:    "missing label",
			input:   `("@method")`,
			wantErr: true,
		},
		{
			name:      "single component",
			input:     `sig1=("@method");created=1700000000;keyid="test"`,
			wantComps: 1,
			wantKeyID: "test",
		},
		{
			name:      "five components",
			input:     `sig1=("@method" "@target-uri" "@authority" "content-digest" "authorization");created=1700000000;keyid="k5"`,
			wantComps: 5,
			wantKeyID: "k5",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			comps, params, err := parseSignatureInput(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(comps) != tc.wantComps {
				t.Fatalf("got %d components, want %d", len(comps), tc.wantComps)
			}
			if tc.wantKeyID != "" && params.KeyID != tc.wantKeyID {
				t.Fatalf("got keyID %q, want %q", params.KeyID, tc.wantKeyID)
			}
		})
	}
}

func TestParseSignatureValue(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:  "valid",
			input: "sig1=:dGVzdA==:",
		},
		{
			name:    "missing colon",
			input:   "sig1=:dGVzdA==",
			wantErr: true,
		},
		{
			name:    "no equals-colon",
			input:   "sig1dGVzdA==:",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseSignatureValue(tc.input)
			if tc.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestSplitQuoted(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{`"@method" "@target-uri"`, []string{"@method", "@target-uri"}},
		{`"@method"`, []string{"@method"}},
		{`"a" "b" "c" "d" "e"`, []string{"a", "b", "c", "d", "e"}},
		{``, nil},
	}

	for _, tc := range tests {
		got := splitQuoted(tc.input)
		if len(got) != len(tc.want) {
			t.Fatalf("splitQuoted(%q): got %v, want %v", tc.input, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("splitQuoted(%q)[%d]: got %q, want %q", tc.input, i, got[i], tc.want[i])
			}
		}
	}
}

func TestMultipleComponentsCovered(t *testing.T) {
	// Verify all five default components are actually in the signature base.
	pub, priv := newTestKeyPair(t)
	body := []byte(`{"test":"data"}`)
	r := newTestRequest(t, "POST", "https://nexus.local/api/v1/memories", string(body))

	if err := SignRequest(r, "agent-1", priv, body); err != nil {
		t.Fatalf("SignRequest: %v", err)
	}

	sigInput := r.Header.Get("Signature-Input")
	for _, comp := range defaultComponents {
		if !strings.Contains(sigInput, `"`+comp+`"`) {
			t.Errorf("Signature-Input missing component %q: %s", comp, sigInput)
		}
	}

	// Verify passes with correct data.
	if err := VerifyRequest(r, pub, body, "agent-1", nil); err != nil {
		t.Fatalf("VerifyRequest: %v", err)
	}
}

func TestEmptyBody_SignAndVerify(t *testing.T) {
	pub, priv := newTestKeyPair(t)
	r := newTestRequest(t, "GET", "https://nexus.local/api/v1/memories", "")

	if err := SignRequest(r, "agent-1", priv, nil); err != nil {
		t.Fatalf("SignRequest with nil body: %v", err)
	}

	if err := VerifyRequest(r, pub, nil, "agent-1", nil); err != nil {
		t.Fatalf("VerifyRequest with nil body: %v", err)
	}
}

func TestSignatureParams_ExpiresSet(t *testing.T) {
	_, priv := newTestKeyPair(t)
	body := []byte("")
	r := newTestRequest(t, "GET", "https://nexus.local/api/v1/memories", "")

	if err := SignRequest(r, "agent-1", priv, body); err != nil {
		t.Fatalf("SignRequest: %v", err)
	}

	sigInput := r.Header.Get("Signature-Input")
	if !strings.Contains(sigInput, "expires=") {
		t.Fatalf("Signature-Input missing expires parameter: %s", sigInput)
	}
	if !strings.Contains(sigInput, `alg="ed25519"`) {
		t.Fatalf("Signature-Input missing alg parameter: %s", sigInput)
	}
}

func TestVerify_ContentDigestMissing_EmptyBody(t *testing.T) {
	// When Content-Digest is absent and body is empty, verification should
	// still pass (verifyContentDigest returns nil for empty body + no header).
	// We test the verifyContentDigest function directly.
	body := []byte("")
	r := newTestRequest(t, "GET", "https://nexus.local/api/v1/memories", "")

	// No Content-Digest header set, body is empty.
	err := verifyContentDigest(r, body)
	if err != nil {
		t.Fatalf("verifyContentDigest with empty body and no header: %v", err)
	}
}

func TestVerify_ContentDigestMissing_NonEmptyBody(t *testing.T) {
	r := newTestRequest(t, "POST", "https://nexus.local/api/v1/memories", "body")
	// No Content-Digest header set, but body is non-empty.
	err := verifyContentDigest(r, []byte("body"))
	if err == nil {
		t.Fatal("expected error for missing Content-Digest with non-empty body")
	}
}
