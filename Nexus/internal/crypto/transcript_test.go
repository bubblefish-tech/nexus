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

package crypto

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

func newTranscriptKeys(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate ed25519 key: %v", err)
	}
	return pub, priv
}

func TestBuildTranscript_Basic(t *testing.T) {
	pub, priv := newTranscriptKeys(t)
	ts := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
	params := TranscriptParams{
		Destination: "sqlite-main",
		Namespace:   "ns1",
		Profile:     "balanced",
		Limit:       10,
	}
	results := [][]byte{
		[]byte("result one"),
		[]byte("result two"),
	}

	tr, err := BuildTranscript("what happened yesterday", params, "agent-42", results, "key-1", priv, ts)
	if err != nil {
		t.Fatalf("BuildTranscript: %v", err)
	}

	if tr.Version != transcriptVersion {
		t.Errorf("version = %d, want %d", tr.Version, transcriptVersion)
	}
	if tr.QueryText != "what happened yesterday" {
		t.Errorf("query_text = %q", tr.QueryText)
	}
	if tr.AgentID != "agent-42" {
		t.Errorf("agent_id = %q", tr.AgentID)
	}
	if tr.KeyID != "key-1" {
		t.Errorf("key_id = %q", tr.KeyID)
	}
	if len(tr.ResultHashes) != 2 {
		t.Fatalf("result_hashes length = %d, want 2", len(tr.ResultHashes))
	}
	if tr.MerkleRoot == "" {
		t.Fatal("merkle_root is empty")
	}
	if len(tr.MerkleProofs) != 2 {
		t.Fatalf("merkle_proofs length = %d, want 2", len(tr.MerkleProofs))
	}
	if tr.Signature == "" {
		t.Fatal("signature is empty")
	}

	// Verify signature.
	if err := VerifyTranscript(tr, pub); err != nil {
		t.Fatalf("VerifyTranscript: %v", err)
	}
}

func TestBuildTranscript_NoResults(t *testing.T) {
	pub, priv := newTranscriptKeys(t)
	ts := time.Now()
	params := TranscriptParams{Destination: "d1"}

	tr, err := BuildTranscript("empty query", params, "agent-1", nil, "k1", priv, ts)
	if err != nil {
		t.Fatalf("BuildTranscript: %v", err)
	}
	if len(tr.ResultHashes) != 0 {
		t.Errorf("expected 0 result hashes, got %d", len(tr.ResultHashes))
	}
	if tr.MerkleRoot != "" {
		t.Errorf("expected empty merkle root, got %q", tr.MerkleRoot)
	}
	if err := VerifyTranscript(tr, pub); err != nil {
		t.Fatalf("VerifyTranscript: %v", err)
	}
}

func TestBuildTranscript_SingleResult(t *testing.T) {
	pub, priv := newTranscriptKeys(t)
	ts := time.Now()
	params := TranscriptParams{Destination: "d1"}

	tr, err := BuildTranscript("q", params, "a", [][]byte{[]byte("one")}, "k", priv, ts)
	if err != nil {
		t.Fatalf("BuildTranscript: %v", err)
	}
	if len(tr.ResultHashes) != 1 {
		t.Fatalf("expected 1 result hash, got %d", len(tr.ResultHashes))
	}
	if tr.MerkleRoot == "" {
		t.Error("merkle root should be set for single result")
	}
	// Single result: root == leaf hash.
	if tr.MerkleRoot != tr.ResultHashes[0] {
		t.Errorf("merkle root should equal the single result hash")
	}
	if err := VerifyTranscript(tr, pub); err != nil {
		t.Fatalf("VerifyTranscript: %v", err)
	}
}

func TestVerifyTranscript_WrongKey(t *testing.T) {
	_, priv := newTranscriptKeys(t)
	otherPub, _ := newTranscriptKeys(t)
	ts := time.Now()

	tr, err := BuildTranscript("q", TranscriptParams{Destination: "d"}, "a", [][]byte{[]byte("x")}, "k", priv, ts)
	if err != nil {
		t.Fatalf("BuildTranscript: %v", err)
	}

	if err := VerifyTranscript(tr, otherPub); err != ErrTranscriptBadSig {
		t.Fatalf("expected ErrTranscriptBadSig, got %v", err)
	}
}

func TestVerifyTranscript_TamperedQueryText(t *testing.T) {
	pub, priv := newTranscriptKeys(t)
	ts := time.Now()

	tr, err := BuildTranscript("original", TranscriptParams{Destination: "d"}, "a", nil, "k", priv, ts)
	if err != nil {
		t.Fatalf("BuildTranscript: %v", err)
	}

	tr.QueryText = "tampered"
	if err := VerifyTranscript(tr, pub); err != ErrTranscriptBadSig {
		t.Fatalf("expected ErrTranscriptBadSig for tampered text, got %v", err)
	}
}

