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

package provenance

import (
	"testing"
	"time"
)

func TestContentHash_Deterministic(t *testing.T) {
	h1 := ContentHash("hello world")
	h2 := ContentHash("hello world")
	if h1 != h2 {
		t.Errorf("content hash not deterministic: %q != %q", h1, h2)
	}
	if len(h1) != 64 { // SHA-256 = 32 bytes = 64 hex chars
		t.Errorf("content hash length = %d, want 64", len(h1))
	}
}

func TestContentHash_Different(t *testing.T) {
	h1 := ContentHash("hello")
	h2 := ContentHash("world")
	if h1 == h2 {
		t.Error("different content produced the same hash")
	}
}

func TestSignAndVerifyEnvelope(t *testing.T) {
	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	env := SignableEnvelope{
		SourceName:     "agent-a",
		Timestamp:      time.Now().UTC().Format(time.RFC3339Nano),
		IdempotencyKey: "test-key-1",
		ContentHash:    ContentHash("test content"),
	}

	sig, err := SignEnvelope(env, kp.PrivateKey)
	if err != nil {
		t.Fatalf("SignEnvelope: %v", err)
	}
	if sig == "" {
		t.Fatal("empty signature")
	}

	valid, err := VerifyEnvelope(env, sig, kp.PublicKey)
	if err != nil {
		t.Fatalf("VerifyEnvelope: %v", err)
	}
	if !valid {
		t.Error("signature should be valid")
	}
}

func TestVerifyEnvelope_TamperedFields(t *testing.T) {
	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	env := SignableEnvelope{
		SourceName:     "agent-a",
		Timestamp:      "2026-04-12T00:00:00Z",
		IdempotencyKey: "key-1",
		ContentHash:    ContentHash("original content"),
	}
	sig, err := SignEnvelope(env, kp.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		env  SignableEnvelope
	}{
		{
			"tampered source_name",
			SignableEnvelope{SourceName: "agent-b", Timestamp: env.Timestamp, IdempotencyKey: env.IdempotencyKey, ContentHash: env.ContentHash},
		},
		{
			"tampered timestamp",
			SignableEnvelope{SourceName: env.SourceName, Timestamp: "2026-04-13T00:00:00Z", IdempotencyKey: env.IdempotencyKey, ContentHash: env.ContentHash},
		},
		{
			"tampered idempotency_key",
			SignableEnvelope{SourceName: env.SourceName, Timestamp: env.Timestamp, IdempotencyKey: "key-2", ContentHash: env.ContentHash},
		},
		{
			"tampered content_hash",
			SignableEnvelope{SourceName: env.SourceName, Timestamp: env.Timestamp, IdempotencyKey: env.IdempotencyKey, ContentHash: ContentHash("different")},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			valid, err := VerifyEnvelope(tc.env, sig, kp.PublicKey)
			if err != nil {
				t.Fatalf("VerifyEnvelope: %v", err)
			}
			if valid {
				t.Error("tampered envelope should not verify")
			}
		})
	}
}

func TestVerifyEnvelope_WrongKey(t *testing.T) {
	kp1, _ := GenerateKeyPair()
	kp2, _ := GenerateKeyPair()

	env := SignableEnvelope{
		SourceName:     "test",
		Timestamp:      "2026-04-12T00:00:00Z",
		IdempotencyKey: "k",
		ContentHash:    ContentHash("c"),
	}
	sig, _ := SignEnvelope(env, kp1.PrivateKey)

	valid, err := VerifyEnvelope(env, sig, kp2.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if valid {
		t.Error("verification with wrong key should fail")
	}
}

func TestVerifyEnvelope_InvalidHex(t *testing.T) {
	kp, _ := GenerateKeyPair()
	env := SignableEnvelope{SourceName: "x", Timestamp: "t", IdempotencyKey: "k", ContentHash: "h"}
	_, err := VerifyEnvelope(env, "not-hex!", kp.PublicKey)
	if err == nil {
		t.Error("expected error for invalid hex signature")
	}
}

func TestSignEnvelope_UnsignedFieldsEmpty(t *testing.T) {
	// Verify that unsigned entries (empty signature) are a valid no-op state.
	env := SignableEnvelope{}
	if env.SourceName != "" || env.ContentHash != "" {
		t.Error("zero-value envelope should have empty fields")
	}
}
