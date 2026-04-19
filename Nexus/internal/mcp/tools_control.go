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

package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// ControlPlaneProvider is the interface the MCP server uses to execute governed
// control-plane tool calls. Implemented by *controlPlaneAdapter in the daemon.
// Nil means the control plane is disabled; all control tools return an error.
type ControlPlaneProvider interface {
	// EvaluatePolicy runs the MT.3 policy engine for (agentID, capability, action).
	// The decision is recorded in the action_log by the engine; callers must not
	// record it again.
	EvaluatePolicy(ctx context.Context, agentID, capability string, action json.RawMessage) ControlDecision

	// ListGrants returns all grants for agentID (newest first, capped at 100).
	ListGrants(ctx context.Context, agentID string) ([]GrantInfo, error)

	// RequestApproval creates a pending approval request for agentID to exercise
	// capability with the given action payload.
	RequestApproval(ctx context.Context, agentID, capability string, action json.RawMessage) (ApprovalInfo, error)

	// GetApproval retrieves an approval request by ID.
	GetApproval(ctx context.Context, requestID string) (ApprovalInfo, error)

	// CreateTask creates a new governed task. Policy must have been evaluated
	// by the caller before invoking this.
	CreateTask(ctx context.Context, agentID, capability string, input json.RawMessage) (TaskInfo, error)

	// GetTask retrieves a task and its full event log by ID.
	GetTask(ctx context.Context, taskID string) (TaskInfo, error)

	// QueryActionLog returns recent policy decisions for agentID (newest first).
	QueryActionLog(ctx context.Context, agentID string, limit int) ([]ActionInfo, error)
}

// ControlDecision mirrors policy.Decision to avoid circular imports.
type ControlDecision struct {
	Allowed    bool
	Reason     string
	GrantID    string
	ApprovalID string
}

// GrantInfo is the MCP-layer view of a grants.Grant.
type GrantInfo struct {
	GrantID    string          `json:"grant_id"`
	AgentID    string          `json:"agent_id"`
	Capability string          `json:"capability"`
	Scope      json.RawMessage `json:"scope,omitempty"`
	GrantedBy  string          `json:"granted_by"`
	GrantedAt  time.Time       `json:"granted_at"`
	ExpiresAt  *time.Time      `json:"expires_at,omitempty"`
}

// ApprovalInfo is the MCP-layer view of an approvals.Request.
type ApprovalInfo struct {
	RequestID   string          `json:"request_id"`
	AgentID     string          `json:"agent_id"`
	Capability  string          `json:"capability"`
	Action      json.RawMessage `json:"action,omitempty"`
	Status      string          `json:"status"`
	RequestedAt time.Time       `json:"requested_at"`
	DecidedAt   *time.Time      `json:"decided_at,omitempty"`
	DecidedBy   string          `json:"decided_by,omitempty"`
	Decision    string          `json:"decision,omitempty"`
	Reason      string          `json:"reason,omitempty"`
}

// TaskInfo is the MCP-layer view of a tasks.Task plus its event log.
type TaskInfo struct {
	TaskID     string          `json:"task_id"`
	AgentID    string          `json:"agent_id"`
	State      string          `json:"state"`
	Capability string          `json:"capability,omitempty"`
	Input      json.RawMessage `json:"input,omitempty"`
	Output     json.RawMessage `json:"output,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
	Events     []TaskEventInfo `json:"events,omitempty"`
}

// TaskEventInfo is the MCP-layer view of a tasks.TaskEvent.
type TaskEventInfo struct {
	EventID   string          `json:"event_id"`
	EventType string          `json:"event_type"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

// ActionInfo is the MCP-layer view of an actions.Action.
type ActionInfo struct {
	ActionID       string    `json:"action_id"`
	AgentID        string    `json:"agent_id"`
	Capability     string    `json:"capability"`
	PolicyDecision string    `json:"policy_decision"`
	PolicyReason   string    `json:"policy_reason,omitempty"`
	GrantID        string    `json:"grant_id,omitempty"`
	ExecutedAt     time.Time `json:"executed_at"`
}

// SetControlPlane configures the governed control-plane tool provider.
// When non-nil, the 6 nexus_ governance tools are advertised and dispatched.
// Must be called before or shortly after Start().
func (s *Server) SetControlPlane(p ControlPlaneProvider) {
	s.controlPlane = p
}

// controlToolDefs returns the 6 governed MCP tool definitions.
func controlToolDefs() []toolDef {
	return []toolDef{
		{
			Name:        "nexus_grant_list",
			Description: "List all grants for the calling agent.",
			InputSchema: inputSchema{
				Type:       "object",
				Properties: map[string]propDef{},
			},
		},
		{
			Name:        "nexus_approval_request",
			Description: "Submit a human-approval request for a governed capability.",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]propDef{
					"capability": {Type: "string", Description: "Capability name requiring approval."},
					"action":     {Type: "object", Description: "Action payload to be approved."},
				},
				Required: []string{"capability", "action"},
			},
		},
		{
			Name:        "nexus_approval_status",
			Description: "Check the status of a previously submitted approval request.",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]propDef{
					"request_id": {Type: "string", Description: "Approval request ID returned by nexus_approval_request."},
				},
				Required: []string{"request_id"},
			},
		},
		{
			Name:        "nexus_task_create",
			Description: "Create a governed task. Policy is evaluated against the task capability, not the tool name.",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]propDef{
					"capability": {Type: "string", Description: "Capability the task exercises (policy-evaluated)."},
					"input":      {Type: "object", Description: "Task input payload."},
				},
				Required: []string{"capability"},
			},
		},
		{
			Name:        "nexus_task_status",
			Description: "Retrieve a governed task and its event log.",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]propDef{
					"task_id": {Type: "string", Description: "Task ID returned by nexus_task_create."},
				},
				Required: []string{"task_id"},
			},
		},
		{
			Name:        "nexus_action_log",
			Description: "Query the policy action log for the calling agent (newest first).",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]propDef{
					"limit": {Type: "integer", Description: "Max entries to return (default 50, 0 = default)."},
				},
			},
		},
	}
}

