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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// buildTestProofBundle constructs a valid proof bundle for testing.
func buildTestProofBundle(t *testing.T) *ProofBundle {
	t.Helper()

	// Generate keys.
	daemonKP, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	sourceKP, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	// Build chain state and genesis.
	cs := NewChainState()
	genesisJSON, err := cs.Genesis(daemonKP)
	if err != nil {
		t.Fatal(err)
	}

	// Build audit chain entries.
	chain := make([]ChainEntry, 0, 6)
	genesisH := sha256.Sum256(genesisJSON)
	chain = append(chain, ChainEntry{
		Hash:    hex.EncodeToString(genesisH[:]),
		Payload: genesisJSON,
	})

	for i := 0; i < 5; i++ {
		payload := []byte(fmt.Sprintf(`{"record_id":"r%d","prev_audit_hash":"%s"}`, i, cs.LastHash()))
		prevHash, currentHash := cs.Extend(payload)
		chain = append(chain, ChainEntry{
			Hash:     currentHash,
			PrevHash: prevHash,
			Payload:  payload,
		})
	}

	// Build memory and signature.
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	content := "test memory content"
	env := SignableEnvelope{
		SourceName:     "agent-a",
		Timestamp:      ts,
		IdempotencyKey: "idem-1",
		ContentHash:    ContentHash(content),
	}
	sig, err := SignEnvelope(env, sourceKP.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}

	return &ProofBundle{
		Version: 1,
		Memory: ProofMemory{
			PayloadID:      "mem-001",
			Source:         "agent-a",
			Subject:        "test",
			Content:        content,
			Timestamp:      ts,
			IdempotencyKey: "idem-1",
			ContentHash:    ContentHash(content),
		},
		Signature:    sig,
		SignatureAlg: SignatureAlgEd25519,
		SourcePubKey: hex.EncodeToString(sourceKP.PublicKey),
		SigningKeyID: sourceKP.KeyID,
		AuditChain:   chain,
		DaemonPubKey: hex.EncodeToString(daemonKP.PublicKey),
		GenesisEntry: genesisJSON,
		GeneratedAt:  time.Now().UTC(),
	}
}

func TestVerifyProofBundle_Valid(t *testing.T) {
	bundle := buildTestProofBundle(t)
	result := VerifyProofBundle(bundle)

	if !result.Valid {
		t.Errorf("expected valid, got error: %s — %s", result.ErrorCode, result.ErrorMessage)
	}
	if result.SignatureValid == nil || !*result.SignatureValid {
		t.Error("signature should be valid")
	}
	if !result.ChainValid {
		t.Error("chain should be valid")
	}
	if !result.DaemonKnown {
		t.Error("daemon should be known")
	}
}

func TestVerifyProofBundle_InvalidSignature(t *testing.T) {
	bundle := buildTestProofBundle(t)
	// Tamper with signature.
	bundle.Signature = "deadbeef" + bundle.Signature[8:]

	result := VerifyProofBundle(bundle)
	if result.Valid {
		t.Error("tampered signature should fail")
	}
	if result.ErrorCode != ErrCodeInvalidSignature {
		t.Errorf("error code = %q, want %q", result.ErrorCode, ErrCodeInvalidSignature)
	}
}

func TestVerifyProofBundle_TamperedContent(t *testing.T) {
	bundle := buildTestProofBundle(t)
	// Tamper with content — signature no longer matches.
	bundle.Memory.Content = "tampered content"

	result := VerifyProofBundle(bundle)
	if result.Valid {
		t.Error("tampered content should fail")
	}
}

func TestVerifyProofBundle_ChainMismatch(t *testing.T) {
	bundle := buildTestProofBundle(t)
	if len(bundle.AuditChain) > 3 {
		// Tamper with chain entry.
		bundle.AuditChain[3].Payload = json.RawMessage(`{"tampered":true}`)
	}

	result := VerifyProofBundle(bundle)
	if result.Valid {
		t.Error("tampered chain should fail")
	}
	if result.ErrorCode != ErrCodeChainMismatch {
		t.Errorf("error code = %q, want %q", result.ErrorCode, ErrCodeChainMismatch)
	}
}

func TestVerifyProofBundle_UnknownDaemon(t *testing.T) {
	bundle := buildTestProofBundle(t)
	// Set a different daemon pubkey.
	otherKP, _ := GenerateKeyPair()
	bundle.DaemonPubKey = hex.EncodeToString(otherKP.PublicKey)

	result := VerifyProofBundle(bundle)
	if result.Valid {
		t.Error("mismatched daemon key should fail")
	}
	if result.ErrorCode != ErrCodeUnknownDaemon {
		t.Errorf("error code = %q, want %q", result.ErrorCode, ErrCodeUnknownDaemon)
	}
}

func TestVerifyProofBundle_Unsigned(t *testing.T) {
	bundle := buildTestProofBundle(t)
	// Clear signature fields — this is valid (unsigned entry).
	bundle.Signature = ""
	bundle.SourcePubKey = ""
	bundle.SignatureAlg = ""
	bundle.SigningKeyID = ""

	result := VerifyProofBundle(bundle)
	if !result.Valid {
		t.Errorf("unsigned bundle should be valid, got: %s", result.ErrorMessage)
	}
	if result.SignatureValid != nil {
		t.Error("signature_valid should be nil for unsigned bundles")
	}
}

func TestVerifyProofBundle_LargeChain(t *testing.T) {
	// Test parallel verification with a large chain.
	daemonKP, _ := GenerateKeyPair()
	cs := NewChainState()
	genesisJSON, _ := cs.Genesis(daemonKP)

	const chainLen = 2000
	chain := make([]ChainEntry, 0, chainLen+1)
	h := sha256.Sum256(genesisJSON)
	chain = append(chain, ChainEntry{
		Hash:    hex.EncodeToString(h[:]),
		Payload: genesisJSON,
	})

	for i := 0; i < chainLen; i++ {
		payload := []byte(fmt.Sprintf(`{"record_id":"r%d","prev_audit_hash":"%s"}`, i, cs.LastHash()))
		prevHash, currentHash := cs.Extend(payload)
		chain = append(chain, ChainEntry{
			Hash:     currentHash,
			PrevHash: prevHash,
			Payload:  payload,
		})
	}

	bundle := &ProofBundle{
		Version:      1,
		Memory:       ProofMemory{PayloadID: "test"},
		AuditChain:   chain,
		DaemonPubKey: hex.EncodeToString(daemonKP.PublicKey),
		GenesisEntry: genesisJSON,
		GeneratedAt:  time.Now().UTC(),
	}

	result := VerifyProofBundle(bundle)
	if !result.Valid {
		t.Errorf("large chain should verify: %s", result.ErrorMessage)
	}
}

func TestVerifyProofBundle_UnsupportedVersion(t *testing.T) {
	bundle := &ProofBundle{Version: 99}
	result := VerifyProofBundle(bundle)
	if result.Valid {
		t.Error("unsupported version should fail")
	}
	if result.ErrorCode != ErrCodeInvalidBundle {
		t.Errorf("error code = %q, want %q", result.ErrorCode, ErrCodeInvalidBundle)
	}
}
