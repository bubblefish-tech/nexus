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

package governance

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/BubbleFish-Nexus/internal/a2a"
	_ "modernc.org/sqlite"
)

func newTestGrantStore(t *testing.T) *GrantStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gov_test.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := MigrateGrants(db); err != nil {
		t.Fatalf("MigrateGrants: %v", err)
	}
	return NewGrantStore(db)
}

func makeGrant(t *testing.T) *Grant {
	t.Helper()
	return &Grant{
		GrantID:        a2a.NewGrantID(),
		SourceAgentID:  "agent-source",
		TargetAgentID:  "agent-target",
		CapabilityGlob: "memory.read",
		Scope:          "SCOPED",
		Decision:       "allow",
		IssuedBy:       "admin",
		IssuedAt:       time.Now().UTC(),
		Notes:          "test grant",
	}
}

func TestGrantStore_CreateAndGet(t *testing.T) {
	s := newTestGrantStore(t)
	g := makeGrant(t)

	if err := s.CreateGrant(g); err != nil {
		t.Fatalf("CreateGrant: %v", err)
	}

	got, err := s.GetGrant(g.GrantID)
	if err != nil {
		t.Fatalf("GetGrant: %v", err)
	}
	if got.GrantID != g.GrantID {
		t.Errorf("GrantID = %q, want %q", got.GrantID, g.GrantID)
	}
	if got.SourceAgentID != g.SourceAgentID {
		t.Errorf("SourceAgentID = %q, want %q", got.SourceAgentID, g.SourceAgentID)
	}
	if got.CapabilityGlob != g.CapabilityGlob {
		t.Errorf("CapabilityGlob = %q, want %q", got.CapabilityGlob, g.CapabilityGlob)
	}
	if got.Decision != g.Decision {
		t.Errorf("Decision = %q, want %q", got.Decision, g.Decision)
	}
}

func TestGrantStore_GetNotFound(t *testing.T) {
	s := newTestGrantStore(t)

	_, err := s.GetGrant("gnt_NONEXISTENT0000000000000")
	if err == nil {
		t.Fatal("expected error for non-existent grant")
	}
}

func TestGrantStore_ListGrants(t *testing.T) {
	s := newTestGrantStore(t)

	for i := 0; i < 5; i++ {
		g := makeGrant(t)
		if err := s.CreateGrant(g); err != nil {
			t.Fatalf("CreateGrant(%d): %v", i, err)
		}
	}

	grants, err := s.ListGrants()
	if err != nil {
		t.Fatalf("ListGrants: %v", err)
	}
	if len(grants) != 5 {
		t.Errorf("len(grants) = %d, want 5", len(grants))
	}
}

func TestGrantStore_ListGrants_Empty(t *testing.T) {
	s := newTestGrantStore(t)

	grants, err := s.ListGrants()
	if err != nil {
		t.Fatalf("ListGrants: %v", err)
	}
	if len(grants) != 0 {
		t.Errorf("len(grants) = %d, want 0", len(grants))
	}
}

func TestGrantStore_RevokeGrant(t *testing.T) {
	s := newTestGrantStore(t)
	g := makeGrant(t)

	if err := s.CreateGrant(g); err != nil {
		t.Fatalf("CreateGrant: %v", err)
	}

	now := time.Now().UTC()
	if err := s.RevokeGrant(g.GrantID, now); err != nil {
		t.Fatalf("RevokeGrant: %v", err)
	}

	got, err := s.GetGrant(g.GrantID)
	if err != nil {
		t.Fatalf("GetGrant: %v", err)
	}
	if got.RevokedAt == nil {
		t.Fatal("RevokedAt should not be nil after revocation")
	}
}

func TestGrantStore_RevokeGrant_NotFound(t *testing.T) {
	s := newTestGrantStore(t)

	err := s.RevokeGrant("gnt_NONEXISTENT0000000000000", time.Now())
	if err == nil {
		t.Fatal("expected error for non-existent grant")
	}
}

func TestGrantStore_RevokeGrant_AlreadyRevoked(t *testing.T) {
	s := newTestGrantStore(t)
	g := makeGrant(t)

	if err := s.CreateGrant(g); err != nil {
		t.Fatalf("CreateGrant: %v", err)
	}
	if err := s.RevokeGrant(g.GrantID, time.Now()); err != nil {
		t.Fatalf("RevokeGrant(1): %v", err)
	}

	// Second revocation should fail.
	err := s.RevokeGrant(g.GrantID, time.Now())
	if err == nil {
		t.Fatal("expected error for already-revoked grant")
	}
}

