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

package bridge

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/BubbleFish-Nexus/internal/a2a/registry"
	"github.com/BubbleFish-Nexus/internal/a2a/server"
)

// HandleA2AListAgents lists registered agents, optionally filtered by status.
func (b *Bridge) HandleA2AListAgents(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	var filter registry.ListFilter
	if status, ok := args["status"].(string); ok {
		filter.Status = status
	}

	agents, err := b.registry.List(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("bridge: list agents: %w", err)
	}

	results := make([]map[string]interface{}, 0, len(agents))
	for _, ag := range agents {
		entry := map[string]interface{}{
			"agent_id":     ag.AgentID,
			"name":         ag.Name,
			"display_name": ag.DisplayName,
			"status":       ag.Status,
		}

		// Include skill summaries.
		if len(ag.AgentCard.Skills) > 0 {
			skills := make([]string, 0, len(ag.AgentCard.Skills))
			for _, s := range ag.AgentCard.Skills {
				skills = append(skills, s.Name)
			}
			entry["skills"] = skills
		}

		results = append(results, entry)
	}

	return map[string]interface{}{
		"agents": results,
		"count":  len(results),
	}, nil
}

// HandleA2ADescribeAgent returns detailed information about a specific agent.
func (b *Bridge) HandleA2ADescribeAgent(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentName, ok := args["agent"].(string)
	if !ok || agentName == "" {
		return nil, fmt.Errorf("bridge: missing required field 'agent'")
	}

	agent, err := b.lookupAgent(ctx, agentName)
	if err != nil {
		return nil, err
	}

	card := agent.AgentCard
	result := map[string]interface{}{
		"agent_id":         agent.AgentID,
		"name":             agent.Name,
		"display_name":     agent.DisplayName,
		"status":           agent.Status,
		"description":      card.Description,
		"version":          card.Version,
		"protocol_version": card.ProtocolVersion,
		"capabilities": map[string]interface{}{
			"streaming":          card.Capabilities.Streaming,
			"push_notifications": card.Capabilities.PushNotifications,
			"state_transitions":  card.Capabilities.StateTransitions,
		},
	}

	if len(card.Skills) > 0 {
		skills := make([]map[string]interface{}, 0, len(card.Skills))
		for _, s := range card.Skills {
			skill := map[string]interface{}{
				"id":          s.ID,
				"name":        s.Name,
				"description": s.Description,
				"destructive": s.Destructive,
			}
			if len(s.Tags) > 0 {
				skill["tags"] = s.Tags
			}
			skills = append(skills, skill)
		}
		result["skills"] = skills
	}

	return result, nil
}

// HandleA2ASendToAgent sends a message to a target agent and returns the task result.
// This is the primary bridge function: MCP -> NA2A -> remote agent -> MCP.
func (b *Bridge) HandleA2ASendToAgent(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentName, ok := args["agent"].(string)
	if !ok || agentName == "" {
		return nil, fmt.Errorf("bridge: missing required field 'agent'")
	}

	// Look up the target agent.
	agent, err := b.lookupAgent(ctx, agentName)
	if err != nil {
		return nil, err
	}

	// Derive source identity from MCP context.
	clientName := clientNameFromCtx(ctx)
	sourceID := DeriveIdentity(clientName, "", "")

	// Convert MCP args to NA2A message.
	msg, skill, cfg, err := MCPToNA2A(args, sourceID)
	if err != nil {
		return nil, err
	}

	// Default to blocking if not specified.
	if _, hasBlocking := args["blocking"]; !hasBlocking {
		cfg.Blocking = true
	}

	// Run governance check.
	var requiredCaps []string
	var destructive bool
	if skill != "" {
		for _, s := range agent.AgentCard.Skills {
			if s.Name == skill || s.ID == skill {
				requiredCaps = s.RequiredCapabilities
				destructive = s.Destructive
				break
			}
		}
	}

	govResult, err := b.governance.Decide(ctx, server.GovernanceReq{
		SourceAgentID:        sourceID,
		TargetAgentID:        agent.AgentID,
		Skill:                skill,
		RequiredCapabilities: requiredCaps,
		Destructive:          destructive,
	})
	if err != nil {
		return nil, fmt.Errorf("bridge: governance error: %w", err)
	}

	if govResult.Decision == "deny" {
		return nil, fmt.Errorf("bridge: governance denied: %s", govResult.Reason)
	}
	if govResult.Decision == "escalate" {
		return map[string]interface{}{
			"status":      "escalated",
			"reason":      govResult.Reason,
			"audit_id":    govResult.AuditID,
			"requires":    "human approval",
			"instruction": "Use a2a_list_pending_approvals to check approval status.",
		}, nil
	}

	// Audit the send event.
	if b.auditSink != nil {
		b.auditSink.LogTaskEvent(ctx, "", "bridge.send", map[string]interface{}{
			"source":   sourceID,
			"target":   agent.AgentID,
			"skill":    skill,
			"grant_id": govResult.GrantID,
			"audit_id": govResult.AuditID,
		})
	}

	// Get client from pool and send.
	c, err := b.clientPool.Get(ctx, *agent)
	if err != nil {
		return nil, fmt.Errorf("bridge: connect to %s: %w", agent.Name, err)
	}

	task, err := c.SendMessage(ctx, msg, skill, cfg)
	if err != nil {
		return nil, fmt.Errorf("bridge: send to %s: %w", agent.Name, err)
	}

	return NA2AToMCP(task), nil
}