// callNexusGrantList handles the nexus_grant_list MCP tool.
func (s *Server) callNexusGrantList(w http.ResponseWriter, r *http.Request, req rpcRequest) {
	if s.controlPlane == nil {
		s.writeToolError(w, r, req.ID, "control plane not enabled")
		return
	}
	agentID := r.Header.Get("X-Agent-ID")
	if agentID == "" {
		s.writeToolError(w, r, req.ID, "X-Agent-ID header required")
		return
	}
	d := s.controlPlane.EvaluatePolicy(r.Context(), agentID, "nexus_grant_list", nil)
	if !d.Allowed {
		s.writeToolError(w, r, req.ID, "policy denied: "+d.Reason)
		return
	}
	grantList, err := s.controlPlane.ListGrants(r.Context(), agentID)
	if err != nil {
		s.writeToolError(w, r, req.ID, "list grants failed: "+err.Error())
		return
	}
	out, _ := json.Marshal(map[string]interface{}{"grants": grantList, "count": len(grantList)})
	s.writeRPCResult(w, r, req.ID, toolCallResult{Content: []contentBlock{{Type: "text", Text: string(out)}}})
}

// callNexusApprovalRequest handles the nexus_approval_request MCP tool.
func (s *Server) callNexusApprovalRequest(w http.ResponseWriter, r *http.Request, req rpcRequest, args json.RawMessage) {
	if s.controlPlane == nil {
		s.writeToolError(w, r, req.ID, "control plane not enabled")
		return
	}
	agentID := r.Header.Get("X-Agent-ID")
	if agentID == "" {
		s.writeToolError(w, r, req.ID, "X-Agent-ID header required")
		return
	}
	var a struct {
		Capability string          `json:"capability"`
		Action     json.RawMessage `json:"action"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &a); err != nil {
			s.writeRPCError(w, r, req.ID, rpcInvalidParams, "invalid nexus_approval_request arguments: "+err.Error())
			return
		}
	}
	if a.Capability == "" {
		s.writeToolError(w, r, req.ID, "nexus_approval_request requires 'capability'")
		return
	}
	if len(a.Action) == 0 {
		s.writeToolError(w, r, req.ID, "nexus_approval_request requires 'action'")
		return
	}
	d := s.controlPlane.EvaluatePolicy(r.Context(), agentID, "nexus_approval_request", args)
	if !d.Allowed {
		s.writeToolError(w, r, req.ID, "policy denied: "+d.Reason)
		return
	}
	info, err := s.controlPlane.RequestApproval(r.Context(), agentID, a.Capability, a.Action)
	if err != nil {
		s.writeToolError(w, r, req.ID, "request approval failed: "+err.Error())
		return
	}
	out, _ := json.Marshal(info)
	s.writeRPCResult(w, r, req.ID, toolCallResult{Content: []contentBlock{{Type: "text", Text: string(out)}}})
}

// callNexusApprovalStatus handles the nexus_approval_status MCP tool.
func (s *Server) callNexusApprovalStatus(w http.ResponseWriter, r *http.Request, req rpcRequest, args json.RawMessage) {
	if s.controlPlane == nil {
		s.writeToolError(w, r, req.ID, "control plane not enabled")
		return
	}
	agentID := r.Header.Get("X-Agent-ID")
	if agentID == "" {
		s.writeToolError(w, r, req.ID, "X-Agent-ID header required")
		return
	}
	var a struct {
		RequestID string `json:"request_id"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &a); err != nil {
			s.writeRPCError(w, r, req.ID, rpcInvalidParams, "invalid nexus_approval_status arguments: "+err.Error())
			return
		}
	}
	if a.RequestID == "" {
		s.writeToolError(w, r, req.ID, "nexus_approval_status requires 'request_id'")
		return
	}
	d := s.controlPlane.EvaluatePolicy(r.Context(), agentID, "nexus_approval_status", args)
	if !d.Allowed {
		s.writeToolError(w, r, req.ID, "policy denied: "+d.Reason)
		return
	}
	info, err := s.controlPlane.GetApproval(r.Context(), a.RequestID)
	if err != nil {
		s.writeToolError(w, r, req.ID, "get approval failed: "+err.Error())
		return
	}
	if info.AgentID != agentID {
		s.writeToolError(w, r, req.ID, "approval not found")
		return
	}
	out, _ := json.Marshal(info)
	s.writeRPCResult(w, r, req.ID, toolCallResult{Content: []contentBlock{{Type: "text", Text: string(out)}}})
}

