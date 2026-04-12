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
	"time"

	"github.com/BubbleFish-Nexus/internal/wal"
)

// WALWriter submits audit records to the WAL as EntryTypeAudit entries.
// This gives audit records the same kill-9 durability as data writes.
// The existing JSONL audit logger continues to operate independently as
// a tail-follower for SIEM integrations.
type WALWriter struct {
	w *wal.WAL
}

// NewWALWriter creates a WALWriter backed by the given WAL instance.
func NewWALWriter(w *wal.WAL) *WALWriter {
	return &WALWriter{w: w}
}

// Submit writes an InteractionRecord to the WAL as an audit entry.
// Returns nil on success. Callers should treat errors as non-fatal
// (log WARN, do not fail the HTTP request).
func (aw *WALWriter) Submit(record InteractionRecord) error {
	if record.RecordID == "" {
		record.RecordID = NewRecordID()
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