func TestVerifyTranscript_TamperedTimestamp(t *testing.T) {
	pub, priv := newTranscriptKeys(t)
	ts := time.Now()

	tr, err := BuildTranscript("q", TranscriptParams{Destination: "d"}, "a", nil, "k", priv, ts)
	if err != nil {
		t.Fatalf("BuildTranscript: %v", err)
	}

	tr.Timestamp = tr.Timestamp.Add(time.Hour)
	if err := VerifyTranscript(tr, pub); err != ErrTranscriptBadSig {
		t.Fatalf("expected ErrTranscriptBadSig for tampered timestamp, got %v", err)
	}
}

func TestVerifyTranscript_TamperedAgentID(t *testing.T) {
	pub, priv := newTranscriptKeys(t)
	ts := time.Now()

	tr, err := BuildTranscript("q", TranscriptParams{Destination: "d"}, "agent-1", nil, "k", priv, ts)
	if err != nil {
		t.Fatalf("BuildTranscript: %v", err)
	}

	tr.AgentID = "agent-evil"
	if err := VerifyTranscript(tr, pub); err != ErrTranscriptBadSig {
		t.Fatalf("expected ErrTranscriptBadSig for tampered agent, got %v", err)
	}
}

func TestVerifyTranscript_TamperedResultHash(t *testing.T) {
	pub, priv := newTranscriptKeys(t)
	ts := time.Now()

	tr, err := BuildTranscript("q", TranscriptParams{Destination: "d"}, "a", [][]byte{[]byte("data")}, "k", priv, ts)
	if err != nil {
		t.Fatalf("BuildTranscript: %v", err)
	}

	tr.ResultHashes[0] = "deadbeef"
	if err := VerifyTranscript(tr, pub); err != ErrTranscriptBadSig {
		t.Fatalf("expected ErrTranscriptBadSig for tampered hash, got %v", err)
	}
}

func TestVerifyTranscript_MissingSignature(t *testing.T) {
	pub, priv := newTranscriptKeys(t)
	ts := time.Now()

	tr, err := BuildTranscript("q", TranscriptParams{Destination: "d"}, "a", nil, "k", priv, ts)
	if err != nil {
		t.Fatalf("BuildTranscript: %v", err)
	}

	tr.Signature = ""
	if err := VerifyTranscript(tr, pub); err != ErrTranscriptNoSignature {
		t.Fatalf("expected ErrTranscriptNoSignature, got %v", err)
	}
}

func TestVerifyTranscript_BadSignatureEncoding(t *testing.T) {
	pub, priv := newTranscriptKeys(t)
	ts := time.Now()

	tr, err := BuildTranscript("q", TranscriptParams{Destination: "d"}, "a", nil, "k", priv, ts)
	if err != nil {
		t.Fatalf("BuildTranscript: %v", err)
	}

	tr.Signature = "not-valid-base64!!!"
	if err := VerifyTranscript(tr, pub); err == nil {
		t.Fatal("expected error for bad signature encoding")
	}
}

func TestVerifyTranscript_NilTranscript(t *testing.T) {
	pub, _ := newTranscriptKeys(t)
	if err := VerifyTranscript(nil, pub); err != ErrTranscriptNoSignature {
		t.Fatalf("expected ErrTranscriptNoSignature for nil, got %v", err)
	}
}

func TestVerifyTranscript_BadPublicKeySize(t *testing.T) {
	_, priv := newTranscriptKeys(t)
	ts := time.Now()

	tr, err := BuildTranscript("q", TranscriptParams{Destination: "d"}, "a", nil, "k", priv, ts)
	if err != nil {
		t.Fatalf("BuildTranscript: %v", err)
	}

	if err := VerifyTranscript(tr, []byte("short")); err != ErrTranscriptBadKey {
		t.Fatalf("expected ErrTranscriptBadKey, got %v", err)
	}
}

func TestBuildTranscript_NilPrivateKey(t *testing.T) {
	_, err := BuildTranscript("q", TranscriptParams{}, "a", nil, "k", nil, time.Now())
	if err != ErrTranscriptNoKey {
		t.Fatalf("expected ErrTranscriptNoKey, got %v", err)
	}
}

