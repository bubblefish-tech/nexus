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

package policy_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/BubbleFish-Nexus/internal/a2a"
	"github.com/BubbleFish-Nexus/internal/a2a/registry"
	"github.com/BubbleFish-Nexus/internal/a2a/transport"
	"github.com/BubbleFish-Nexus/internal/actions"
	"github.com/BubbleFish-Nexus/internal/approvals"
	"github.com/BubbleFish-Nexus/internal/grants"
	"github.com/BubbleFish-Nexus/internal/policy"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

type engineFixture struct {
	reg       *registry.Store
	grantSt   *grants.Store
	approvalSt *approvals.Store
	actionSt  *actions.Store
}

func newEngineFixture(t *testing.T) *engineFixture {
	t.Helper()
	path := filepath.Join(t.TempDir(), "registry.db")
	reg, err := registry.NewStore(path)
	if err != nil {
		t.Fatalf("registry.NewStore: %v", err)
	}
	t.Cleanup(func() { _ = reg.Close() })
	db := reg.DB()
	return &engineFixture{
		reg:        reg,
		grantSt:    grants.NewStore(db),
		approvalSt: approvals.NewStore(db),
		actionSt:   actions.NewStore(db),
	}
}

func (f *engineFixture) engine(t *testing.T, cfg policy.EngineConfig) *policy.Engine {
	t.Helper()
	return policy.NewEngine(f.reg, f.grantSt, f.approvalSt, f.actionSt, cfg, nil)
}

func (f *engineFixture) registerAgent(t *testing.T, agentID, status string) {
	t.Helper()
	err := f.reg.Register(context.Background(), registry.RegisteredAgent{
		AgentID:     agentID,
		Name:        agentID,
		DisplayName: agentID,
		AgentCard: a2a.AgentCard{
			Name:            agentID,
			URL:             "http://localhost",
			ProtocolVersion: "0.1",
		},
		TransportConfig: transport.TransportConfig{Kind: "http", URL: "http://localhost"},
		Status:          status,
	})
	if err != nil {
		t.Fatalf("register agent %q: %v", agentID, err)
	}
}

func (f *engineFixture) createGrant(t *testing.T, agentID, capability string, scope json.RawMessage) string {
	t.Helper()
	g, err := f.grantSt.Create(context.Background(), grants.Grant{
		AgentID:    agentID,
		Capability: capability,
		Scope:      scope,
		GrantedBy:  "test",
	})
	if err != nil {
		t.Fatalf("create grant: %v", err)
	}
	return g.GrantID
}

