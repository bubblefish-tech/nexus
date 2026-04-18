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

package policy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/BubbleFish-Nexus/internal/a2a/registry"
	"github.com/BubbleFish-Nexus/internal/actions"
	"github.com/BubbleFish-Nexus/internal/approvals"
	"github.com/BubbleFish-Nexus/internal/grants"
)

// EngineConfig holds the runtime configuration for the policy engine. The
// daemon derives this from config.ControlConfig to avoid an import cycle
// (internal/config already imports internal/policy for compiled source policies).
type EngineConfig struct {
	// RequireApproval is a list of capability names that require an approved
	// approval request before the policy engine will allow the action.
	RequireApproval []string
}

// Decision is the outcome of a single policy evaluation.
type Decision struct {
	Allowed    bool
	Reason     string
	GrantID    string
	ApprovalID string
}

// Engine evaluates whether an agent may exercise a capability for a given
// action payload. It is fail-closed: any lookup error produces a deny.
type Engine struct {
	registry  *registry.Store
	grants    *grants.Store
	approvals *approvals.Store
	actions   *actions.Store
	cfg       EngineConfig
	logger    *slog.Logger
}

// NewEngine constructs a policy Engine. If logger is nil, slog.Default() is used.
func NewEngine(
	reg *registry.Store,
	g *grants.Store,
	apr *approvals.Store,
	act *actions.Store,
	cfg EngineConfig,
	logger *slog.Logger,
) *Engine {
	if logger == nil {
		logger = slog.Default()
	}
	return &Engine{
		registry:  reg,
		grants:    g,
		approvals: apr,
		actions:   act,
		cfg:       cfg,
		logger:    logger,
	}
}

// Evaluate checks whether agentID may exercise capability for the given action
// payload. The decision is recorded in the action_log regardless of outcome.
// action may be nil or empty — a nil/empty action is treated as an unconstrained
// action payload (equivalent to "{}") for scope matching purposes.
func (e *Engine) Evaluate(ctx context.Context, agentID, capability string, action json.RawMessage) Decision {
	// Step 1: look up agent — fail-closed on any error (not found or DB).
	agent, err := e.registry.Get(ctx, agentID)
	if err != nil {
		d := Decision{Allowed: false, Reason: "agent not found"}
		e.record(ctx, agentID, capability, "", "", d)
		return d
	}

	// Step 2: agent must be active.
	if agent.Status != registry.StatusActive {
		d := Decision{Allowed: false, Reason: fmt.Sprintf("agent status: %s", agent.Status)}
		e.record(ctx, agentID, capability, "", "", d)
		return d
	}

	// Step 3: find the most recently granted active grant for this (agent, capability).
	grant, err := e.grants.CheckGrant(ctx, agentID, capability)
	if err != nil {
		d := Decision{Allowed: false, Reason: "grant check failed"}
		e.record(ctx, agentID, capability, "", "", d)
		return d
	}
	if grant == nil {
		d := Decision{Allowed: false, Reason: "no active grant"}
		e.record(ctx, agentID, capability, "", "", d)
		return d
	}

	// Step 4: validate action payload against grant scope constraints.
	if !matchesScope(grant.Scope, action) {
		d := Decision{Allowed: false, Reason: "action outside grant scope"}
		e.record(ctx, agentID, capability, grant.GrantID, "", d)
		return d
	}

	// Step 5: check if capability requires a human approval.
	if e.requiresApproval(capability) {
		apr, err := e.findApproval(ctx, agentID, capability)
		if err != nil {
			d := Decision{Allowed: false, Reason: "approval lookup failed"}
			e.record(ctx, agentID, capability, grant.GrantID, "", d)
			return d
		}
		if apr == nil {
			d := Decision{Allowed: false, Reason: "approval required"}
			e.record(ctx, agentID, capability, grant.GrantID, "", d)
			return d
		}
		d := Decision{
			Allowed:    true,
			Reason:     "grant+approval",
			GrantID:    grant.GrantID,
			ApprovalID: apr.RequestID,
		}
		e.record(ctx, agentID, capability, grant.GrantID, apr.RequestID, d)
		return d
	}

	// Step 6: allowed by grant alone.
	d := Decision{Allowed: true, Reason: "grant", GrantID: grant.GrantID}
	e.record(ctx, agentID, capability, grant.GrantID, "", d)
	return d
}

func (e *Engine) requiresApproval(capability string) bool {
	for _, c := range e.cfg.RequireApproval {
		if c == capability {
			return true
		}
	}
	return false
}

func (e *Engine) findApproval(ctx context.Context, agentID, capability string) (*approvals.Request, error) {
	list, err := e.approvals.List(ctx, approvals.ListFilter{
		AgentID:    agentID,
		Capability: capability,
		Status:     approvals.StatusApproved,
		Limit:      1,
	})
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, nil
	}
	return &list[0], nil
}

func (e *Engine) record(ctx context.Context, agentID, capability, grantID, approvalID string, d Decision) {
	decision := actions.DecisionAllow
	if !d.Allowed {
		decision = actions.DecisionDeny
	}
	if _, err := e.actions.Record(ctx, actions.Action{
		AgentID:        agentID,
		Capability:     capability,
		GrantID:        grantID,
		ApprovalID:     approvalID,
		PolicyDecision: decision,
		PolicyReason:   d.Reason,
	}); err != nil {
		e.logger.Warn("policy: failed to record action", "error", err)
	}
}

// matchesScope reports whether action satisfies the grant scope constraints.
// An empty, nil, or "{}" scope is unconstrained (always true). Otherwise every
// top-level key in scope must appear in action with a JSON-equal value.
func matchesScope(scope, action json.RawMessage) bool {
	if len(scope) == 0 {
		return true
	}
	var sm map[string]json.RawMessage
	if err := json.Unmarshal(scope, &sm); err != nil || len(sm) == 0 {
		return true
	}
	if len(action) == 0 {
		return false
	}
	var am map[string]json.RawMessage
	if err := json.Unmarshal(action, &am); err != nil {
		return false
	}
	for k, sv := range sm {
		av, ok := am[k]
		if !ok {
			return false
		}
		if compactJSON(sv) != compactJSON(av) {
			return false
		}
	}
	return true
}

func compactJSON(b json.RawMessage) string {
	var buf bytes.Buffer
	if err := json.Compact(&buf, b); err != nil {
		return string(b)
	}
	return buf.String()
}
