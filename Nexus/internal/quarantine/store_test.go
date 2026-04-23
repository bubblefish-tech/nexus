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

package quarantine_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/bubblefish-tech/nexus/internal/quarantine"
)

func openTestStore(t *testing.T) *quarantine.Store {
	t.Helper()
	s, err := quarantine.New(filepath.Join(t.TempDir(), "quarantine.db"))
	if err != nil {
		t.Fatalf("quarantine.New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func makeRecord(id, payloadID, source, rule string) quarantine.Record {
	return quarantine.Record{
		ID:                id,
		OriginalPayloadID: payloadID,
		Content:           "test content",
		MetadataJSON:      "{}",
		SourceName:        source,
		AgentID:           "",
		QuarantineReason:  "rule triggered",
		RuleID:            rule,
		QuarantinedAtMs:   time.Now().UnixMilli(),
	}
}

func TestInsert_Get_RoundTrip(t *testing.T) {
	s := openTestStore(t)
	rec := makeRecord("qtn_001", "pay_001", "src-a", "T0-001")
	if err := s.Insert(rec); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	got, err := s.Get("qtn_001")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.RuleID != "T0-001" {
		t.Errorf("RuleID: got %q, want T0-001", got.RuleID)
	}
	if got.ReviewAction != nil {
		t.Errorf("new record should have nil ReviewAction")
	}
}

func TestGet_NotFound(t *testing.T) {
	s := openTestStore(t)
	_, err := s.Get("qtn_missing")
	if err != quarantine.ErrNotFound {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestList_UnreviewedOnly(t *testing.T) {
	s := openTestStore(t)
	for i, id := range []string{"qtn_a", "qtn_b", "qtn_c"} {
		rec := makeRecord(id, "pay_"+id, "src", "T0-001")
		rec.QuarantinedAtMs = time.Now().UnixMilli() - int64(i*1000)
		if err := s.Insert(rec); err != nil {
			t.Fatalf("Insert %s: %v", id, err)
		}
	}
	// Decide one
	if err := s.Decide("qtn_a", quarantine.ReviewActionApproved, "admin"); err != nil {
		t.Fatalf("Decide: %v", err)
	}
	recs, err := s.List(quarantine.ListFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(recs) != 2 {
		t.Errorf("want 2 unreviewed, got %d", len(recs))
	}
}

func TestList_IncludeReviewed(t *testing.T) {
	s := openTestStore(t)
	for _, id := range []string{"qtn_x", "qtn_y"} {
		if err := s.Insert(makeRecord(id, "pay_"+id, "src", "T0-002")); err != nil {
			t.Fatalf("Insert: %v", err)
		}
	}
	if err := s.Decide("qtn_x", quarantine.ReviewActionRejected, "admin"); err != nil {
		t.Fatalf("Decide: %v", err)
	}
	recs, err := s.List(quarantine.ListFilter{IncludeReviewed: true})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(recs) != 2 {
		t.Errorf("want 2 total, got %d", len(recs))
	}
}

func TestList_FilterBySource(t *testing.T) {
	s := openTestStore(t)
	_ = s.Insert(makeRecord("qtn_s1", "p1", "source-alpha", "T0-001"))
	_ = s.Insert(makeRecord("qtn_s2", "p2", "source-beta", "T0-001"))
	recs, err := s.List(quarantine.ListFilter{SourceName: "source-alpha"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(recs) != 1 || recs[0].ID != "qtn_s1" {
		t.Errorf("expected only source-alpha record, got %v", recs)
	}
}

func TestList_LimitDefault(t *testing.T) {
	s := openTestStore(t)
	// Insert 5 records — check limit=0 defaults to 1000 (returns all 5)
	for i := 0; i < 5; i++ {
		id := quarantine.NewID()
		_ = s.Insert(makeRecord(id, "p"+id, "src", "T0-003"))
	}
	recs, err := s.List(quarantine.ListFilter{Limit: 0})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(recs) != 5 {
		t.Errorf("want 5, got %d", len(recs))
	}
}

func TestList_LimitCap(t *testing.T) {
	s := openTestStore(t)
	for i := 0; i < 5; i++ {
		id := quarantine.NewID()
		_ = s.Insert(makeRecord(id, "p"+id, "src", "T0-004"))
	}
	recs, err := s.List(quarantine.ListFilter{Limit: 2})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(recs) != 2 {
		t.Errorf("want 2, got %d", len(recs))
	}
}

func TestDecide_Approve(t *testing.T) {
	s := openTestStore(t)
	_ = s.Insert(makeRecord("qtn_approve", "p1", "src", "T0-001"))
	if err := s.Decide("qtn_approve", quarantine.ReviewActionApproved, "admin-user"); err != nil {
		t.Fatalf("Decide: %v", err)
	}
	rec, err := s.Get("qtn_approve")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if rec.ReviewAction == nil || *rec.ReviewAction != quarantine.ReviewActionApproved {
		t.Errorf("ReviewAction: got %v, want approved", rec.ReviewAction)
	}
	if rec.ReviewedBy == nil || *rec.ReviewedBy != "admin-user" {
		t.Errorf("ReviewedBy: got %v, want admin-user", rec.ReviewedBy)
	}
	if rec.ReviewedAtMs == nil {
		t.Error("ReviewedAtMs should be set")
	}
}

func TestDecide_Reject(t *testing.T) {
	s := openTestStore(t)
	_ = s.Insert(makeRecord("qtn_reject", "p2", "src", "T0-005"))
	if err := s.Decide("qtn_reject", quarantine.ReviewActionRejected, "ops"); err != nil {
		t.Fatalf("Decide: %v", err)
	}
	rec, _ := s.Get("qtn_reject")
	if rec.ReviewAction == nil || *rec.ReviewAction != quarantine.ReviewActionRejected {
		t.Errorf("want rejected, got %v", rec.ReviewAction)
	}
}

func TestDecide_NotFound(t *testing.T) {
	s := openTestStore(t)
	err := s.Decide("qtn_missing", quarantine.ReviewActionApproved, "admin")
	if err != quarantine.ErrNotFound {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestDecide_InvalidAction(t *testing.T) {
	s := openTestStore(t)
	_ = s.Insert(makeRecord("qtn_bad", "p1", "src", "T0-001"))
	err := s.Decide("qtn_bad", "unknown", "admin")
	if err == nil {
		t.Error("expected error for invalid action")
	}
}

func TestNewID_Unique(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := quarantine.NewID()
		if seen[id] {
			t.Fatalf("duplicate ID: %s", id)
		}
		seen[id] = true
	}
}

func TestInsert_DuplicateID(t *testing.T) {
	s := openTestStore(t)
	rec := makeRecord("qtn_dup", "p1", "src", "T0-001")
	_ = s.Insert(rec)
	if err := s.Insert(rec); err == nil {
		t.Error("expected error on duplicate primary key")
	}
}

func TestCount_Empty(t *testing.T) {
	t.Helper()
	s := openTestStore(t)
	total, pending, err := s.Count()
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if total != 0 || pending != 0 {
		t.Errorf("want 0/0, got %d/%d", total, pending)
	}
}

func TestCount_MixedReviewed(t *testing.T) {
	t.Helper()
	s := openTestStore(t)
	_ = s.Insert(makeRecord("qtn_c1", "p1", "src", "T0-001"))
	_ = s.Insert(makeRecord("qtn_c2", "p2", "src", "T0-002"))
	_ = s.Insert(makeRecord("qtn_c3", "p3", "src", "T0-003"))
	_ = s.Decide("qtn_c1", quarantine.ReviewActionApproved, "admin")

	total, pending, err := s.Count()
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if total != 3 {
		t.Errorf("want total 3, got %d", total)
	}
	if pending != 2 {
		t.Errorf("want pending 2, got %d", pending)
	}
}