func TestBuildTranscript_BadPrivateKeySize(t *testing.T) {
	_, err := BuildTranscript("q", TranscriptParams{}, "a", nil, "k", []byte("short"), time.Now())
	if err != ErrTranscriptBadKey {
		t.Fatalf("expected ErrTranscriptBadKey, got %v", err)
	}
}

func TestTranscriptJSON_Roundtrip(t *testing.T) {
	pub, priv := newTranscriptKeys(t)
	ts := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
	params := TranscriptParams{
		Destination: "d",
		Namespace:   "ns",
		Subject:     "sub",
		Profile:     "balanced",
		Limit:       5,
	}

	tr, err := BuildTranscript("test query", params, "agent-x", [][]byte{
		[]byte("r1"),
		[]byte("r2"),
		[]byte("r3"),
	}, "key-id", priv, ts)
	if err != nil {
		t.Fatalf("BuildTranscript: %v", err)
	}

	data, err := TranscriptJSON(tr)
	if err != nil {
		t.Fatalf("TranscriptJSON: %v", err)
	}

	var decoded Transcript
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if err := VerifyTranscript(&decoded, pub); err != nil {
		t.Fatalf("VerifyTranscript after roundtrip: %v", err)
	}
}

func TestVerifyTranscript_BadVersion(t *testing.T) {
	pub, priv := newTranscriptKeys(t)
	ts := time.Now()

	tr, err := BuildTranscript("q", TranscriptParams{Destination: "d"}, "a", nil, "k", priv, ts)
	if err != nil {
		t.Fatalf("BuildTranscript: %v", err)
	}

	tr.Version = 99
	if err := VerifyTranscript(tr, pub); err != ErrTranscriptBadVersion {
		t.Fatalf("expected ErrTranscriptBadVersion, got %v", err)
	}
}

func TestBuildTranscript_ManyResults(t *testing.T) {
	pub, priv := newTranscriptKeys(t)
	ts := time.Now()

	results := make([][]byte, 17)
	for i := range results {
		results[i] = []byte("result-" + string(rune('A'+i)))
	}

	tr, err := BuildTranscript("q", TranscriptParams{Destination: "d"}, "a", results, "k", priv, ts)
	if err != nil {
		t.Fatalf("BuildTranscript: %v", err)
	}

	if len(tr.ResultHashes) != 17 {
		t.Fatalf("result_hashes = %d, want 17", len(tr.ResultHashes))
	}
	if len(tr.MerkleProofs) != 17 {
		t.Fatalf("merkle_proofs = %d, want 17", len(tr.MerkleProofs))
	}
	if tr.MerkleRoot == "" {
		t.Fatal("merkle root should be set")
	}

	if err := VerifyTranscript(tr, pub); err != nil {
		t.Fatalf("VerifyTranscript: %v", err)
	}
}

func TestBuildTranscript_TimestampUTC(t *testing.T) {
	_, priv := newTranscriptKeys(t)
	loc, _ := time.LoadLocation("America/New_York")
	ts := time.Date(2026, 4, 25, 12, 0, 0, 0, loc)

	tr, err := BuildTranscript("q", TranscriptParams{Destination: "d"}, "a", nil, "k", priv, ts)
	if err != nil {
		t.Fatalf("BuildTranscript: %v", err)
	}

	if tr.Timestamp.Location() != time.UTC {
		t.Errorf("timestamp location = %v, want UTC", tr.Timestamp.Location())
	}
}

func TestTranscript_SignatureIsValidBase64(t *testing.T) {
	_, priv := newTranscriptKeys(t)
	ts := time.Now()

	tr, err := BuildTranscript("q", TranscriptParams{Destination: "d"}, "a", nil, "k", priv, ts)
	if err != nil {
		t.Fatalf("BuildTranscript: %v", err)
	}

	decoded, err := base64.StdEncoding.DecodeString(tr.Signature)
	if err != nil {
		t.Fatalf("signature is not valid base64: %v", err)
	}
	if len(decoded) != ed25519.SignatureSize {
		t.Errorf("signature size = %d, want %d", len(decoded), ed25519.SignatureSize)
	}
}

func TestMerkleRoot_Empty(t *testing.T) {
	root := merkleRoot(nil)
	if root != nil {
		t.Errorf("expected nil root for empty leaves, got %x", root)
	}
}

func TestMerkleRoot_Deterministic(t *testing.T) {
	leaves := [][]byte{
		{0x01, 0x02},
		{0x03, 0x04},
		{0x05, 0x06},
	}
	root1 := merkleRoot(leaves)
	root2 := merkleRoot(leaves)
	if string(root1) != string(root2) {
		t.Errorf("merkle root is not deterministic")
	}
}