// callNexusTaskCreate handles the nexus_task_create MCP tool.
// Policy is evaluated against the TASK's capability, not "nexus_task_create" —
// this is the core governed operation: the agent needs a grant for whatever
// capability the task exercises.
func (s *Server) callNexusTaskCreate(w http.ResponseWriter, r *http.Request, req rpcRequest, args json.RawMessage) {
	if s.controlPlane == nil {
		s.writeToolError(w, r, req.ID, "control plane not enabled")
		return
	}
	agentID := r.Header.Get("X-Agent-ID")
	if agentID == "" {
		s.writeToolError(w, r, req.ID, "X-Agent-ID header required")
		return
	}
	var a struct {
		Capability string          `json:"capability"`
		Input      json.RawMessage `json:"input"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &a); err != nil {
			s.writeRPCError(w, r, req.ID, rpcInvalidParams, "invalid nexus_task_create arguments: "+err.Error())
			return
		}
	}
	if a.Capability == "" {
		s.writeToolError(w, r, req.ID, "nexus_task_create requires 'capability'")
		return
	}
	d := s.controlPlane.EvaluatePolicy(r.Context(), agentID, a.Capability, a.Input)
	if !d.Allowed {
		s.writeToolError(w, r, req.ID, "policy denied: "+d.Reason)
		return
	}
	task, err := s.controlPlane.CreateTask(r.Context(), agentID, a.Capability, a.Input)
	if err != nil {
		s.writeToolError(w, r, req.ID, "create task failed: "+err.Error())
		return
	}
	out, _ := json.Marshal(task)
	s.writeRPCResult(w, r, req.ID, toolCallResult{Content: []contentBlock{{Type: "text", Text: string(out)}}})
}

// callNexusTaskStatus handles the nexus_task_status MCP tool.
func (s *Server) callNexusTaskStatus(w http.ResponseWriter, r *http.Request, req rpcRequest, args json.RawMessage) {
	if s.controlPlane == nil {
		s.writeToolError(w, r, req.ID, "control plane not enabled")
		return
	}
	agentID := r.Header.Get("X-Agent-ID")
	if agentID == "" {
		s.writeToolError(w, r, req.ID, "X-Agent-ID header required")
		return
	}
	var a struct {
		TaskID string `json:"task_id"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &a); err != nil {
			s.writeRPCError(w, r, req.ID, rpcInvalidParams, "invalid nexus_task_status arguments: "+err.Error())
			return
		}
	}
	if a.TaskID == "" {
		s.writeToolError(w, r, req.ID, "nexus_task_status requires 'task_id'")
		return
	}
	d := s.controlPlane.EvaluatePolicy(r.Context(), agentID, "nexus_task_status", args)
	if !d.Allowed {
		s.writeToolError(w, r, req.ID, "policy denied: "+d.Reason)
		return
	}
	task, err := s.controlPlane.GetTask(r.Context(), a.TaskID)
	if err != nil {
		s.writeToolError(w, r, req.ID, "get task failed: "+err.Error())
		return
	}
	if task.AgentID != agentID {
		s.writeToolError(w, r, req.ID, "task not found")
		return
	}
	out, _ := json.Marshal(task)
	s.writeRPCResult(w, r, req.ID, toolCallResult{Content: []contentBlock{{Type: "text", Text: string(out)}}})
}

// callNexusActionLog handles the nexus_action_log MCP tool.
func (s *Server) callNexusActionLog(w http.ResponseWriter, r *http.Request, req rpcRequest, args json.RawMessage) {
	if s.controlPlane == nil {
		s.writeToolError(w, r, req.ID, "control plane not enabled")
		return
	}
	agentID := r.Header.Get("X-Agent-ID")
	if agentID == "" {
		s.writeToolError(w, r, req.ID, "X-Agent-ID header required")
		return
	}
	var a struct {
		Limit int `json:"limit"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &a); err != nil {
			s.writeRPCError(w, r, req.ID, rpcInvalidParams, "invalid nexus_action_log arguments: "+err.Error())
			return
		}
	}
	limit := a.Limit
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	d := s.controlPlane.EvaluatePolicy(r.Context(), agentID, "nexus_action_log", args)
	if !d.Allowed {
		s.writeToolError(w, r, req.ID, "policy denied: "+d.Reason)
		return
	}
	entries, err := s.controlPlane.QueryActionLog(r.Context(), agentID, limit)
	if err != nil {
		s.writeToolError(w, r, req.ID, "action log query failed: "+err.Error())
		return
	}
	out, _ := json.Marshal(map[string]interface{}{"entries": entries, "count": len(entries)})
	s.writeRPCResult(w, r, req.ID, toolCallResult{Content: []contentBlock{{Type: "text", Text: string(out)}}})
}