// HandleA2AStreamToAgent sends a streaming message to a target agent.
func (b *Bridge) HandleA2AStreamToAgent(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	agentName, ok := args["agent"].(string)
	if !ok || agentName == "" {
		return nil, fmt.Errorf("bridge: missing required field 'agent'")
	}

	agent, err := b.lookupAgent(ctx, agentName)
	if err != nil {
		return nil, err
	}

	clientName := clientNameFromCtx(ctx)
	sourceID := DeriveIdentity(clientName, "", "")

	msg, skill, cfg, err := MCPToNA2A(args, sourceID)
	if err != nil {
		return nil, err
	}

	c, err := b.clientPool.Get(ctx, *agent)
	if err != nil {
		return nil, fmt.Errorf("bridge: connect to %s: %w", agent.Name, err)
	}

	events, err := c.StreamMessage(ctx, msg, skill, cfg)
	if err != nil {
		return nil, fmt.Errorf("bridge: stream to %s: %w", agent.Name, err)
	}

	// Collect all events into a result. In a real streaming MCP implementation,
	// these would be streamed progressively. For now we collect them.
	var collected []map[string]interface{}
	for ev := range events {
		collected = append(collected, map[string]interface{}{
			"kind":    ev.Kind,
			"task_id": ev.TaskID,
			"payload": json.RawMessage(ev.Payload),
		})
	}

	return map[string]interface{}{
		"events": collected,
		"count":  len(collected),
	}, nil
}

// HandleA2AGetTask retrieves a task by ID from the agent that owns it.
func (b *Bridge) HandleA2AGetTask(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	taskID, ok := args["task_id"].(string)
	if !ok || taskID == "" {
		return nil, fmt.Errorf("bridge: missing required field 'task_id'")
	}

	agentName, _ := args["agent"].(string)
	if agentName == "" {
		return nil, fmt.Errorf("bridge: 'agent' is required to look up a task")
	}

	agent, err := b.lookupAgent(ctx, agentName)
	if err != nil {
		return nil, err
	}

	includeHistory, _ := args["include_history"].(bool)

	c, err := b.clientPool.Get(ctx, *agent)
	if err != nil {
		return nil, fmt.Errorf("bridge: connect to %s: %w", agent.Name, err)
	}

	task, err := c.GetTask(ctx, taskID, includeHistory)
	if err != nil {
		return nil, fmt.Errorf("bridge: get task from %s: %w", agent.Name, err)
	}

	return NA2AToMCP(task), nil
}