func (f *engineFixture) createApproval(t *testing.T, agentID, capability, status string) string {
	t.Helper()
	r, err := f.approvalSt.Create(context.Background(), approvals.Request{
		AgentID:    agentID,
		Capability: capability,
		Action:     json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("create approval: %v", err)
	}
	if status != approvals.StatusPending {
		if err := f.approvalSt.Decide(context.Background(), r.RequestID, approvals.DecideInput{
			Decision:  approvals.DecisionApprove,
			DecidedBy: "test",
		}); err != nil {
			t.Fatalf("decide approval: %v", err)
		}
	}
	return r.RequestID
}

func noopCfg() policy.EngineConfig {
	return policy.EngineConfig{}
}

func requireApprovalCfg(caps ...string) policy.EngineConfig {
	return policy.EngineConfig{RequireApproval: caps}
}

// ---------------------------------------------------------------------------
// Tests: agent checks
// ---------------------------------------------------------------------------

func TestEvaluate_AgentNotFound(t *testing.T) {
	f := newEngineFixture(t)
	e := f.engine(t, noopCfg())
	d := e.Evaluate(context.Background(), "ghost_agent", "cap", nil)
	if d.Allowed {
		t.Fatal("want denied for unknown agent")
	}
	if d.Reason != "agent not found" {
		t.Fatalf("reason = %q", d.Reason)
	}
}

func TestEvaluate_AgentSuspended(t *testing.T) {
	f := newEngineFixture(t)
	f.registerAgent(t, "a1", registry.StatusSuspended)
	e := f.engine(t, noopCfg())
	d := e.Evaluate(context.Background(), "a1", "cap", nil)
	if d.Allowed {
		t.Fatal("want denied for suspended agent")
	}
	if d.Reason != "agent status: suspended" {
		t.Fatalf("reason = %q", d.Reason)
	}
}

func TestEvaluate_AgentRetired(t *testing.T) {
	f := newEngineFixture(t)
	f.registerAgent(t, "a1", registry.StatusRetired)
	e := f.engine(t, noopCfg())
	d := e.Evaluate(context.Background(), "a1", "cap", nil)
	if d.Allowed {
		t.Fatal("want denied for retired agent")
	}
	if d.Reason != "agent status: retired" {
		t.Fatalf("reason = %q", d.Reason)
	}
}

// ---------------------------------------------------------------------------
// Tests: grant checks
// ---------------------------------------------------------------------------

func TestEvaluate_NoGrant(t *testing.T) {
	f := newEngineFixture(t)
	f.registerAgent(t, "a1", registry.StatusActive)
	e := f.engine(t, noopCfg())
	d := e.Evaluate(context.Background(), "a1", "nexus_write", nil)
	if d.Allowed {
		t.Fatal("want denied — no grant")
	}
	if d.Reason != "no active grant" {
		t.Fatalf("reason = %q", d.Reason)
	}
}

func TestEvaluate_ActiveGrant_Allowed(t *testing.T) {
	f := newEngineFixture(t)
	f.registerAgent(t, "a1", registry.StatusActive)
	grantID := f.createGrant(t, "a1", "nexus_write", nil)
	e := f.engine(t, noopCfg())
	d := e.Evaluate(context.Background(), "a1", "nexus_write", json.RawMessage(`{}`))
	if !d.Allowed {
		t.Fatalf("want allowed: %s", d.Reason)
	}
	if d.GrantID != grantID {
		t.Fatalf("GrantID = %q, want %q", d.GrantID, grantID)
	}
	if d.Reason != "grant" {
		t.Fatalf("reason = %q", d.Reason)
	}
}

func TestEvaluate_RevokedGrant_Denied(t *testing.T) {
	f := newEngineFixture(t)
	f.registerAgent(t, "a1", registry.StatusActive)
	grantID := f.createGrant(t, "a1", "nexus_write", nil)
	if err := f.grantSt.Revoke(context.Background(), grantID, "test revoke"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	e := f.engine(t, noopCfg())
	d := e.Evaluate(context.Background(), "a1", "nexus_write", nil)
	if d.Allowed {
		t.Fatal("want denied after revoke")
	}
	if d.Reason != "no active grant" {
		t.Fatalf("reason = %q", d.Reason)
	}
}

// ---------------------------------------------------------------------------
// Tests: scope matching
// ---------------------------------------------------------------------------

func TestEvaluate_ScopeEmpty_Allowed(t *testing.T) {
	f := newEngineFixture(t)
	f.registerAgent(t, "a1", registry.StatusActive)
	f.createGrant(t, "a1", "nexus_write", json.RawMessage(`{}`))
	e := f.engine(t, noopCfg())
	d := e.Evaluate(context.Background(), "a1", "nexus_write", json.RawMessage(`{"any":"field"}`))
	if !d.Allowed {
		t.Fatalf("empty scope should allow any action: %s", d.Reason)
	}
}

func TestEvaluate_ScopeMatch_Allowed(t *testing.T) {
	f := newEngineFixture(t)
	f.registerAgent(t, "a1", registry.StatusActive)
	f.createGrant(t, "a1", "nexus_write", json.RawMessage(`{"collection":"memories"}`))
	e := f.engine(t, noopCfg())
	d := e.Evaluate(context.Background(), "a1", "nexus_write",
		json.RawMessage(`{"collection":"memories","extra":"ok"}`))
	if !d.Allowed {
		t.Fatalf("scope key+value present: %s", d.Reason)
	}
}

func TestEvaluate_ScopeMismatch_Denied(t *testing.T) {
	f := newEngineFixture(t)
	f.registerAgent(t, "a1", registry.StatusActive)
	f.createGrant(t, "a1", "nexus_write", json.RawMessage(`{"collection":"memories"}`))
	e := f.engine(t, noopCfg())
	d := e.Evaluate(context.Background(), "a1", "nexus_write",
		json.RawMessage(`{"collection":"secrets"}`))
	if d.Allowed {
		t.Fatal("scope value mismatch should deny")
	}
	if d.Reason != "action outside grant scope" {
		t.Fatalf("reason = %q", d.Reason)
	}
}

func TestEvaluate_ScopeKeyMissing_Denied(t *testing.T) {
	f := newEngineFixture(t)
	f.registerAgent(t, "a1", registry.StatusActive)
	f.createGrant(t, "a1", "nexus_write", json.RawMessage(`{"collection":"memories"}`))
	e := f.engine(t, noopCfg())
	d := e.Evaluate(context.Background(), "a1", "nexus_write",
		json.RawMessage(`{"other":"field"}`))
	if d.Allowed {
		t.Fatal("missing scope key should deny")
	}
}

func TestEvaluate_ScopeNilAction_Denied(t *testing.T) {
	f := newEngineFixture(t)
	f.registerAgent(t, "a1", registry.StatusActive)
	f.createGrant(t, "a1", "nexus_write", json.RawMessage(`{"required":"field"}`))
	e := f.engine(t, noopCfg())
	d := e.Evaluate(context.Background(), "a1", "nexus_write", nil)
	if d.Allowed {
		t.Fatal("nil action against non-empty scope should deny")
	}
}

// ---------------------------------------------------------------------------
// Tests: approval requirement
// ---------------------------------------------------------------------------

func TestEvaluate_ApprovalRequired_NoApproval_Denied(t *testing.T) {
	f := newEngineFixture(t)
	f.registerAgent(t, "a1", registry.StatusActive)
	f.createGrant(t, "a1", "guarded_cap", nil)
	e := f.engine(t, requireApprovalCfg("guarded_cap"))
	d := e.Evaluate(context.Background(), "a1", "guarded_cap", nil)
	if d.Allowed {
		t.Fatal("want denied — no approval present")
	}
	if d.Reason != "approval required" {
		t.Fatalf("reason = %q", d.Reason)
	}
}

func TestEvaluate_ApprovalRequired_PendingNotSufficient_Denied(t *testing.T) {
	f := newEngineFixture(t)
	f.registerAgent(t, "a1", registry.StatusActive)
	f.createGrant(t, "a1", "guarded_cap", nil)
	f.createApproval(t, "a1", "guarded_cap", approvals.StatusPending)
	e := f.engine(t, requireApprovalCfg("guarded_cap"))
	d := e.Evaluate(context.Background(), "a1", "guarded_cap", nil)
	if d.Allowed {
		t.Fatal("want denied — approval still pending")
	}
}

func TestEvaluate_ApprovalRequired_WithApproval_Allowed(t *testing.T) {
	f := newEngineFixture(t)
	f.registerAgent(t, "a1", registry.StatusActive)
	grantID := f.createGrant(t, "a1", "guarded_cap", nil)
	aprID := f.createApproval(t, "a1", "guarded_cap", approvals.StatusApproved)
	e := f.engine(t, requireApprovalCfg("guarded_cap"))
	d := e.Evaluate(context.Background(), "a1", "guarded_cap", nil)
	if !d.Allowed {
		t.Fatalf("want allowed with approval: %s", d.Reason)
	}
	if d.GrantID != grantID {
		t.Fatalf("GrantID = %q, want %q", d.GrantID, grantID)
	}
	if d.ApprovalID != aprID {
		t.Fatalf("ApprovalID = %q, want %q", d.ApprovalID, aprID)
	}
	if d.Reason != "grant+approval" {
		t.Fatalf("reason = %q", d.Reason)
	}
}

func TestEvaluate_ApprovalNotRequired_OtherCap_Allowed(t *testing.T) {
	f := newEngineFixture(t)
	f.registerAgent(t, "a1", registry.StatusActive)
	f.createGrant(t, "a1", "free_cap", nil)
	e := f.engine(t, requireApprovalCfg("guarded_cap"))
	d := e.Evaluate(context.Background(), "a1", "free_cap", nil)
	if !d.Allowed {
		t.Fatalf("free_cap should not need approval: %s", d.Reason)
	}
}

// ---------------------------------------------------------------------------
// Tests: action log recording
// ---------------------------------------------------------------------------

func TestEvaluate_RecordsActionLog_Allow(t *testing.T) {
	f := newEngineFixture(t)
	f.registerAgent(t, "a1", registry.StatusActive)
	f.createGrant(t, "a1", "nexus_write", nil)
	e := f.engine(t, noopCfg())
	_ = e.Evaluate(context.Background(), "a1", "nexus_write", nil)

	acts, err := f.actionSt.Query(context.Background(), actions.QueryFilter{
		AgentID:        "a1",
		Capability:     "nexus_write",
		PolicyDecision: actions.DecisionAllow,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(acts) != 1 {
		t.Fatalf("got %d records, want 1", len(acts))
	}
}

func TestEvaluate_RecordsActionLog_Deny(t *testing.T) {
	f := newEngineFixture(t)
	f.registerAgent(t, "a1", registry.StatusActive)
	// no grant — will be denied
	e := f.engine(t, noopCfg())
	_ = e.Evaluate(context.Background(), "a1", "nexus_write", nil)

	acts, err := f.actionSt.Query(context.Background(), actions.QueryFilter{
		AgentID:        "a1",
		PolicyDecision: actions.DecisionDeny,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(acts) != 1 {
		t.Fatalf("got %d deny records, want 1", len(acts))
	}
}

func TestEvaluate_RecordsActionLog_AgentNotFound(t *testing.T) {
	f := newEngineFixture(t)
	e := f.engine(t, noopCfg())
	_ = e.Evaluate(context.Background(), "ghost", "cap", nil)

	acts, err := f.actionSt.Query(context.Background(), actions.QueryFilter{
		AgentID:        "ghost",
		PolicyDecision: actions.DecisionDeny,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(acts) != 1 {
		t.Fatalf("got %d records, want 1", len(acts))
	}
	if acts[0].PolicyReason != "agent not found" {
		t.Fatalf("reason = %q", acts[0].PolicyReason)
	}
}

// ---------------------------------------------------------------------------
// Tests: multiple evaluations / independence
// ---------------------------------------------------------------------------

func TestEvaluate_TwoAgentsSameCapability(t *testing.T) {
	f := newEngineFixture(t)
	f.registerAgent(t, "a1", registry.StatusActive)
	f.registerAgent(t, "a2", registry.StatusActive)
	f.createGrant(t, "a1", "nexus_write", nil) // only a1 has a grant
	e := f.engine(t, noopCfg())

	d1 := e.Evaluate(context.Background(), "a1", "nexus_write", nil)
	d2 := e.Evaluate(context.Background(), "a2", "nexus_write", nil)

	if !d1.Allowed {
		t.Fatalf("a1 should be allowed: %s", d1.Reason)
	}
	if d2.Allowed {
		t.Fatal("a2 should be denied — no grant")
	}
}

func TestEvaluate_SameAgentDifferentCapabilities(t *testing.T) {
	f := newEngineFixture(t)
	f.registerAgent(t, "a1", registry.StatusActive)
	f.createGrant(t, "a1", "nexus_write", nil)
	// no grant for nexus_read
	e := f.engine(t, noopCfg())

	dw := e.Evaluate(context.Background(), "a1", "nexus_write", nil)
	dr := e.Evaluate(context.Background(), "a1", "nexus_read", nil)

	if !dw.Allowed {
		t.Fatalf("nexus_write should be allowed: %s", dw.Reason)
	}
	if dr.Allowed {
		t.Fatal("nexus_read should be denied — no grant")
	}
}

func TestEvaluate_ScopeMultipleKeys_AllMatch(t *testing.T) {
	f := newEngineFixture(t)
	f.registerAgent(t, "a1", registry.StatusActive)
	f.createGrant(t, "a1", "nexus_write", json.RawMessage(`{"coll":"mem","tier":1}`))
	e := f.engine(t, noopCfg())
	d := e.Evaluate(context.Background(), "a1", "nexus_write",
		json.RawMessage(`{"coll":"mem","tier":1,"extra":"ok"}`))
	if !d.Allowed {
		t.Fatalf("all scope keys present: %s", d.Reason)
	}
}

func TestEvaluate_ScopeMultipleKeys_OneMissing(t *testing.T) {
	f := newEngineFixture(t)
	f.registerAgent(t, "a1", registry.StatusActive)
	f.createGrant(t, "a1", "nexus_write", json.RawMessage(`{"coll":"mem","tier":1}`))
	e := f.engine(t, noopCfg())
	d := e.Evaluate(context.Background(), "a1", "nexus_write",
		json.RawMessage(`{"coll":"mem"}`)) // tier missing
	if d.Allowed {
		t.Fatal("missing scope key should deny")
	}
}