func TestGrantStore_FindMatchingGrants(t *testing.T) {
	s := newTestGrantStore(t)

	g1 := makeGrant(t)
	g1.SourceAgentID = "agent-A"
	g1.TargetAgentID = "agent-B"
	if err := s.CreateGrant(g1); err != nil {
		t.Fatalf("CreateGrant: %v", err)
	}

	g2 := makeGrant(t)
	g2.SourceAgentID = "agent-A"
	g2.TargetAgentID = "agent-C"
	if err := s.CreateGrant(g2); err != nil {
		t.Fatalf("CreateGrant: %v", err)
	}

	grants, err := s.FindMatchingGrants("agent-A", "agent-B")
	if err != nil {
		t.Fatalf("FindMatchingGrants: %v", err)
	}
	if len(grants) != 1 {
		t.Errorf("len(grants) = %d, want 1", len(grants))
	}

	grants, err = s.FindMatchingGrants("agent-A", "agent-C")
	if err != nil {
		t.Fatalf("FindMatchingGrants: %v", err)
	}
	if len(grants) != 1 {
		t.Errorf("len(grants) = %d, want 1", len(grants))
	}

	// No match.
	grants, err = s.FindMatchingGrants("agent-X", "agent-Y")
	if err != nil {
		t.Fatalf("FindMatchingGrants: %v", err)
	}
	if len(grants) != 0 {
		t.Errorf("len(grants) = %d, want 0", len(grants))
	}
}

func TestGrantStore_FindMatchingGrants_ExcludesRevoked(t *testing.T) {
	s := newTestGrantStore(t)

	g := makeGrant(t)
	if err := s.CreateGrant(g); err != nil {
		t.Fatalf("CreateGrant: %v", err)
	}
	if err := s.RevokeGrant(g.GrantID, time.Now()); err != nil {
		t.Fatalf("RevokeGrant: %v", err)
	}

	grants, err := s.FindMatchingGrants(g.SourceAgentID, g.TargetAgentID)
	if err != nil {
		t.Fatalf("FindMatchingGrants: %v", err)
	}
	if len(grants) != 0 {
		t.Errorf("len(grants) = %d, want 0 (revoked grants excluded)", len(grants))
	}
}

func TestGrantStore_WithExpiry(t *testing.T) {
	s := newTestGrantStore(t)

	exp := time.Now().Add(time.Hour).UTC()
	g := makeGrant(t)
	g.ExpiresAt = &exp
	if err := s.CreateGrant(g); err != nil {
		t.Fatalf("CreateGrant: %v", err)
	}

	got, err := s.GetGrant(g.GrantID)
	if err != nil {
		t.Fatalf("GetGrant: %v", err)
	}
	if got.ExpiresAt == nil {
		t.Fatal("ExpiresAt should not be nil")
	}
	// Compare truncated to milliseconds (SQLite stores ms).
	if got.ExpiresAt.UnixMilli() != exp.UnixMilli() {
		t.Errorf("ExpiresAt = %v, want %v", got.ExpiresAt, exp)
	}
}

func TestApprovalStore_CreateAndGet(t *testing.T) {
	s := newTestGrantStore(t)

	ap := &PendingApproval{
		ApprovalID:           a2a.NewApprovalID(),
		TaskID:               a2a.NewTaskID(),
		SourceAgentID:        "agent-A",
		TargetAgentID:        "agent-B",
		Skill:                "echo",
		RequiredCapabilities: []string{"memory.read", "memory.write"},
		InputPreview:         "test input",
		CreatedAt:            time.Now().UTC(),
	}

	if err := s.CreateApproval(ap); err != nil {
		t.Fatalf("CreateApproval: %v", err)
	}

	got, err := s.GetApproval(ap.ApprovalID)
	if err != nil {
		t.Fatalf("GetApproval: %v", err)
	}
	if got.ApprovalID != ap.ApprovalID {
		t.Errorf("ApprovalID = %q, want %q", got.ApprovalID, ap.ApprovalID)
	}
	if got.Skill != "echo" {
		t.Errorf("Skill = %q, want %q", got.Skill, "echo")
	}
	if len(got.RequiredCapabilities) != 2 {
		t.Errorf("len(RequiredCapabilities) = %d, want 2", len(got.RequiredCapabilities))
	}
}