// HandleA2AResumeTask resumes a task that is in the input-required state.
func (b *Bridge) HandleA2AResumeTask(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	taskID, ok := args["task_id"].(string)
	if !ok || taskID == "" {
		return nil, fmt.Errorf("bridge: missing required field 'task_id'")
	}

	agentName, _ := args["agent"].(string)
	if agentName == "" {
		return nil, fmt.Errorf("bridge: 'agent' is required to resume a task")
	}

	agent, err := b.lookupAgent(ctx, agentName)
	if err != nil {
		return nil, err
	}

	clientName := clientNameFromCtx(ctx)
	sourceID := DeriveIdentity(clientName, "", "")

	msg, skill, cfg, err := MCPToNA2A(args, sourceID)
	if err != nil {
		return nil, err
	}

	// Set the context ID to the task ID for task resumption.
	msg.ContextID = taskID

	c, err := b.clientPool.Get(ctx, *agent)
	if err != nil {
		return nil, fmt.Errorf("bridge: connect to %s: %w", agent.Name, err)
	}

	task, err := c.SendMessage(ctx, msg, skill, cfg)
	if err != nil {
		return nil, fmt.Errorf("bridge: resume task on %s: %w", agent.Name, err)
	}

	return NA2AToMCP(task), nil
}

// HandleA2ACancelTask cancels a running task on a remote agent.
func (b *Bridge) HandleA2ACancelTask(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	taskID, ok := args["task_id"].(string)
	if !ok || taskID == "" {
		return nil, fmt.Errorf("bridge: missing required field 'task_id'")
	}

	agentName, _ := args["agent"].(string)
	if agentName == "" {
		return nil, fmt.Errorf("bridge: 'agent' is required to cancel a task")
	}

	agent, err := b.lookupAgent(ctx, agentName)
	if err != nil {
		return nil, err
	}

	reason, _ := args["reason"].(string)

	c, err := b.clientPool.Get(ctx, *agent)
	if err != nil {
		return nil, fmt.Errorf("bridge: connect to %s: %w", agent.Name, err)
	}

	task, err := c.CancelTask(ctx, taskID, reason)
	if err != nil {
		return nil, fmt.Errorf("bridge: cancel task on %s: %w", agent.Name, err)
	}

	return NA2AToMCP(task), nil
}

// HandleA2AListPendingApprovals lists all governance approvals awaiting decision.
func (b *Bridge) HandleA2AListPendingApprovals(ctx context.Context, _ map[string]interface{}) (interface{}, error) {
	approvals, err := b.governance.ListApprovals(ctx)
	if err != nil {
		return nil, fmt.Errorf("bridge: list approvals: %w", err)
	}

	results := make([]map[string]interface{}, 0, len(approvals))
	for _, ap := range approvals {
		results = append(results, map[string]interface{}{
			"approval_id":    ap.ApprovalID,
			"source_agent":   ap.SourceAgentID,
			"target_agent":   ap.TargetAgentID,
			"skill":          ap.Skill,
			"reason":         ap.Reason,
			"created_at":     ap.CreatedAt,
		})
	}

	return map[string]interface{}{
		"approvals": results,
		"count":     len(results),
	}, nil
}

// HandleA2AListGrants lists all governance grants.
func (b *Bridge) HandleA2AListGrants(ctx context.Context, _ map[string]interface{}) (interface{}, error) {
	grants, err := b.governance.ListGrants(ctx)
	if err != nil {
		return nil, fmt.Errorf("bridge: list grants: %w", err)
	}

	results := make([]map[string]interface{}, 0, len(grants))
	for _, g := range grants {
		results = append(results, map[string]interface{}{
			"grant_id":     g.GrantID,
			"source_agent": g.SourceAgentID,
			"target_agent": g.TargetAgentID,
			"decision":     g.Decision,
			"reason":       g.Reason,
			"created_at":   g.CreatedAt,
		})
	}

	return map[string]interface{}{
		"grants": results,
		"count":  len(results),
	}, nil
}

// lookupAgent finds an agent by name or ID. It tries name first, then ID.
func (b *Bridge) lookupAgent(ctx context.Context, nameOrID string) (*registry.RegisteredAgent, error) {
	agent, err := b.registry.GetByName(ctx, nameOrID)
	if err == nil {
		return agent, nil
	}

	agent, err = b.registry.Get(ctx, nameOrID)
	if err != nil {
		return nil, fmt.Errorf("bridge: agent %q not found", nameOrID)
	}
	return agent, nil
}
