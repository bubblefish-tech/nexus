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

package audit

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	nexuscrypto "github.com/bubblefish-tech/nexus/internal/crypto"
	"github.com/bubblefish-tech/nexus/internal/provenance"
	"github.com/bubblefish-tech/nexus/internal/wal"
)

// WALWriter submits audit records to the WAL as EntryTypeAudit entries.
// This gives audit records the same kill-9 durability as data writes.
// The existing JSONL audit logger continues to operate independently as
// a tail-follower for SIEM integrations.
//
// When a ChainState is configured, each audit record is hash-chained to
// the previous one before WAL append, ensuring the chain extension is in
// the same fsync as the entry itself.
// Reference: v0.1.3 Build Plan Phase 4 Subtask 4.3.
//
// When a PayloadCrypto is configured (via SetEncryption), sensitive payload
// fields are encrypted with AES-256-GCM before WAL append. Chain metadata
// (record_id, prev_hash, timestamp, event_type, hash) stays plaintext for
// selective disclosure. Reference: CU.0.5.
type WALWriter struct {
	w       *wal.WAL
	chain   *provenance.ChainState // nil when hash chain is disabled
	chainMu sync.Mutex             // serializes the LastHash → Extend atomic sequence
	crypto  *PayloadCrypto         // nil when payload encryption is disabled (CU.0.5)
}

// SetEncryption wires audit payload encryption using the master key manager.
// No-op when mkm is nil or disabled.
func (aw *WALWriter) SetEncryption(mkm *nexuscrypto.MasterKeyManager) {
	aw.crypto = NewPayloadCrypto(mkm)
}

// NewWALWriter creates a WALWriter backed by the given WAL instance.
// chain may be nil to disable hash chaining (backward compatible).
func NewWALWriter(w *wal.WAL, chain *provenance.ChainState) *WALWriter {
	return &WALWriter{w: w, chain: chain}
}

// Submit writes an InteractionRecord to the WAL as an audit entry.
// If a ChainState is configured, the record's PrevAuditHash is set before
// marshaling, and the chain is extended atomically.
// If a PayloadCrypto is configured (CU.0.5), sensitive payload fields are
// encrypted before WAL append; chain fields stay plaintext.
// Returns nil on success. Callers should treat errors as non-fatal
// (log WARN, do not fail the HTTP request).
func (aw *WALWriter) Submit(record InteractionRecord) error {
	if record.RecordID == "" {
		record.RecordID = NewRecordID()
	}

	if aw.chain != nil {
		aw.chainMu.Lock()
		record.PrevAuditHash = aw.chain.LastHash()

		// CU.0.5: encrypt payload fields before marshaling (hash covers encrypted blob).
		if aw.crypto != nil {
			if err := encryptInteractionPayload(aw.crypto, &record); err != nil {
				aw.chainMu.Unlock()
				return fmt.Errorf("audit: encrypt interaction payload: %w", err)
			}
		}

		payload, err := json.Marshal(record)
		if err != nil {
			aw.chainMu.Unlock()
			return fmt.Errorf("audit: marshal record for WAL: %w", err)
		}

		aw.chain.Extend(payload)
		aw.chainMu.Unlock()

		entry := wal.Entry{
			PayloadID: fmt.Sprintf("audit-%s", record.RecordID),
			Status:    wal.StatusDelivered,
			Timestamp: time.Now().UTC(),
			Source:    record.Source,
			EntryType: wal.EntryTypeAudit,
			Payload:   payload,
		}
		return aw.w.Append(entry)
	}

	// CU.0.5: encrypt payload fields (no-chain path).
	if aw.crypto != nil {
		if err := encryptInteractionPayload(aw.crypto, &record); err != nil {
			return fmt.Errorf("audit: encrypt interaction payload: %w", err)
		}
	}

	payload, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("audit: marshal record for WAL: %w", err)
	}

	// TODO(monotonic): Timestamp is wall-clock for display. Ordering uses
	// MonotonicSeq assigned by WAL.Append when WithSequence is configured.
	entry := wal.Entry{
		PayloadID: fmt.Sprintf("audit-%s", record.RecordID),
		Status:    wal.StatusDelivered, // audit entries are not queued for delivery
		Timestamp: time.Now().UTC(),
		Source:    record.Source,
		EntryType: wal.EntryTypeAudit,
		Payload:   payload,
	}

	return aw.w.Append(entry)
}

// SubmitControl writes a ControlEventRecord to the WAL as an audit entry.
// If a ChainState is configured, the record is hash-chained into the existing
// audit log (same chain as InteractionRecords) and the record's Hash field is
// set to the SHA-256 of the serialized record before WAL append.
// If a PayloadCrypto is configured (CU.0.5), sensitive payload fields are
// encrypted before the hash is computed — so the hash covers the encrypted
// envelope, enabling chain verification without the decryption key.
// Returns nil on success. Callers should treat errors as non-fatal.
func (aw *WALWriter) SubmitControl(record ControlEventRecord) error {
	if record.RecordID == "" {
		record.RecordID = NewRecordID()
	}
	record.Timestamp = record.Timestamp.UTC()
	if record.Timestamp.IsZero() {
		record.Timestamp = time.Now().UTC()
	}

	if aw.chain != nil {
		aw.chainMu.Lock()
		record.PrevHash = aw.chain.LastHash()

		// CU.0.5: encrypt before computing hash so the hash covers the
		// encrypted envelope (chain verifiable without decryption key).
		if aw.crypto != nil {
			if err := encryptControlPayload(aw.crypto, &record); err != nil {
				aw.chainMu.Unlock()
				return fmt.Errorf("audit: encrypt control payload: %w", err)
			}
		}

		record.Hash = record.ComputeHash()

		payload, err := json.Marshal(record)
		if err != nil {
			aw.chainMu.Unlock()
			return fmt.Errorf("audit: marshal control record for WAL: %w", err)
		}
		aw.chain.Extend(payload)
		aw.chainMu.Unlock()

		entry := wal.Entry{
			PayloadID: fmt.Sprintf("audit-ctrl-%s", record.RecordID),
			Status:    wal.StatusDelivered,
			Timestamp: record.Timestamp,
			EntryType: wal.EntryTypeAudit,
			Payload:   payload,
		}
		return aw.w.Append(entry)
	}

	// CU.0.5: encrypt payload fields (no-chain path).
	if aw.crypto != nil {
		if err := encryptControlPayload(aw.crypto, &record); err != nil {
			return fmt.Errorf("audit: encrypt control payload: %w", err)
		}
	}

	record.Hash = record.ComputeHash()
	payload, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("audit: marshal control record for WAL: %w", err)
	}

	entry := wal.Entry{
		PayloadID: fmt.Sprintf("audit-ctrl-%s", record.RecordID),
		Status:    wal.StatusDelivered,
		Timestamp: record.Timestamp,
		EntryType: wal.EntryTypeAudit,
		Payload:   payload,
	}
	return aw.w.Append(entry)
}
