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
	"encoding/json"
	"time"
)

// ProofBundle is the cryptographic proof of a memory's integrity and
// provenance. It contains the memory, its signature, the audit chain
// from genesis, and the daemon's identity key.
//
// Reference: v0.1.3 Build Plan Phase 4 Subtask 4.6.
type ProofBundle struct {
	// Version is the proof bundle format version. Currently 1.
	Version int `json:"version"`

	// Memory holds the memory record as stored in the destination.
	Memory ProofMemory `json:"memory"`

	// Signature is the hex-encoded Ed25519 signature over the signable
	// envelope. Empty if the source did not have signing enabled.
	Signature string `json:"signature,omitempty"`

	// SignatureAlg is the algorithm used (e.g. "ed25519"). Empty if unsigned.
	SignatureAlg string `json:"signature_alg,omitempty"`

	// SourcePubKey is the hex-encoded Ed25519 public key of the source that
	// signed the write envelope. Empty if unsigned.
	SourcePubKey string `json:"source_pubkey,omitempty"`

	// SigningKeyID is the fingerprint of the signing key. Empty if unsigned.
	SigningKeyID string `json:"signing_key_id,omitempty"`

	// AuditChain is the audit entry chain from genesis to the memory's
	// write event. Each entry links to the previous via prev_hash.
	AuditChain []ChainEntry `json:"audit_chain"`

	// DaemonPubKey is the hex-encoded Ed25519 public key of the daemon.
	DaemonPubKey string `json:"daemon_pubkey"`

	// GenesisEntry is the raw JSON of the genesis entry.
	GenesisEntry json.RawMessage `json:"genesis_entry"`

	// GeneratedAt is when this proof bundle was created.
	GeneratedAt time.Time `json:"generated_at"`
}

// ProofMemory is a simplified memory record for inclusion in proof bundles.
type ProofMemory struct {
	PayloadID      string `json:"payload_id"`
	Source         string `json:"source"`
	Subject        string `json:"subject"`
	Content        string `json:"content"`
	Timestamp      string `json:"timestamp"`
	IdempotencyKey string `json:"idempotency_key"`
	ContentHash    string `json:"content_hash"`
}
