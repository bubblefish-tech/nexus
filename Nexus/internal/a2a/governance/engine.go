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
	"fmt"
	"log/slog"
	"time"

	"github.com/bubblefish-tech/nexus/internal/a2a"
	"github.com/bubblefish-tech/nexus/internal/a2a/server"
)

// Compile-time checks that Engine implements the server interfaces.
var (
	_ server.GovernanceEngine     = (*Engine)(nil)
	_ server.GrantsListEngine     = (*Engine)(nil)
	_ server.GrantsCreateEngine   = (*Engine)(nil)
	_ server.GrantsRevokeEngine   = (*Engine)(nil)
	_ server.ApprovalsListEngine  = (*Engine)(nil)
	_ server.ApprovalsDecideEngine = (*Engine)(nil)
)

// Engine implements server.GovernanceEngine and the governance sub-interfaces.
// It is deterministic: given identical inputs and state, it produces identical
// output. Time is provided via the nowFunc field, never via time.Now().
type Engine struct {
	store   *GrantStore
	nowFunc func() time.Time
	logger  *slog.Logger
}

// EngineOption configures an Engine.
type EngineOption func(*Engine)

// WithNowFunc sets the time function for deterministic testing.
func WithNowFunc(f func() time.Time) EngineOption {
	return func(e *Engine) { e.nowFunc = f }
}

// WithLogger sets the logger for the engine.
func WithLogger(l *slog.Logger) EngineOption {
	return func(e *Engine) { e.logger = l }
}

