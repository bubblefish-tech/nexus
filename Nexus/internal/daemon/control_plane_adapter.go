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

package daemon

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/BubbleFish-Nexus/internal/actions"
	"github.com/BubbleFish-Nexus/internal/approvals"
	"github.com/BubbleFish-Nexus/internal/grants"
	"github.com/BubbleFish-Nexus/internal/mcp"
	"github.com/BubbleFish-Nexus/internal/policy"
	"github.com/BubbleFish-Nexus/internal/tasks"
)

// controlPlaneAdapter adapts the daemon's control-plane stores and policy
// engine to the mcp.ControlPlaneProvider interface. One instance is created
// per daemon Start() when cfg.Control.Enabled is true.
type controlPlaneAdapter struct {
	engine    *policy.Engine
	grants    *grants.Store
	approvals *approvals.Store
	tasks     *tasks.Store
	actions   *actions.Store
	logger    *slog.Logger
}

func (a *controlPlaneAdapter) EvaluatePolicy(ctx context.Context, agentID, capability string, action json.RawMessage) mcp.ControlDecision {
	d := a.engine.Evaluate(ctx, agentID, capability, action)
	return mcp.ControlDecision{
		Allowed:    d.Allowed,
		Reason:     d.Reason,
		GrantID:    d.GrantID,
		ApprovalID: d.ApprovalID,
	}
}

func (a *controlPlaneAdapter) ListGrants(ctx context.Context, agentID string) ([]mcp.GrantInfo, error) {
	gs, err := a.grants.List(ctx, grants.ListFilter{AgentID: agentID, Limit: 100})
	if err != nil {
		return nil, err
	}
	out := make([]mcp.GrantInfo, len(gs))
	for i, g := range gs {
		out[i] = mcp.GrantInfo{
			GrantID:    g.GrantID,
			AgentID:    g.AgentID,
			Capability: g.Capability,
			Scope:      g.Scope,
			GrantedBy:  g.GrantedBy,
			GrantedAt:  g.GrantedAt,
			ExpiresAt:  g.ExpiresAt,
		}
	}
	return out, nil
}

func (a *controlPlaneAdapter) RequestApproval(ctx context.Context, agentID, capability string, action json.RawMessage) (mcp.ApprovalInfo, error) {
	r, err := a.approvals.Create(ctx, approvals.Request{
		AgentID:    agentID,
		Capability: capability,
		Action:     action,
	})
	if err != nil {
		return mcp.ApprovalInfo{}, err
	}
	return approvalToInfo(r), nil
}

func (a *controlPlaneAdapter) GetApproval(ctx context.Context, requestID string) (mcp.ApprovalInfo, error) {
	r, err := a.approvals.Get(ctx, requestID)
	if err != nil {
		return mcp.ApprovalInfo{}, err
	}
	return approvalToInfo(*r), nil
}

func (a *controlPlaneAdapter) CreateTask(ctx context.Context, agentID, capability string, input json.RawMessage) (mcp.TaskInfo, error) {
	t, err := a.tasks.Create(ctx, tasks.Task{
		AgentID:    agentID,
		Capability: capability,
		Input:      input,
	})
	if err != nil {
		return mcp.TaskInfo{}, err
	}
	return taskToInfo(t, nil), nil
}

func (a *controlPlaneAdapter) GetTask(ctx context.Context, taskID string) (mcp.TaskInfo, error) {
	t, err := a.tasks.Get(ctx, taskID)
	if err != nil {
		return mcp.TaskInfo{}, err
	}
	evts, err := a.tasks.ListEvents(ctx, taskID)
	if err != nil {
		return mcp.TaskInfo{}, err
	}
	return taskToInfo(*t, evts), nil
}

func (a *controlPlaneAdapter) QueryActionLog(ctx context.Context, agentID string, limit int) ([]mcp.ActionInfo, error) {
	if limit <= 0 {
		limit = 50
	}
	acts, err := a.actions.Query(ctx, actions.QueryFilter{AgentID: agentID, Limit: limit})
	if err != nil {
		return nil, err
	}
	out := make([]mcp.ActionInfo, len(acts))
	for i, act := range acts {
		out[i] = mcp.ActionInfo{
			ActionID:       act.ActionID,
			AgentID:        act.AgentID,
			Capability:     act.Capability,
			PolicyDecision: act.PolicyDecision,
			PolicyReason:   act.PolicyReason,
			GrantID:        act.GrantID,
			ExecutedAt:     act.ExecutedAt,
		}
	}
	return out, nil
}

func approvalToInfo(r approvals.Request) mcp.ApprovalInfo {
	return mcp.ApprovalInfo{
		RequestID:   r.RequestID,
		AgentID:     r.AgentID,
		Capability:  r.Capability,
		Action:      r.Action,
		Status:      r.Status,
		RequestedAt: r.RequestedAt,
		DecidedAt:   r.DecidedAt,
		DecidedBy:   r.DecidedBy,
		Decision:    r.Decision,
		Reason:      r.Reason,
	}
}

func taskToInfo(t tasks.Task, evts []tasks.TaskEvent) mcp.TaskInfo {
	info := mcp.TaskInfo{
		TaskID:     t.TaskID,
		AgentID:    t.AgentID,
		State:      t.State,
		Capability: t.Capability,
		Input:      t.Input,
		Output:     t.Output,
		CreatedAt:  t.CreatedAt,
	}
	if len(evts) > 0 {
		info.Events = make([]mcp.TaskEventInfo, len(evts))
		for i, e := range evts {
			info.Events[i] = mcp.TaskEventInfo{
				EventID:   e.EventID,
				EventType: e.EventType,
				Payload:   e.Payload,
				CreatedAt: e.CreatedAt,
			}
		}
	}
	return info
}
