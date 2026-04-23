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

// Package audit implements the AI Interaction Log (Black Box Recorder) for
// BubbleFish Nexus. Every HTTP interaction generates a structured, CRC32-protected
// interaction record appended to a dedicated append-only log file with the same
// durability guarantees as the WAL.
//
// The interaction log is separate from the WAL. The WAL records payload data for
// crash recovery. The interaction log records operational metadata for audit,
// compliance, and forensics.
//
// Reference: Tech Spec Addendum Sections A2.1–A2.7.
package audit

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// InteractionRecord is the schema for one audit entry in the interaction log.
// Every HTTP request reaching the auth layer generates exactly one record.
//
// Reference: Tech Spec Addendum Section A2.2.
type InteractionRecord struct {
	// Identity
	RecordID  string    `json:"record_id"`  // UUID via crypto/rand, unique per interaction
	RequestID string    `json:"request_id"` // Correlation ID from HTTP ingress
	Timestamp time.Time `json:"timestamp"`  // RFC3339Nano, when the interaction started

	// Actor
	Source      string `json:"source"`       // Source name from config
	ActorType   string `json:"actor_type"`   // user, agent, or system
	ActorID     string `json:"actor_id"`     // Identity of the actor
	EffectiveIP string `json:"effective_ip"` // Client IP (GDPR-sensitive — see A2.3)

	// Operation
	OperationType  string `json:"operation_type"`   // write, query, admin
	Endpoint       string `json:"endpoint"`         // e.g. /inbound/claude, /query/sqlite
	HTTPMethod     string `json:"http_method"`      // GET, POST
	HTTPStatusCode int    `json:"http_status_code"` // Response status

	// Write-specific (empty for reads)
	PayloadID            string   `json:"payload_id,omitempty"`
	Destination          string   `json:"destination,omitempty"`
	Subject              string   `json:"subject,omitempty"`
	IdempotencyKey       string   `json:"idempotency_key,omitempty"`
	IsDuplicate          bool     `json:"is_duplicate,omitempty"`
	SensitivityLabelsSet []string `json:"sensitivity_labels_set,omitempty"` // Labels assigned on write

	// Read-specific (empty for writes)
	RetrievalProfile string   `json:"retrieval_profile,omitempty"`
	StagesHit        []string `json:"stages_hit,omitempty"`
	ResultCount      int      `json:"result_count,omitempty"`
	CacheHit         bool     `json:"cache_hit,omitempty"`

	// Policy
	PolicyDecision            string   `json:"policy_decision"`                        // allowed, denied, filtered
	PolicyReason              string   `json:"policy_reason,omitempty"`                // Reason for denial/filtering
	SensitivityLabelsFiltered []string `json:"sensitivity_labels_filtered,omitempty"`  // Labels that caused filtering
	TierFiltered              bool     `json:"tier_filtered,omitempty"`                // True if tier caused filtering

	// Performance
	LatencyMs   float64 `json:"latency_ms"`
	WALAppendMs float64 `json:"wal_append_ms,omitempty"`

	// Integrity — CRC32 computed over JSON with this field set to empty string.
	CRC32 string `json:"crc32"`

	// PrevAuditHash is the SHA-256 hash of the previous audit entry in the
	// hash chain. Empty for the genesis entry. Set by the WAL writer before
	// marshaling to ensure the chain extension is durable in the same fsync.
	// Reference: v0.1.3 Build Plan Phase 4 Subtask 4.3.
	PrevAuditHash string `json:"prev_audit_hash,omitempty"`

	// CU.0.5 — Selective disclosure: when EncryptionVersion=1, PayloadEncrypted
	// holds the AES-256-GCM encrypted JSON of all sensitive operational fields
	// above. Chain fields (RecordID, Timestamp, PrevAuditHash) remain plaintext
	// so the hash chain can be verified without the decryption key.
	PayloadEncrypted  []byte `json:"payload_encrypted,omitempty"`
	EncryptionVersion int    `json:"encryption_version,omitempty"`
}

// NewRecordID generates a cryptographically random UUID for use as record_id.
// Uses crypto/rand; panics on failure (catastrophic — OS entropy exhausted).
func NewRecordID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("audit: crypto/rand.Read failed: %v", err))
	}
	return hex.EncodeToString(b)
}
