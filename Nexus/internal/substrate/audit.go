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

// Substrate audit event types and emission functions. These compose with the
// existing Phase 4 hash-chained audit log via ChainState.Extend().
//
// Reference: v0.1.3 BF-Sketch Substrate Build Plan, Section 5.2.
package substrate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/BubbleFish-Nexus/internal/provenance"
)

// Substrate audit event types. Added to the Phase 4 audit event taxonomy.
const (
	EventRatchetInitialized = "substrate.ratchet_initialized"
	EventRatchetAdvanced    = "substrate.ratchet_advanced"
	EventSketchWritten      = "substrate.sketch_written"
	EventMemoryShredded     = "substrate.memory_shredded"
	EventCuckooRebuild      = "substrate.cuckoo_rebuild"
	EventDeletionProof      = "substrate.deletion_proof_issued"
)

// AuditEntry is a generic substrate audit event that composes with the
// Phase 4 hash chain via ChainState.Extend().
type AuditEntry struct {
	EventType string          `json:"event_type"`
	Timestamp time.Time       `json:"timestamp"`
	Payload   json.RawMessage `json:"payload"`
	PrevHash  string          `json:"prev_hash"`
	Hash      string          `json:"hash"`
}

// RatchetAdvancedPayload is the structured payload for a ratchet-advanced event.
type RatchetAdvancedPayload struct {
	OldStateID uint32    `json:"old_state_id"`
	NewStateID uint32    `json:"new_state_id"`
	Reason     string    `json:"reason"`
	Timestamp  time.Time `json:"timestamp"`
}

// SketchWrittenPayload records a sketch write.
type SketchWrittenPayload struct {
	MemoryID     string    `json:"memory_id"`
	StateID      uint32    `json:"state_id"`
	SketchHash   string    `json:"sketch_hash"` // hex SHA-256 of sketch bytes
	CanonicalDim uint32    `json:"canonical_dim"`
	Timestamp    time.Time `json:"timestamp"`
}

// MemoryShreddedPayload records a shred-seed deletion.
type MemoryShreddedPayload struct {
	MemoryID            string    `json:"memory_id"`
	OldStateID          uint32    `json:"old_state_id"`
	NewStateID          uint32    `json:"new_state_id"`
	CuckooRemoved       bool      `json:"cuckoo_removed"`
	CanonicalRowDeleted bool      `json:"canonical_row_deleted"`
	Timestamp           time.Time `json:"timestamp"`
}

// CuckooRebuildPayload records a cuckoo filter rebuild.
type CuckooRebuildPayload struct {
	Reason      string        `json:"reason"`
	MemoryCount uint          `json:"memory_count"`
	Duration    time.Duration `json:"duration_ns"`
	Timestamp   time.Time     `json:"timestamp"`
}

// SubstrateAuditLog wraps the Phase 4 ChainState to emit substrate events.
// Nil-safe: all methods are no-ops when the chain is nil (audit disabled).
type SubstrateAuditLog struct {
	chain *provenance.ChainState
}

// NewSubstrateAuditLog creates a substrate audit log that composes with
// the given Phase 4 chain state. Pass nil to disable audit logging.
func NewSubstrateAuditLog(chain *provenance.ChainState) *SubstrateAuditLog {
	return &SubstrateAuditLog{chain: chain}
}

// Emit appends a substrate event to the Phase 4 hash chain.
// Returns the entry with PrevHash and Hash populated.
func (l *SubstrateAuditLog) Emit(eventType string, payload interface{}) (*AuditEntry, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	entry := &AuditEntry{
		EventType: eventType,
		Timestamp: time.Now().UTC(),
		Payload:   payloadBytes,
	}

	// Compose with Phase 4 hash chain
	if l.chain != nil {
		entryBytes, err := json.Marshal(entry)
		if err != nil {
			return nil, err
		}
		prevHash, hash := l.chain.Extend(entryBytes)
		entry.PrevHash = prevHash
		entry.Hash = hash
	}

	return entry, nil
}

// EmitRatchetAdvanced logs a ratchet advance event.
func (l *SubstrateAuditLog) EmitRatchetAdvanced(oldStateID, newStateID uint32, reason string) (*AuditEntry, error) {
	return l.Emit(EventRatchetAdvanced, RatchetAdvancedPayload{
		OldStateID: oldStateID,
		NewStateID: newStateID,
		Reason:     reason,
		Timestamp:  time.Now().UTC(),
	})
}

// EmitSketchWritten logs a sketch write event.
func (l *SubstrateAuditLog) EmitSketchWritten(memoryID string, stateID uint32, sketchBytes []byte, canonicalDim uint32) (*AuditEntry, error) {
	h := sha256.Sum256(sketchBytes)
	return l.Emit(EventSketchWritten, SketchWrittenPayload{
		MemoryID:     memoryID,
		StateID:      stateID,
		SketchHash:   hex.EncodeToString(h[:]),
		CanonicalDim: canonicalDim,
		Timestamp:    time.Now().UTC(),
	})
}

// EmitMemoryShredded logs a shred-seed deletion event.
func (l *SubstrateAuditLog) EmitMemoryShredded(memoryID string, oldStateID, newStateID uint32, cuckooRemoved, rowDeleted bool) (*AuditEntry, error) {
	return l.Emit(EventMemoryShredded, MemoryShreddedPayload{
		MemoryID:            memoryID,
		OldStateID:          oldStateID,
		NewStateID:          newStateID,
		CuckooRemoved:       cuckooRemoved,
		CanonicalRowDeleted: rowDeleted,
		Timestamp:           time.Now().UTC(),
	})
}

// EmitCuckooRebuild logs a cuckoo filter rebuild event.
func (l *SubstrateAuditLog) EmitCuckooRebuild(reason string, memoryCount uint, duration time.Duration) (*AuditEntry, error) {
	return l.Emit(EventCuckooRebuild, CuckooRebuildPayload{
		Reason:      reason,
		MemoryCount: memoryCount,
		Duration:    duration,
		Timestamp:   time.Now().UTC(),
	})
}
