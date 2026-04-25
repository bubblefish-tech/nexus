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
	"errors"
	"fmt"
	"time"
)

// Transcript is a signed record of a query execution: query text, parameters,
// timestamp, agent identity, result content hashes, Merkle inclusion proofs,
// and an Ed25519 signature covering the entire transcript.
//
// Reference: Tech Spec MT.11 — Cryptographic Query Transcripts.
type Transcript struct {
	// Version is the transcript format version.
	Version int `json:"version"`

	// QueryText is the original query string.
	QueryText string `json:"query_text"`

	// Parameters holds the query parameters (destination, namespace, etc.).
	Parameters TranscriptParams `json:"parameters"`

	// Timestamp is the time the query was executed (UTC).
	Timestamp time.Time `json:"timestamp"`

	// AgentID identifies the agent that executed the query.
	AgentID string `json:"agent_id"`

	// ResultHashes contains the SHA3-256 content hash of each result record.
	// Each hash is hex-encoded.
	ResultHashes []string `json:"result_hashes"`

	// MerkleRoot is the hex-encoded root hash of the Merkle tree built from
	// ResultHashes. Empty when ResultHashes is empty.
	MerkleRoot string `json:"merkle_root,omitempty"`

	// MerkleProofs contains one Merkle inclusion proof per result, keyed by
	// the result index. Each proof is a list of hex-encoded sibling hashes
	// from leaf to root.
	MerkleProofs map[int][]string `json:"merkle_proofs,omitempty"`

	// KeyID is the opaque identifier for the signing key.
	KeyID string `json:"key_id"`

	// Signature is the base64-encoded Ed25519 signature over the canonical
	// transcript payload (all fields except Signature itself).
	Signature string `json:"signature"`
}

// TranscriptParams holds the query parameters recorded in a transcript.
type TranscriptParams struct {
	Destination string `json:"destination"`
	Namespace   string `json:"namespace,omitempty"`
	Subject     string `json:"subject,omitempty"`
	Profile     string `json:"profile,omitempty"`
	Limit       int    `json:"limit,omitempty"`
}

// transcriptVersion is the current transcript format version.
const transcriptVersion = 1

// Errors returned by transcript operations.
var (
	ErrTranscriptNoKey       = errors.New("crypto: transcript: private key required")
	ErrTranscriptBadKey      = errors.New("crypto: transcript: invalid Ed25519 key size")
	ErrTranscriptNoSignature = errors.New("crypto: transcript: missing signature")
	ErrTranscriptBadSig      = errors.New("crypto: transcript: signature verification failed")
	ErrTranscriptBadVersion  = errors.New("crypto: transcript: unsupported version")
)

// BuildTranscript constructs and signs a cryptographic query transcript.
// The transcript covers the query text, parameters, timestamp, agent identity,
// and SHA3-256 content hashes of each result. An Ed25519 signature is computed
// over the canonical JSON encoding (all fields except "signature").
//
// If results is non-empty, a Merkle tree is built from the result hashes and
// inclusion proofs are generated for each result.
func BuildTranscript(
	queryText string,
	params TranscriptParams,
	agentID string,
	results [][]byte,
	keyID string,
	privKey ed25519.PrivateKey,
	ts time.Time,
) (*Transcript, error) {
	if privKey == nil {
		return nil, ErrTranscriptNoKey
	}
	if len(privKey) != ed25519.PrivateKeySize {
		return nil, ErrTranscriptBadKey
	}

	// Hash each result content using the active crypto profile (SHA3-256).
	resultHashes := make([]string, len(results))
	hashBytes := make([][]byte, len(results))
	for i, r := range results {
		h := ActiveProfile.HashNew()
		h.Write(r)
		sum := h.Sum(nil)
		resultHashes[i] = fmt.Sprintf("%x", sum)
		hashBytes[i] = sum
	}

	t := &Transcript{
		Version:      transcriptVersion,
		QueryText:    queryText,
		Parameters:   params,
		Timestamp:    ts.UTC(),
		AgentID:      agentID,
		ResultHashes: resultHashes,
		KeyID:        keyID,
	}

	// Build Merkle tree and proofs when there are results.
	if len(hashBytes) > 0 {
		root := merkleRoot(hashBytes)
		t.MerkleRoot = fmt.Sprintf("%x", root)
		t.MerkleProofs = make(map[int][]string, len(hashBytes))
		for i := range hashBytes {
			proof := merkleProofForLeaf(hashBytes, i)
			hexProof := make([]string, len(proof))
			for j, p := range proof {
				hexProof[j] = fmt.Sprintf("%x", p)
			}
			t.MerkleProofs[i] = hexProof
		}
	}

	// Sign the transcript.
	payload, err := transcriptSigningPayload(t)
	if err != nil {
		return nil, fmt.Errorf("crypto: transcript: marshal signing payload: %w", err)
	}
	sig := ed25519.Sign(privKey, payload)
	t.Signature = base64.StdEncoding.EncodeToString(sig)

	return t, nil
}

