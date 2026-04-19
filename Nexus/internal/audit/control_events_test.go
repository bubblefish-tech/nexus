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

package audit_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/bubblefish-tech/nexus/internal/audit"
)

func TestControlEventTypes(t *testing.T) {
	t.Helper()
	types := []string{
		audit.ControlEventGrantCreated,
		audit.ControlEventGrantRevoked,
		audit.ControlEventApprovalRequested,
		audit.ControlEventApprovalDecided,
		audit.ControlEventTaskCreated,
		audit.ControlEventTaskStateChanged,
		audit.ControlEventActionExecuted,
		audit.ControlEventActionDenied,
	}
	seen := map[string]bool{}
	for _, et := range types {
		if et == "" {
			t.Error("empty event type constant")
		}
		if seen[et] {
			t.Errorf("duplicate event type: %s", et)
		}
		seen[et] = true
	}
	if len(seen) != 8 {
		t.Errorf("expected 8 distinct event types, got %d", len(seen))
	}
}

func TestControlEventRecord_JSONRoundTrip(t *testing.T) {
	t.Helper()
	rec := audit.ControlEventRecord{
		RecordID:   "rec-1",
		EventType:  audit.ControlEventGrantCreated,
		Actor:      "admin",
		ActorType:  "admin",
		TargetID:   "grant-abc",
		TargetType: "grant",
		AgentID:    "agent-1",
		Capability: "nexus_write",
		EntityJSON: json.RawMessage(`{"grant_id":"grant-abc"}`),
		Decision:   "allowed",
		Timestamp:  time.Date(2026, 4, 18, 0, 0, 0, 0, time.UTC),
	}

	b, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got audit.ControlEventRecord
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.RecordID != rec.RecordID {
		t.Errorf("RecordID = %q, want %q", got.RecordID, rec.RecordID)
	}
	if got.EventType != rec.EventType {
		t.Errorf("EventType = %q, want %q", got.EventType, rec.EventType)
	}
	if got.AgentID != rec.AgentID {
		t.Errorf("AgentID = %q, want %q", got.AgentID, rec.AgentID)
	}
}

func TestControlEventRecord_ComputeHash(t *testing.T) {
	t.Helper()
	rec := audit.ControlEventRecord{
		RecordID:  "rec-hash-test",
		EventType: audit.ControlEventTaskCreated,
		AgentID:   "agent-2",
		Timestamp: time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC),
	}

	h1 := rec.ComputeHash()
	if h1 == "" {
		t.Fatal("expected non-empty hash")
	}
	if len(h1) != 64 {
		t.Errorf("expected 64-char hex hash, got %d: %s", len(h1), h1)
	}

	// Deterministic: same input → same hash.
	h2 := rec.ComputeHash()
	if h1 != h2 {
		t.Errorf("hash not deterministic: %s != %s", h1, h2)
	}

	// Changing a field changes the hash.
	rec.AgentID = "agent-3"
	h3 := rec.ComputeHash()
	if h1 == h3 {
		t.Error("hash should differ after field change")
	}
}

func TestControlEventRecord_HashExcludesHashField(t *testing.T) {
	t.Helper()
	rec := audit.ControlEventRecord{
		RecordID:  "rec-hash-excl",
		EventType: audit.ControlEventGrantRevoked,
		AgentID:   "agent-x",
		Timestamp: time.Date(2026, 4, 18, 1, 0, 0, 0, time.UTC),
	}

	h1 := rec.ComputeHash()
	rec.Hash = "some-prior-value"
	h2 := rec.ComputeHash()
	if h1 != h2 {
		t.Error("hash should be independent of the Hash field itself")
	}
}

func TestControlEventRecord_PrevHashChaining(t *testing.T) {
	t.Helper()
	rec1 := audit.ControlEventRecord{
		RecordID:  "rec-chain-1",
		EventType: audit.ControlEventGrantCreated,
		AgentID:   "agent-chain",
		Timestamp: time.Date(2026, 4, 18, 2, 0, 0, 0, time.UTC),
	}
	rec1.Hash = rec1.ComputeHash()

	rec2 := audit.ControlEventRecord{
		RecordID:  "rec-chain-2",
		EventType: audit.ControlEventGrantRevoked,
		AgentID:   "agent-chain",
		PrevHash:  rec1.Hash,
		Timestamp: time.Date(2026, 4, 18, 2, 1, 0, 0, time.UTC),
	}
	rec2.Hash = rec2.ComputeHash()

	if rec2.PrevHash != rec1.Hash {
		t.Errorf("PrevHash = %q, want %q", rec2.PrevHash, rec1.Hash)
	}
	if rec1.Hash == rec2.Hash {
		t.Error("consecutive records must have different hashes")
	}
}

func TestControlEventRecord_EntityJSONOptional(t *testing.T) {
	t.Helper()
	rec := audit.ControlEventRecord{
		RecordID:  "rec-no-entity",
		EventType: audit.ControlEventActionDenied,
		AgentID:   "agent-y",
		Timestamp: time.Now().UTC(),
	}

	b, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), `"entity"`) {
		// omitempty should suppress nil/empty entity.
		t.Error("entity field should be omitted when nil")
	}
}

func TestControlEventRecord_AllEventTypesMarshal(t *testing.T) {
	t.Helper()
	types := []string{
		audit.ControlEventGrantCreated,
		audit.ControlEventGrantRevoked,
		audit.ControlEventApprovalRequested,
		audit.ControlEventApprovalDecided,
		audit.ControlEventTaskCreated,
		audit.ControlEventTaskStateChanged,
		audit.ControlEventActionExecuted,
		audit.ControlEventActionDenied,
	}
	for _, et := range types {
		rec := audit.ControlEventRecord{
			RecordID:  "rec-" + et,
			EventType: et,
			AgentID:   "agent-1",
			Timestamp: time.Now().UTC(),
		}
		b, err := json.Marshal(rec)
		if err != nil {
			t.Errorf("marshal %s: %v", et, err)
			continue
		}
		if !strings.Contains(string(b), et) {
			t.Errorf("marshaled %s: event_type not found in JSON", et)
		}
	}
}
