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
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

const (
	// SignatureAlgEd25519 is the algorithm identifier stored alongside
	// signatures. Future post-quantum algorithms are pluggable via this field.
	SignatureAlgEd25519 = "ed25519"
)

// SignableEnvelope is the canonical payload whose JSON serialization is
// signed by the source's Ed25519 key. Go's json.Marshal produces sorted
// keys by default, providing canonical ordering.
//
// Reference: v0.1.3 Build Plan Phase 4 Subtask 4.2.
type SignableEnvelope struct {
	SourceName     string `json:"source_name"`
	Timestamp      string `json:"timestamp"`
	IdempotencyKey string `json:"idempotency_key"`
	ContentHash    string `json:"content_hash"`
}

// ContentHash computes SHA-256 of the content string and returns it hex-encoded.
func ContentHash(content string) string {
	h := sha256.Sum256([]byte(content))
	return hex.EncodeToString(h[:])
}

// SignEnvelope signs the canonical JSON of env with the given Ed25519 private
// key. Returns the signature as hex-encoded bytes.
func SignEnvelope(env SignableEnvelope, key ed25519.PrivateKey) (string, error) {
	data, err := json.Marshal(env)
	if err != nil {
		return "", fmt.Errorf("provenance: marshal signable envelope: %w", err)
	}
	sig := ed25519.Sign(key, data)
	return hex.EncodeToString(sig), nil
}

// VerifyEnvelope verifies the hex-encoded signature against the canonical
// JSON of env using the given Ed25519 public key.
func VerifyEnvelope(env SignableEnvelope, signatureHex string, pub ed25519.PublicKey) (bool, error) {
	data, err := json.Marshal(env)
	if err != nil {
		return false, fmt.Errorf("provenance: marshal signable envelope: %w", err)
	}
	sig, err := hex.DecodeString(signatureHex)
	if err != nil {
		return false, fmt.Errorf("provenance: decode signature hex: %w", err)
	}
	return ed25519.Verify(pub, data, sig), nil
}
