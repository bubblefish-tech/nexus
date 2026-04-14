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
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/BubbleFish-Nexus/internal/a2a"
	"github.com/BubbleFish-Nexus/internal/a2a/server"
	_ "modernc.org/sqlite"
)

var frozenTime = time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)

func frozenNow() time.Time { return frozenTime }

func newTestEngine(t *testing.T) (*Engine, *GrantStore) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "engine_test.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := MigrateGrants(db); err != nil {
		t.Fatalf("MigrateGrants: %v", err)
	}
	store := NewGrantStore(db)
	engine := NewEngine(store, WithNowFunc(frozenNow))
	return engine, store
}

func addGrant(t *testing.T, s *GrantStore, source, target, glob, scope, decision string) *Grant {
	t.Helper()
	g := &Grant{
		GrantID:        a2a.NewGrantID(),
		SourceAgentID:  source,
		TargetAgentID:  target,
		CapabilityGlob: glob,
		Scope:          scope,
		Decision:       decision,
		IssuedBy:       "test",
		IssuedAt:       frozenTime.Add(-time.Hour),
	}
	if err := s.CreateGrant(g); err != nil {
		t.Fatalf("CreateGrant: %v", err)
	}
	return g
}

// --- Decide tests ---

func TestDecide_NoCapabilities_Allow(t *testing.T) {
	e, _ := newTestEngine(t)
	ctx := context.Background()

	result, err := e.Decide(ctx, server.GovernanceReq{
		SourceAgentID:        "agent-A",
		TargetAgentID:        "agent-B",
		Skill:                "echo",
		RequiredCapabilities: nil,
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if result.Decision != "allow" {
		t.Errorf("Decision = %q, want %q", result.Decision, "allow")
	}
}

func TestDecide_ExistingAllowGrant_Allow(t *testing.T) {
	e, s := newTestEngine(t)
	ctx := context.Background()

	addGrant(t, s, "agent-A", "agent-B", "memory.read", "SCOPED", "allow")

	result, err := e.Decide(ctx, server.GovernanceReq{
		SourceAgentID:        "agent-A",
		TargetAgentID:        "agent-B",
		Skill:                "echo",
		RequiredCapabilities: []string{"memory.read"},
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if result.Decision != "allow" {
		t.Errorf("Decision = %q, want %q", result.Decision, "allow")
	}
}

func TestDecide_ExistingDenyGrant_Deny(t *testing.T) {
	e, s := newTestEngine(t)
	ctx := context.Background()

	addGrant(t, s, "agent-A", "agent-B", "memory.read", "SCOPED", "deny")

	result, err := e.Decide(ctx, server.GovernanceReq{
		SourceAgentID:        "agent-A",
		TargetAgentID:        "agent-B",
		Skill:                "echo",
		RequiredCapabilities: []string{"memory.read"},
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if result.Decision != "deny" {
		t.Errorf("Decision = %q, want %q", result.Decision, "deny")
	}
}

func TestDecide_NoGrant_AutoAllowDefault_Allow(t *testing.T) {
	e, _ := newTestEngine(t)
	ctx := context.Background()

	// memory.read has default policy auto-allow.
	result, err := e.Decide(ctx, server.GovernanceReq{
		SourceAgentID:        "agent-A",
		TargetAgentID:        "agent-B",
		Skill:                "echo",
		RequiredCapabilities: []string{"memory.read"},
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if result.Decision != "allow" {
		t.Errorf("Decision = %q, want %q", result.Decision, "allow")
	}
}

func TestDecide_NoGrant_ApproveOnceDefault_Escalate(t *testing.T) {
	e, _ := newTestEngine(t)
	ctx := context.Background()

	// memory.write has default policy approve-once-per-scope.
	result, err := e.Decide(ctx, server.GovernanceReq{
		SourceAgentID:        "agent-A",
		TargetAgentID:        "agent-B",
		Skill:                "echo",
		RequiredCapabilities: []string{"memory.write"},
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if result.Decision != "escalate" {
		t.Errorf("Decision = %q, want %q", result.Decision, "escalate")
	}
}

func TestDecide_NoGrant_AlwaysApproveDefault_Escalate(t *testing.T) {
	e, _ := newTestEngine(t)
	ctx := context.Background()

	// memory.delete has default policy always-approve-audit.
	result, err := e.Decide(ctx, server.GovernanceReq{
		SourceAgentID:        "agent-A",
		TargetAgentID:        "agent-B",
		Skill:                "echo",
		RequiredCapabilities: []string{"memory.delete"},
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if result.Decision != "escalate" {
		t.Errorf("Decision = %q, want %q", result.Decision, "escalate")
	}
}

func TestDecide_ALLGrant_CoversEverything(t *testing.T) {
	e, s := newTestEngine(t)
	ctx := context.Background()

	addGrant(t, s, "agent-A", "agent-B", "*", "ALL", "allow")

	result, err := e.Decide(ctx, server.GovernanceReq{
		SourceAgentID:        "agent-A",
		TargetAgentID:        "agent-B",
		Skill:                "echo",
		RequiredCapabilities: []string{"memory.read", "memory.write", "fs.read"},
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if result.Decision != "allow" {
		t.Errorf("Decision = %q, want %q", result.Decision, "allow")
	}
}

func TestDecide_ExpiredGrant_Ignored(t *testing.T) {
	e, s := newTestEngine(t)
	ctx := context.Background()

	exp := frozenTime.Add(-time.Hour) // expired
	g := &Grant{
		GrantID:        a2a.NewGrantID(),
		SourceAgentID:  "agent-A",
		TargetAgentID:  "agent-B",
		CapabilityGlob: "memory.write",
		Scope:          "SCOPED",
		Decision:       "allow",
		ExpiresAt:      &exp,
		IssuedBy:       "test",
		IssuedAt:       frozenTime.Add(-2 * time.Hour),
	}
	if err := s.CreateGrant(g); err != nil {
		t.Fatalf("CreateGrant: %v", err)
	}

	// memory.write without grant -> escalate (approve-once default).
	result, err := e.Decide(ctx, server.GovernanceReq{
		SourceAgentID:        "agent-A",
		TargetAgentID:        "agent-B",
		Skill:                "echo",
		RequiredCapabilities: []string{"memory.write"},
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if result.Decision != "escalate" {
		t.Errorf("Decision = %q, want %q (expired grant should be ignored)", result.Decision, "escalate")
	}
}

func TestDecide_RevokedGrant_Ignored(t *testing.T) {
	e, s := newTestEngine(t)
	ctx := context.Background()

	g := addGrant(t, s, "agent-A", "agent-B", "memory.write", "SCOPED", "allow")
	if err := s.RevokeGrant(g.GrantID, frozenTime.Add(-30*time.Minute)); err != nil {
		t.Fatalf("RevokeGrant: %v", err)
	}

	result, err := e.Decide(ctx, server.GovernanceReq{
		SourceAgentID:        "agent-A",
		TargetAgentID:        "agent-B",
		Skill:                "echo",
		RequiredCapabilities: []string{"memory.write"},
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if result.Decision != "escalate" {
		t.Errorf("Decision = %q, want %q (revoked grant should be ignored)", result.Decision, "escalate")
	}
}

func TestDecide_DestructiveSkill_AlwaysEscalates(t *testing.T) {
	e, s := newTestEngine(t)
	ctx := context.Background()

	// Even with an ALL allow grant, destructive skills escalate.
	addGrant(t, s, "agent-A", "agent-B", "*", "ALL", "allow")

	result, err := e.Decide(ctx, server.GovernanceReq{
		SourceAgentID:        "agent-A",
		TargetAgentID:        "agent-B",
		Skill:                "dangerous",
		RequiredCapabilities: []string{"shell.exec"},
		Destructive:          true,
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if result.Decision != "escalate" {
		t.Errorf("Decision = %q, want %q", result.Decision, "escalate")
	}
}

func TestDecide_GlobMatchExact(t *testing.T) {
	e, s := newTestEngine(t)
	ctx := context.Background()

	addGrant(t, s, "agent-A", "agent-B", "memory.read", "SCOPED", "allow")

	result, err := e.Decide(ctx, server.GovernanceReq{
		SourceAgentID:        "agent-A",
		TargetAgentID:        "agent-B",
		RequiredCapabilities: []string{"memory.read"},
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if result.Decision != "allow" {
		t.Errorf("Decision = %q, want %q", result.Decision, "allow")
	}
}

func TestDecide_GlobMatchWildcard(t *testing.T) {
	e, s := newTestEngine(t)
	ctx := context.Background()

	addGrant(t, s, "agent-A", "agent-B", "memory.*", "SCOPED", "allow")

	result, err := e.Decide(ctx, server.GovernanceReq{
		SourceAgentID:        "agent-A",
		TargetAgentID:        "agent-B",
		RequiredCapabilities: []string{"memory.read"},
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if result.Decision != "allow" {
		t.Errorf("Decision = %q, want %q", result.Decision, "allow")
	}
}

func TestDecide_GlobMatchColonWildcard(t *testing.T) {
	e, s := newTestEngine(t)
	ctx := context.Background()

	addGrant(t, s, "agent-A", "agent-B", "messaging.send:*", "SCOPED", "allow")

	result, err := e.Decide(ctx, server.GovernanceReq{
		SourceAgentID:        "agent-A",
		TargetAgentID:        "agent-B",
		RequiredCapabilities: []string{"messaging.send:slack"},
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if result.Decision != "allow" {
		t.Errorf("Decision = %q, want %q", result.Decision, "allow")
	}
}

func TestDecide_MultipleGrants_MostSpecificWins(t *testing.T) {
	e, s := newTestEngine(t)
	ctx := context.Background()

	// ALL grant that denies.
	addGrant(t, s, "agent-A", "agent-B", "*", "ALL", "deny")
	// Exact grant that allows. Should take precedence.
	addGrant(t, s, "agent-A", "agent-B", "memory.read", "SCOPED", "allow")

	result, err := e.Decide(ctx, server.GovernanceReq{
		SourceAgentID:        "agent-A",
		TargetAgentID:        "agent-B",
		RequiredCapabilities: []string{"memory.read"},
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if result.Decision != "allow" {
		t.Errorf("Decision = %q, want %q (exact match should override ALL deny)", result.Decision, "allow")
	}
}

func TestDecide_MultipleCaps_PartialCoverage(t *testing.T) {
	e, s := newTestEngine(t)
	ctx := context.Background()

	// Grant covers memory.read (auto-allow default anyway) but not memory.write.
	addGrant(t, s, "agent-A", "agent-B", "memory.read", "SCOPED", "allow")

	result, err := e.Decide(ctx, server.GovernanceReq{
		SourceAgentID:        "agent-A",
		TargetAgentID:        "agent-B",
		RequiredCapabilities: []string{"memory.read", "memory.write"},
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	// memory.write default is approve-once-per-scope -> escalate.
	if result.Decision != "escalate" {
		t.Errorf("Decision = %q, want %q", result.Decision, "escalate")
	}
}

func TestDecide_AuditIDAlwaysPresent(t *testing.T) {
	e, _ := newTestEngine(t)
	ctx := context.Background()

	result, err := e.Decide(ctx, server.GovernanceReq{
		SourceAgentID:        "agent-A",
		TargetAgentID:        "agent-B",
		RequiredCapabilities: []string{"memory.read"},
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if result.AuditID == "" {
		t.Error("AuditID should not be empty")
	}
	if err := a2a.ValidateID(result.AuditID); err != nil {
		t.Errorf("AuditID %q is invalid: %v", result.AuditID, err)
	}
}

func TestDecide_Determinism_100Runs(t *testing.T) {
	e, s := newTestEngine(t)
	ctx := context.Background()

	addGrant(t, s, "agent-A", "agent-B", "memory.read", "SCOPED", "allow")

	req := server.GovernanceReq{
		SourceAgentID:        "agent-A",
		TargetAgentID:        "agent-B",
		Skill:                "echo",
		RequiredCapabilities: []string{"memory.read"},
	}

	// All 100 runs should produce the same Decision.
	var firstDecision string
	for i := 0; i < 100; i++ {
		result, err := e.Decide(ctx, req)
		if err != nil {
			t.Fatalf("Decide(%d): %v", i, err)
		}
		if i == 0 {
			firstDecision = result.Decision
		} else if result.Decision != firstDecision {
			t.Fatalf("run %d: Decision = %q, want %q (determinism)", i, result.Decision, firstDecision)
		}
	}
	if firstDecision != "allow" {
		t.Errorf("expected 'allow', got %q", firstDecision)
	}
}

func TestDecide_Determinism_Escalate(t *testing.T) {
	e, _ := newTestEngine(t)
	ctx := context.Background()

	req := server.GovernanceReq{
		SourceAgentID:        "agent-A",
		TargetAgentID:        "agent-B",
		Skill:                "echo",
		RequiredCapabilities: []string{"memory.write"},
	}

	for i := 0; i < 100; i++ {
		result, err := e.Decide(ctx, req)
		if err != nil {
			t.Fatalf("Decide(%d): %v", i, err)
		}
		if result.Decision != "escalate" {
			t.Fatalf("run %d: Decision = %q, want %q", i, result.Decision, "escalate")
		}
	}
}

func TestDecide_Determinism_Deny(t *testing.T) {
	e, s := newTestEngine(t)
	ctx := context.Background()

	addGrant(t, s, "agent-A", "agent-B", "memory.read", "SCOPED", "deny")

	req := server.GovernanceReq{
		SourceAgentID:        "agent-A",
		TargetAgentID:        "agent-B",
		RequiredCapabilities: []string{"memory.read"},
	}

	for i := 0; i < 100; i++ {
		result, err := e.Decide(ctx, req)
		if err != nil {
			t.Fatalf("Decide(%d): %v", i, err)
		}
		if result.Decision != "deny" {
			t.Fatalf("run %d: Decision = %q, want %q", i, result.Decision, "deny")
		}
	}
}

func TestDecide_UnknownCustomCapability_Escalate(t *testing.T) {
	e, _ := newTestEngine(t)
	ctx := context.Background()

	// Unknown capabilities default to always-approve-audit -> escalate.
	result, err := e.Decide(ctx, server.GovernanceReq{
		SourceAgentID:        "agent-A",
		TargetAgentID:        "agent-B",
		RequiredCapabilities: []string{"custom.capability.foo"},
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if result.Decision != "escalate" {
		t.Errorf("Decision = %q, want %q", result.Decision, "escalate")
	}
}

func TestDecide_SystemInfo_AutoAllow(t *testing.T) {
	e, _ := newTestEngine(t)
	ctx := context.Background()

	result, err := e.Decide(ctx, server.GovernanceReq{
		SourceAgentID:        "agent-A",
		TargetAgentID:        "agent-B",
		RequiredCapabilities: []string{"system.info"},
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if result.Decision != "allow" {
		t.Errorf("Decision = %q, want %q", result.Decision, "allow")
	}
}

func TestDecide_MixedAutoAllowAndApprove(t *testing.T) {
	e, _ := newTestEngine(t)
	ctx := context.Background()

	// memory.read = auto-allow, memory.write = approve-once -> escalate.
	result, err := e.Decide(ctx, server.GovernanceReq{
		SourceAgentID:        "agent-A",
		TargetAgentID:        "agent-B",
		RequiredCapabilities: []string{"memory.read", "memory.write"},
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if result.Decision != "escalate" {
		t.Errorf("Decision = %q, want %q", result.Decision, "escalate")
	}
}

func TestDecide_OnlyAutoAllow_Allow(t *testing.T) {
	e, _ := newTestEngine(t)
	ctx := context.Background()

	result, err := e.Decide(ctx, server.GovernanceReq{
		SourceAgentID:        "agent-A",
		TargetAgentID:        "agent-B",
		RequiredCapabilities: []string{"memory.read", "system.info"},
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if result.Decision != "allow" {
		t.Errorf("Decision = %q, want %q", result.Decision, "allow")
	}
}

func TestDecide_DenyGrantOverridesAutoAllow(t *testing.T) {
	e, s := newTestEngine(t)
	ctx := context.Background()

	// Deny grant for memory.read overrides its auto-allow default.
	addGrant(t, s, "agent-A", "agent-B", "memory.read", "SCOPED", "deny")

	result, err := e.Decide(ctx, server.GovernanceReq{
		SourceAgentID:        "agent-A",
		TargetAgentID:        "agent-B",
		RequiredCapabilities: []string{"memory.read"},
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if result.Decision != "deny" {
		t.Errorf("Decision = %q, want %q", result.Decision, "deny")
	}
}

func TestDecide_GrantForDifferentPair_NoMatch(t *testing.T) {
	e, s := newTestEngine(t)
	ctx := context.Background()

	addGrant(t, s, "agent-X", "agent-Y", "memory.write", "SCOPED", "allow")

	result, err := e.Decide(ctx, server.GovernanceReq{
		SourceAgentID:        "agent-A",
		TargetAgentID:        "agent-B",
		RequiredCapabilities: []string{"memory.write"},
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if result.Decision != "escalate" {
		t.Errorf("Decision = %q, want %q", result.Decision, "escalate")
	}
}

func TestDecide_GlobDoesNotMatchPartialPrefix(t *testing.T) {
	e, s := newTestEngine(t)
	ctx := context.Background()

	// "memory.*" should NOT match "memory" alone.
	addGrant(t, s, "agent-A", "agent-B", "memory.*", "SCOPED", "allow")

	result, err := e.Decide(ctx, server.GovernanceReq{
		SourceAgentID:        "agent-A",
		TargetAgentID:        "agent-B",
		RequiredCapabilities: []string{"memoryfoo"},
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	// memoryfoo is unknown -> always-approve-audit -> escalate.
	if result.Decision != "escalate" {
		t.Errorf("Decision = %q, want %q", result.Decision, "escalate")
	}
}

func TestDecide_ActiveGrantWithFutureExpiry(t *testing.T) {
	e, s := newTestEngine(t)
	ctx := context.Background()

	exp := frozenTime.Add(time.Hour) // not expired
	g := &Grant{
		GrantID:        a2a.NewGrantID(),
		SourceAgentID:  "agent-A",
		TargetAgentID:  "agent-B",
		CapabilityGlob: "memory.write",
		Scope:          "SCOPED",
		Decision:       "allow",
		ExpiresAt:      &exp,
		IssuedBy:       "test",
		IssuedAt:       frozenTime.Add(-time.Hour),
	}
	if err := s.CreateGrant(g); err != nil {
		t.Fatalf("CreateGrant: %v", err)
	}

	result, err := e.Decide(ctx, server.GovernanceReq{
		SourceAgentID:        "agent-A",
		TargetAgentID:        "agent-B",
		RequiredCapabilities: []string{"memory.write"},
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if result.Decision != "allow" {
		t.Errorf("Decision = %q, want %q", result.Decision, "allow")
	}
}

// --- Sub-interface tests ---

func TestEngine_ListGrants(t *testing.T) {
	e, s := newTestEngine(t)
	ctx := context.Background()

	addGrant(t, s, "agent-A", "agent-B", "memory.read", "SCOPED", "allow")
	addGrant(t, s, "agent-C", "agent-D", "fs.read", "SCOPED", "allow")

	grants, err := e.ListGrants(ctx)
	if err != nil {
		t.Fatalf("ListGrants: %v", err)
	}
	if len(grants) != 2 {
		t.Errorf("len(grants) = %d, want 2", len(grants))
	}
}

func TestEngine_CreateGrant(t *testing.T) {
	e, _ := newTestEngine(t)
	ctx := context.Background()

	sg := server.Grant{
		GrantID:       a2a.NewGrantID(),
		SourceAgentID: "agent-A",
		TargetAgentID: "agent-B",
		Decision:      "allow",
		Skill:         "echo",
	}

	created, err := e.CreateGrant(ctx, sg)
	if err != nil {
		t.Fatalf("CreateGrant: %v", err)
	}
	if created.GrantID != sg.GrantID {
		t.Errorf("GrantID = %q, want %q", created.GrantID, sg.GrantID)
	}
	if created.SourceAgentID != sg.SourceAgentID {
		t.Errorf("SourceAgentID = %q, want %q", created.SourceAgentID, sg.SourceAgentID)
	}
}

func TestEngine_RevokeGrant(t *testing.T) {
	e, s := newTestEngine(t)
	ctx := context.Background()

	g := addGrant(t, s, "agent-A", "agent-B", "memory.read", "SCOPED", "allow")

	if err := e.RevokeGrant(ctx, g.GrantID); err != nil {
		t.Fatalf("RevokeGrant: %v", err)
	}

	got, err := s.GetGrant(g.GrantID)
	if err != nil {
		t.Fatalf("GetGrant: %v", err)
	}
	if got.RevokedAt == nil {
		t.Fatal("RevokedAt should not be nil")
	}
}

func TestEngine_ListApprovals(t *testing.T) {
	e, s := newTestEngine(t)
	ctx := context.Background()

	ap := &PendingApproval{
		ApprovalID:    a2a.NewApprovalID(),
		TaskID:        a2a.NewTaskID(),
		SourceAgentID: "agent-A",
		TargetAgentID: "agent-B",
		Skill:         "echo",
		CreatedAt:     frozenTime,
	}
	if err := s.CreateApproval(ap); err != nil {
		t.Fatalf("CreateApproval: %v", err)
	}

	approvals, err := e.ListApprovals(ctx)
	if err != nil {
		t.Fatalf("ListApprovals: %v", err)
	}
	if len(approvals) != 1 {
		t.Errorf("len(approvals) = %d, want 1", len(approvals))
	}
	if approvals[0].ApprovalID != ap.ApprovalID {
		t.Errorf("ApprovalID = %q, want %q", approvals[0].ApprovalID, ap.ApprovalID)
	}
}

func TestEngine_DecideApproval(t *testing.T) {
	e, s := newTestEngine(t)
	ctx := context.Background()

	ap := &PendingApproval{
		ApprovalID:    a2a.NewApprovalID(),
		TaskID:        a2a.NewTaskID(),
		SourceAgentID: "agent-A",
		TargetAgentID: "agent-B",
		Skill:         "echo",
		CreatedAt:     frozenTime,
	}
	if err := s.CreateApproval(ap); err != nil {
		t.Fatalf("CreateApproval: %v", err)
	}

	if err := e.DecideApproval(ctx, ap.ApprovalID, "approved", "looks good"); err != nil {
		t.Fatalf("DecideApproval: %v", err)
	}

	got, err := s.GetApproval(ap.ApprovalID)
	if err != nil {
		t.Fatalf("GetApproval: %v", err)
	}
	if got.Resolution != "approved" {
		t.Errorf("Resolution = %q, want %q", got.Resolution, "approved")
	}
}

func TestEngine_DecideApproval_NotFound(t *testing.T) {
	e, _ := newTestEngine(t)
	ctx := context.Background()

	err := e.DecideApproval(ctx, "apr_NONEXISTENT0000000000000", "approved", "")
	if err == nil {
		t.Fatal("expected error for non-existent approval")
	}
}

// --- Grant.IsExpired / IsRevoked / IsActive tests ---

func TestGrant_IsExpired(t *testing.T) {
	now := frozenTime
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)

	g := &Grant{ExpiresAt: &past}
	if !g.IsExpired(now) {
		t.Error("expected expired for past time")
	}

	g = &Grant{ExpiresAt: &future}
	if g.IsExpired(now) {
		t.Error("expected not expired for future time")
	}

	g = &Grant{ExpiresAt: nil}
	if g.IsExpired(now) {
		t.Error("expected not expired for nil expiry")
	}
}

func TestGrant_IsRevoked(t *testing.T) {
	now := frozenTime

	g := &Grant{RevokedAt: &now}
	if !g.IsRevoked() {
		t.Error("expected revoked")
	}

	g = &Grant{RevokedAt: nil}
	if g.IsRevoked() {
		t.Error("expected not revoked")
	}
}

func TestGrant_IsActive(t *testing.T) {
	now := frozenTime
	past := now.Add(-time.Hour)

	tests := []struct {
		name      string
		expiresAt *time.Time
		revokedAt *time.Time
		wantActive bool
	}{
		{"active", nil, nil, true},
		{"expired", &past, nil, false},
		{"revoked", nil, &now, false},
		{"both", &past, &now, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &Grant{ExpiresAt: tt.expiresAt, RevokedAt: tt.revokedAt}
			if got := g.IsActive(now); got != tt.wantActive {
				t.Errorf("IsActive = %v, want %v", got, tt.wantActive)
			}
		})
	}
}

func TestDecide_ShellExec_NoGrant_Escalate(t *testing.T) {
	e, _ := newTestEngine(t)
	ctx := context.Background()

	result, err := e.Decide(ctx, server.GovernanceReq{
		SourceAgentID:        "agent-A",
		TargetAgentID:        "agent-B",
		RequiredCapabilities: []string{"shell.exec"},
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if result.Decision != "escalate" {
		t.Errorf("Decision = %q, want %q", result.Decision, "escalate")
	}
}

func TestDecide_FSWrite_NoGrant_Escalate(t *testing.T) {
	e, _ := newTestEngine(t)
	ctx := context.Background()

	result, err := e.Decide(ctx, server.GovernanceReq{
		SourceAgentID:        "agent-A",
		TargetAgentID:        "agent-B",
		RequiredCapabilities: []string{"fs.write"},
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if result.Decision != "escalate" {
		t.Errorf("Decision = %q, want %q", result.Decision, "escalate")
	}
}

func TestDecide_NetFetch_NoGrant_Escalate(t *testing.T) {
	e, _ := newTestEngine(t)
	ctx := context.Background()

	result, err := e.Decide(ctx, server.GovernanceReq{
		SourceAgentID:        "agent-A",
		TargetAgentID:        "agent-B",
		RequiredCapabilities: []string{"net.fetch"},
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if result.Decision != "escalate" {
		t.Errorf("Decision = %q, want %q", result.Decision, "escalate")
	}
}

func TestDecide_AllAllowThenDeny(t *testing.T) {
	e, s := newTestEngine(t)
	ctx := context.Background()

	// First grant allows memory.read.
	addGrant(t, s, "agent-A", "agent-B", "memory.read", "SCOPED", "allow")
	// Second grant denies memory.write.
	addGrant(t, s, "agent-A", "agent-B", "memory.write", "SCOPED", "deny")

	result, err := e.Decide(ctx, server.GovernanceReq{
		SourceAgentID:        "agent-A",
		TargetAgentID:        "agent-B",
		RequiredCapabilities: []string{"memory.read", "memory.write"},
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if result.Decision != "deny" {
		t.Errorf("Decision = %q, want %q", result.Decision, "deny")
	}
}

func TestDecide_ReasonNotEmpty(t *testing.T) {
	e, _ := newTestEngine(t)
	ctx := context.Background()

	result, err := e.Decide(ctx, server.GovernanceReq{
		SourceAgentID:        "agent-A",
		TargetAgentID:        "agent-B",
		RequiredCapabilities: []string{"memory.read"},
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if result.Reason == "" {
		t.Error("Reason should not be empty")
	}
}

func TestDecide_GrantID_PresentOnAllow(t *testing.T) {
	e, s := newTestEngine(t)
	ctx := context.Background()

	g := addGrant(t, s, "agent-A", "agent-B", "memory.read", "SCOPED", "allow")

	result, err := e.Decide(ctx, server.GovernanceReq{
		SourceAgentID:        "agent-A",
		TargetAgentID:        "agent-B",
		RequiredCapabilities: []string{"memory.read"},
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if result.GrantID != g.GrantID {
		t.Errorf("GrantID = %q, want %q", result.GrantID, g.GrantID)
	}
}