func TestApprovalStore_GetNotFound(t *testing.T) {
	s := newTestGrantStore(t)

	_, err := s.GetApproval("apr_NONEXISTENT0000000000000")
	if err == nil {
		t.Fatal("expected error for non-existent approval")
	}
}

func TestApprovalStore_ListPending(t *testing.T) {
	s := newTestGrantStore(t)

	for i := 0; i < 3; i++ {
		ap := &PendingApproval{
			ApprovalID:    a2a.NewApprovalID(),
			TaskID:        a2a.NewTaskID(),
			SourceAgentID: "agent-A",
			TargetAgentID: "agent-B",
			Skill:         "echo",
			CreatedAt:     time.Now().UTC(),
		}
		if err := s.CreateApproval(ap); err != nil {
			t.Fatalf("CreateApproval(%d): %v", i, err)
		}
	}

	pending, err := s.ListPendingApprovals()
	if err != nil {
		t.Fatalf("ListPendingApprovals: %v", err)
	}
	if len(pending) != 3 {
		t.Errorf("len(pending) = %d, want 3", len(pending))
	}
}

func TestApprovalStore_Resolve(t *testing.T) {
	s := newTestGrantStore(t)

	ap := &PendingApproval{
		ApprovalID:    a2a.NewApprovalID(),
		TaskID:        a2a.NewTaskID(),
		SourceAgentID: "agent-A",
		TargetAgentID: "agent-B",
		Skill:         "echo",
		CreatedAt:     time.Now().UTC(),
	}
	if err := s.CreateApproval(ap); err != nil {
		t.Fatalf("CreateApproval: %v", err)
	}

	now := time.Now().UTC()
	if err := s.ResolveApproval(ap.ApprovalID, "admin", "approved", now); err != nil {
		t.Fatalf("ResolveApproval: %v", err)
	}

	got, err := s.GetApproval(ap.ApprovalID)
	if err != nil {
		t.Fatalf("GetApproval: %v", err)
	}
	if got.Resolution != "approved" {
		t.Errorf("Resolution = %q, want %q", got.Resolution, "approved")
	}
	if got.ResolvedBy != "admin" {
		t.Errorf("ResolvedBy = %q, want %q", got.ResolvedBy, "admin")
	}
	if got.ResolvedAt == nil {
		t.Fatal("ResolvedAt should not be nil after resolution")
	}

	// Resolved approvals should not appear in pending list.
	pending, err := s.ListPendingApprovals()
	if err != nil {
		t.Fatalf("ListPendingApprovals: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("len(pending) = %d, want 0", len(pending))
	}
}

func TestApprovalStore_ResolveNotFound(t *testing.T) {
	s := newTestGrantStore(t)

	err := s.ResolveApproval("apr_NONEXISTENT0000000000000", "admin", "approved", time.Now())
	if err == nil {
		t.Fatal("expected error for non-existent approval")
	}
}

func TestApprovalStore_ResolveAlreadyResolved(t *testing.T) {
	s := newTestGrantStore(t)

	ap := &PendingApproval{
		ApprovalID:    a2a.NewApprovalID(),
		TaskID:        a2a.NewTaskID(),
		SourceAgentID: "agent-A",
		TargetAgentID: "agent-B",
		Skill:         "echo",
		CreatedAt:     time.Now().UTC(),
	}
	if err := s.CreateApproval(ap); err != nil {
		t.Fatalf("CreateApproval: %v", err)
	}
	if err := s.ResolveApproval(ap.ApprovalID, "admin", "approved", time.Now()); err != nil {
		t.Fatalf("ResolveApproval(1): %v", err)
	}

	err := s.ResolveApproval(ap.ApprovalID, "admin", "denied", time.Now())
	if err == nil {
		t.Fatal("expected error for already-resolved approval")
	}
}

func TestMigrateGrants_Idempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()

	if err := MigrateGrants(db); err != nil {
		t.Fatalf("MigrateGrants(1): %v", err)
	}
	if err := MigrateGrants(db); err != nil {
		t.Fatalf("MigrateGrants(2): %v", err)
	}
}
