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
	"time"
)

// QueryAttestation is a cryptographic proof that a specific query produced
// a specific result set. The daemon signs the attestation, making every
// query attestable.
//
// Reference: v0.1.3 Build Plan Phase 4 Subtask 4.9.
type QueryAttestation struct {
	QueryHash       string    `json:"query_hash"`
	ResultSetHash   string    `json:"result_set_hash"`
	ResultCount     int       `json:"result_count"`
	DaemonSignature string    `json:"daemon_signature"`
	DaemonPubKey    string    `json:"daemon_pubkey"`
	Timestamp       time.Time `json:"timestamp"`
}

// BuildQueryAttestation creates a signed attestation for a query and its results.
// queryJSON is the raw query parameters. resultPayloads are the serialized result records.
func BuildQueryAttestation(queryJSON []byte, resultPayloads [][]byte, daemonKP *KeyPair) (*QueryAttestation, error) {
	// Hash the query.
	qh := sha256.Sum256(queryJSON)
	queryHash := hex.EncodeToString(qh[:])

	// Hash the result set (concatenation of all result payloads).
	rsh := sha256.New()
	for _, p := range resultPayloads {
		rsh.Write(p)
	}
	resultSetHash := hex.EncodeToString(rsh.Sum(nil))

	// Sign the attestation: sign(queryHash || resultSetHash).
	signable := queryHash + resultSetHash
	sig := ed25519.Sign(daemonKP.PrivateKey, []byte(signable))

	return &QueryAttestation{
		QueryHash:       queryHash,
		ResultSetHash:   resultSetHash,
		ResultCount:     len(resultPayloads),
		DaemonSignature: hex.EncodeToString(sig),
		DaemonPubKey:    hex.EncodeToString(daemonKP.PublicKey),
		Timestamp:       time.Now().UTC(),
	}, nil
}

// VerifyQueryAttestation checks the daemon signature on a query attestation.
func VerifyQueryAttestation(att *QueryAttestation) (bool, error) {
	pubBytes, err := hex.DecodeString(att.DaemonPubKey)
	if err != nil {
		return false, fmt.Errorf("provenance: invalid daemon key hex: %w", err)
	}
	if len(pubBytes) != ed25519.PublicKeySize {
		return false, fmt.Errorf("provenance: invalid daemon key size %d", len(pubBytes))
	}

	sigBytes, err := hex.DecodeString(att.DaemonSignature)
	if err != nil {
		return false, fmt.Errorf("provenance: invalid signature hex: %w", err)
	}

	pub := ed25519.PublicKey(pubBytes)
	signable := att.QueryHash + att.ResultSetHash
	return ed25519.Verify(pub, []byte(signable), sigBytes), nil
}

// MarshalQueryAttestation serializes the attestation to JSON.
func MarshalQueryAttestation(att *QueryAttestation) ([]byte, error) {
	return json.MarshalIndent(att, "", "  ")
}
