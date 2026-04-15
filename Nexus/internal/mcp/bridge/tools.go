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
	"time"

	"github.com/BubbleFish-Nexus/internal/a2a/client"
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

	// Check if the agent uses tasks/send (OpenClaw-style) instead of
	// message/send (A2A v1.0). If so, send a simplified request with
	// the message as a plain string.
	if agentUsesTasksSend(agent) {
		return b.sendViaTasksSend(ctx, c, args, agent)
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

// agentUsesTasksSend returns true if the agent's card declares tasks/send
// (OpenClaw-style) as a supported method instead of message/send (A2A v1.0).
func agentUsesTasksSend(agent *registry.RegisteredAgent) bool {
	for _, m := range agent.AgentCard.Methods {
		if m == "tasks/send" {
			return true
		}
	}
	return false
}

// sendViaTasksSend dispatches a message using the OpenClaw tasks/send format.
// OpenClaw expects: {"message": "plain string"} as params.
//
// Flow: send → get taskId → poll tasks/get until status !== "pending" → return.
// If tasks/send returns a completed result directly (legacy sync mode), the
// response is used immediately without polling.
func (b *Bridge) sendViaTasksSend(ctx context.Context, c *client.Client, args map[string]interface{}, agent *registry.RegisteredAgent) (interface{}, error) {
	// Extract the message as a plain string.
	var messageStr string
	switch v := args["input"].(type) {
	case string:
		messageStr = v
	case map[string]interface{}:
		data, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("bridge: marshal input: %w", err)
		}
		messageStr = string(data)
	default:
		data, err := json.Marshal(args["input"])
		if err != nil {
			return nil, fmt.Errorf("bridge: marshal input: %w", err)
		}
		messageStr = string(data)
	}

	params := map[string]interface{}{
		"message": messageStr,
	}
	if skill, ok := args["skill"].(string); ok && skill != "" {
		params["agentId"] = skill
	}

	resp, err := c.Call(ctx, "tasks/send", params)
	if err != nil {
		return nil, fmt.Errorf("bridge: tasks/send to %s: %w", agent.Name, err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(resp.Result, &raw); err != nil {
		return nil, fmt.Errorf("bridge: unmarshal tasks/send result: %w", err)
	}

	// If the response is already complete (sync mode), return immediately.
	status, _ := raw["status"].(string)
	if status != "pending" && status != "running" {
		return transformTasksSendResponse(raw, agent.Name), nil
	}

	// Async mode: extract identifiers and poll tasks/get until done.
	// OpenClaw returns taskId (always) and optionally runId and sessionKey.
	taskID, _ := raw["taskId"].(string)
	sessionKey, _ := raw["sessionKey"].(string)
	runID, _ := raw["runId"].(string)

	if taskID == "" && sessionKey == "" && runID == "" {
		return nil, fmt.Errorf("bridge: tasks/send returned pending but no taskId, sessionKey, or runId")
	}

	b.logger.Info("bridge: polling for task completion",
		"agent", agent.Name,
		"taskId", taskID,
		"runId", runID,
		"sessionKey", sessionKey,
	)

	return b.pollTasksGet(ctx, c, agent, taskID, sessionKey, runID)
}

const (
	pollInterval   = 1500 * time.Millisecond
	pollMaxWait    = 120 * time.Second
)

// pollTasksGet polls tasks/get until the task is no longer pending/running.
func (b *Bridge) pollTasksGet(ctx context.Context, c *client.Client, agent *registry.RegisteredAgent, taskID, sessionKey, runID string) (interface{}, error) {
	deadline := time.Now().Add(pollMaxWait)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("bridge: context cancelled while polling %s: %w", agent.Name, ctx.Err())
		case <-ticker.C:
			if time.Now().After(deadline) {
				return map[string]interface{}{
					"agent":    agent.Name,
					"status":   "timeout",
					"taskId":   taskID,
					"runId":    runID,
					"response": "(timed out waiting for agent response)",
				}, nil
			}

			// Build poll params — use taskId first, fall back to sessionKey/runId.
			params := map[string]interface{}{}
			if taskID != "" {
				params["taskId"] = taskID
			}
			if sessionKey != "" {
				params["sessionKey"] = sessionKey
			}
			if runID != "" {
				params["runId"] = runID
			}

			resp, err := c.Call(ctx, "tasks/get", params)
			if err != nil {
				b.logger.Warn("bridge: poll error (retrying)",
					"agent", agent.Name,
					"error", err,
				)
				continue
			}

			var raw map[string]interface{}
			if err := json.Unmarshal(resp.Result, &raw); err != nil {
				continue
			}

			status, _ := raw["status"].(string)
			if status == "pending" || status == "running" {
				continue
			}

			// Terminal state — transform and return.
			return transformTasksSendResponse(raw, agent.Name), nil
		}
	}
}

// transformTasksSendResponse extracts a clean response from the OpenClaw
// tasks/send result. It finds the last assistant message's text content
// and returns a concise result suitable for an MCP tool response.
func transformTasksSendResponse(raw map[string]interface{}, agentName string) map[string]interface{} {
	result := map[string]interface{}{
		"agent":  agentName,
		"status": raw["status"],
	}

	if runID, ok := raw["runId"].(string); ok {
		result["runId"] = runID
	}
	if taskID, ok := raw["taskId"].(string); ok {
		result["taskId"] = taskID
	}
	if dur, ok := raw["durationMs"].(float64); ok {
		result["durationMs"] = dur
	}

	// New async format: result is a plain string at top level.
	if text, ok := raw["result"].(string); ok && text != "" {
		result["response"] = text
		return result
	}

	// Legacy sync format: walk messages backwards to find the last assistant text.
	messages, ok := raw["messages"].([]interface{})
	if !ok || len(messages) == 0 {
		result["response"] = "(no response)"
		return result
	}

	for i := len(messages) - 1; i >= 0; i-- {
		msg, ok := messages[i].(map[string]interface{})
		if !ok {
			continue
		}
		if msg["role"] != "assistant" {
			continue
		}

		// Check for error.
		if errMsg, ok := msg["errorMessage"].(string); ok && errMsg != "" {
			result["response"] = "(error: " + errMsg + ")"
			result["error"] = true
			return result
		}

		// Extract text parts from content array.
		content, ok := msg["content"].([]interface{})
		if !ok {
			continue
		}
		for _, part := range content {
			p, ok := part.(map[string]interface{})
			if !ok {
				continue
			}
			if p["type"] == "text" {
				if text, ok := p["text"].(string); ok {
					result["response"] = text
					return result
				}
			}
		}
	}

	result["response"] = "(no text in response)"
	return result
}