// NewEngine creates a governance Engine backed by the given GrantStore.
func NewEngine(store *GrantStore, opts ...EngineOption) *Engine {
	e := &Engine{
		store:   store,
		nowFunc: time.Now,
		logger:  slog.Default(),
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Decide evaluates governance policy for the given request.
//
// Decision logic:
//  1. Look up grants matching (source, target).
//  2. For each required capability, find a grant whose glob matches.
//  3. If any matching grant has decision "deny" -> deny.
//  4. If all capabilities are covered by "allow" grants -> allow.
//  5. If the request is destructive, always escalate (even with grants).
//  6. Otherwise, check the default policy for uncovered capabilities:
//     - auto-allow -> allow
//     - approve-once / always-approve / always-approve-audit -> escalate
//     - deny -> deny
func (e *Engine) Decide(_ context.Context, req server.GovernanceReq) (*server.GovernanceResult, error) {
	now := e.nowFunc()
	auditID := a2a.NewAuditID()

	grants, err := e.store.FindMatchingGrants(req.SourceAgentID, req.TargetAgentID)
	if err != nil {
		return nil, fmt.Errorf("governance: find grants: %w", err)
	}

	// Filter to active grants only.
	var active []*Grant
	for _, g := range grants {
		if g.IsActive(now) {
			active = append(active, g)
		}
	}

	// If the request is destructive, always escalate regardless of grants.
	if req.Destructive {
		e.logger.Info("governance: destructive skill always escalates",
			"source", req.SourceAgentID,
			"target", req.TargetAgentID,
			"skill", req.Skill,
		)
		return &server.GovernanceResult{
			Decision: "escalate",
			Reason:   "destructive skill always requires approval",
			AuditID:  auditID,
		}, nil
	}

	// If no capabilities are required, allow.
	if len(req.RequiredCapabilities) == 0 {
		return &server.GovernanceResult{
			Decision: "allow",
			Reason:   "no capabilities required",
			AuditID:  auditID,
		}, nil
	}

	// For each required capability, try to find a matching grant.
	var (
		allCovered  = true
		denyGrant   *Grant
		coverGrant  *Grant
		uncoveredCaps []string
	)

	for _, cap := range req.RequiredCapabilities {
		matched := findBestGrant(active, cap)
		if matched == nil {
			allCovered = false
			uncoveredCaps = append(uncoveredCaps, cap)
			continue
		}
		if matched.Decision == "deny" {
			denyGrant = matched
			break
		}
		// Track the most recent covering grant for the response.
		if coverGrant == nil || matched.IssuedAt.After(coverGrant.IssuedAt) {
			coverGrant = matched
		}
	}

	// Explicit deny takes priority.
	if denyGrant != nil {
		e.logger.Info("governance: deny grant matched",
			"grant_id", denyGrant.GrantID,
			"source", req.SourceAgentID,
			"target", req.TargetAgentID,
		)
		return &server.GovernanceResult{
			Decision: "deny",
			GrantID:  denyGrant.GrantID,
			Reason:   fmt.Sprintf("denied by grant %s", denyGrant.GrantID),
			AuditID:  auditID,
		}, nil
	}

	// All capabilities covered by allow grants.
	if allCovered {
		grantID := ""
		if coverGrant != nil {
			grantID = coverGrant.GrantID
		}
		return &server.GovernanceResult{
			Decision: "allow",
			GrantID:  grantID,
			Reason:   "all capabilities covered by grants",
			AuditID:  auditID,
		}, nil
	}

	// Some capabilities uncovered: check default policies.
	return e.decideByDefaultPolicy(uncoveredCaps, auditID)
}

// decideByDefaultPolicy applies the built-in default policies for
// uncovered capabilities. If any capability's default is deny, the
// overall result is deny. If any requires approval, escalate.
// Otherwise allow (auto-allow).
func (e *Engine) decideByDefaultPolicy(uncoveredCaps []string, auditID string) (*server.GovernanceResult, error) {
	needsEscalation := false

	for _, cap := range uncoveredCaps {
		policy, _ := a2a.ResolvePolicy(cap)
		switch policy {
		case a2a.PolicyDeny:
			return &server.GovernanceResult{
				Decision: "deny",
				Reason:   fmt.Sprintf("default policy denies capability %q", cap),
				AuditID:  auditID,
			}, nil
		case a2a.PolicyAutoAllow:
			// Continue checking other caps.
		case a2a.PolicyApproveOncePerScope, a2a.PolicyAlwaysApprove, a2a.PolicyAlwaysApproveAudit:
			needsEscalation = true
		}
	}

	if needsEscalation {
		return &server.GovernanceResult{
			Decision: "escalate",
			Reason:   "default policy requires approval for one or more capabilities",
			AuditID:  auditID,
		}, nil
	}

	return &server.GovernanceResult{
		Decision: "allow",
		Reason:   "all uncovered capabilities auto-allowed by default policy",
		AuditID:  auditID,
	}, nil
}

// findBestGrant finds the most specific grant that matches the given capability.
// Priority: exact match > scoped glob > ALL scope.
// Returns nil if no grant matches.
func findBestGrant(grants []*Grant, capability string) *Grant {
	var (
		exactMatch  *Grant
		globMatch   *Grant
		allMatch    *Grant
	)

	for _, g := range grants {
		if g.Scope == "ALL" || g.CapabilityGlob == a2a.CapAll {
			if allMatch == nil || g.IssuedAt.After(allMatch.IssuedAt) {
				allMatch = g
			}
			continue
		}
		if g.CapabilityGlob == capability {
			if exactMatch == nil || g.IssuedAt.After(exactMatch.IssuedAt) {
				exactMatch = g
			}
			continue
		}
		if a2a.MatchCapabilityGlob(g.CapabilityGlob, capability) {
			if globMatch == nil || g.IssuedAt.After(globMatch.IssuedAt) {
				globMatch = g
			}
		}
	}

	// Most specific wins.
	if exactMatch != nil {
		return exactMatch
	}
	if globMatch != nil {
		return globMatch
	}
	return allMatch
}

// --- server.GrantsListEngine ---

// ListGrants returns all grants as server.Grant objects.
func (e *Engine) ListGrants(_ context.Context) ([]server.Grant, error) {
	grants, err := e.store.ListGrants()
	if err != nil {
		return nil, err
	}

	out := make([]server.Grant, len(grants))
	for i, g := range grants {
		out[i] = toServerGrant(g)
	}
	return out, nil
}

// --- server.GrantsCreateEngine ---

// CreateGrant creates a new grant from the server layer's Grant type.
func (e *Engine) CreateGrant(_ context.Context, sg server.Grant) (*server.Grant, error) {
	now := e.nowFunc()

	g := &Grant{
		GrantID:        sg.GrantID,
		SourceAgentID:  sg.SourceAgentID,
		TargetAgentID:  sg.TargetAgentID,
		CapabilityGlob: capGlobFromServerGrant(sg),
		Scope:          "SCOPED",
		Decision:       sg.Decision,
		IssuedBy:       "admin",
		IssuedAt:       now,
		Notes:          sg.Reason,
	}

	if err := e.store.CreateGrant(g); err != nil {
		return nil, err
	}

	result := toServerGrant(g)
	return &result, nil
}

// --- server.GrantsRevokeEngine ---

// RevokeGrant revokes a grant by ID.
func (e *Engine) RevokeGrant(_ context.Context, grantID string) error {
	return e.store.RevokeGrant(grantID, e.nowFunc())
}

// --- server.ApprovalsListEngine ---

// ListApprovals returns all pending approvals.
func (e *Engine) ListApprovals(_ context.Context) ([]server.Approval, error) {
	approvals, err := e.store.ListPendingApprovals()
	if err != nil {
		return nil, err
	}

	out := make([]server.Approval, len(approvals))
	for i, ap := range approvals {
		out[i] = server.Approval{
			ApprovalID:    ap.ApprovalID,
			SourceAgentID: ap.SourceAgentID,
			TargetAgentID: ap.TargetAgentID,
			Skill:         ap.Skill,
			Reason:        ap.InputPreview,
			CreatedAt:     a2a.FormatTime(ap.CreatedAt),
		}
	}
	return out, nil
}

// --- server.ApprovalsDecideEngine ---

// DecideApproval resolves a pending approval with the given decision.
func (e *Engine) DecideApproval(_ context.Context, approvalID string, decision string, reason string) error {
	resolvedBy := "admin"
	if reason != "" {
		resolvedBy = reason
	}
	return e.store.ResolveApproval(approvalID, resolvedBy, decision, e.nowFunc())
}

// --- helpers ---

// toServerGrant converts an internal Grant to the server package's Grant type.
func toServerGrant(g *Grant) server.Grant {
	return server.Grant{
		GrantID:       g.GrantID,
		SourceAgentID: g.SourceAgentID,
		TargetAgentID: g.TargetAgentID,
		Decision:      g.Decision,
		Reason:        g.Notes,
		CreatedAt:     a2a.FormatTime(g.IssuedAt),
	}
}

// capGlobFromServerGrant builds a capability glob from the server Grant.
// If RequiredCapabilities has entries, use the first one; otherwise use "*".
func capGlobFromServerGrant(sg server.Grant) string {
	if len(sg.RequiredCapabilities) > 0 {
		return sg.RequiredCapabilities[0]
	}
	if sg.Skill != "" {
		return "agent.invoke:" + sg.Skill
	}
	return "*"
}