// VerifyTranscript verifies the Ed25519 signature on a transcript using the
// provided public key. Returns nil if the signature is valid.
func VerifyTranscript(t *Transcript, pubKey ed25519.PublicKey) error {
	if t == nil {
		return ErrTranscriptNoSignature
	}
	if len(pubKey) != ed25519.PublicKeySize {
		return ErrTranscriptBadKey
	}
	if t.Version != transcriptVersion {
		return ErrTranscriptBadVersion
	}
	if t.Signature == "" {
		return ErrTranscriptNoSignature
	}

	sigBytes, err := base64.StdEncoding.DecodeString(t.Signature)
	if err != nil {
		return fmt.Errorf("crypto: transcript: decode signature: %w", err)
	}

	payload, err := transcriptSigningPayload(t)
	if err != nil {
		return fmt.Errorf("crypto: transcript: marshal signing payload: %w", err)
	}

	if !ed25519.Verify(pubKey, payload, sigBytes) {
		return ErrTranscriptBadSig
	}
	return nil
}

// TranscriptJSON returns the full transcript as indented JSON.
func TranscriptJSON(t *Transcript) ([]byte, error) {
	return json.MarshalIndent(t, "", "  ")
}

// transcriptSigningPayload returns the canonical JSON bytes that are signed.
// It temporarily zeroes the Signature field to exclude it from the payload.
func transcriptSigningPayload(t *Transcript) ([]byte, error) {
	// Create a copy with Signature blanked out for signing.
	cp := *t
	cp.Signature = ""
	return json.Marshal(&cp)
}

// ---------------------------------------------------------------------------
// Internal Merkle helpers (transcript-local; the shared package-level Merkle
// code lives in merkle_proof.go).
// ---------------------------------------------------------------------------

// merkleRoot computes the Merkle tree root from the given leaf hashes.
// Uses the active crypto profile's hash function.
func merkleRoot(leaves [][]byte) []byte {
	if len(leaves) == 0 {
		return nil
	}
	if len(leaves) == 1 {
		return leaves[0]
	}

	// Build tree bottom-up. If odd number of nodes, duplicate the last.
	level := make([][]byte, len(leaves))
	copy(level, leaves)

	for len(level) > 1 {
		var next [][]byte
		for i := 0; i < len(level); i += 2 {
			if i+1 < len(level) {
				next = append(next, hashPair(level[i], level[i+1]))
			} else {
				next = append(next, hashPair(level[i], level[i]))
			}
		}
		level = next
	}
	return level[0]
}

// merkleProofForLeaf generates the Merkle inclusion proof (sibling hashes
// from leaf to root) for the leaf at the given index.
func merkleProofForLeaf(leaves [][]byte, index int) [][]byte {
	if len(leaves) <= 1 {
		return nil
	}

	level := make([][]byte, len(leaves))
	copy(level, leaves)
	var proof [][]byte
	idx := index

	for len(level) > 1 {
		var next [][]byte
		for i := 0; i < len(level); i += 2 {
			if i+1 < len(level) {
				next = append(next, hashPair(level[i], level[i+1]))
			} else {
				next = append(next, hashPair(level[i], level[i]))
			}
		}

		// Record the sibling hash.
		if idx%2 == 0 {
			if idx+1 < len(level) {
				proof = append(proof, level[idx+1])
			} else {
				proof = append(proof, level[idx])
			}
		} else {
			proof = append(proof, level[idx-1])
		}
		idx /= 2
		level = next
	}
	return proof
}

// hashPair hashes two byte slices together using the active crypto profile.
func hashPair(a, b []byte) []byte {
	h := ActiveProfile.HashNew()
	h.Write(a)
	h.Write(b)
	return h.Sum(nil)
}
