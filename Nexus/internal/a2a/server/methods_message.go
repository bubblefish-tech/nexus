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

package server

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/bubblefish-tech/nexus/internal/a2a"
	"github.com/bubblefish-tech/nexus/internal/a2a/jsonrpc"
)

// messageSendParams is the parameter object for message/send.
type messageSendParams struct {
	Message       a2a.Message        `json:"message"`
	TaskID        *string            `json:"taskId,omitempty"`
	Skill         string             `json:"skill"`
	Configuration *messageSendConfig `json:"configuration,omitempty"`
}

// messageSendConfig is optional configuration for message/send.
type messageSendConfig struct {
	Blocking  bool `json:"blocking"`
	TimeoutMs int  `json:"timeoutMs,omitempty"`
}

// handleMessageSend processes the message/send JSON-RPC method.
func (s *Server) handleMessageSend(ctx context.Context, method string, params json.RawMessage) (interface{}, *jsonrpc.ErrorObject) {
	var p messageSendParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeInvalidParams,
			Message: fmt.Sprintf("invalid params: %v", err),
		}
	}

	// Validate message.
	if vErr := p.Message.Validate(); vErr != nil {
		return nil, jsonrpc.FromA2AError(vErr)
	}

	// Validate skill name is provided.
	if p.Skill == "" {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeInvalidParams,
			Message: "skill is required",
		}
	}

	// Look up skill.
	if s.skillRegistry == nil {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeInternalError,
			Message: "no skill registry configured",
		}
	}
	skill, ok := s.skillRegistry.GetSkill(p.Skill)
	if !ok {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeSkillNotFound,
			Message: fmt.Sprintf("skill %q not found", p.Skill),
		}
	}

	// Run governance check.
	if s.governance != nil {
		sourceAgent, _ := ctx.Value(CtxKeySourceAgent).(string)
		gReq := GovernanceReq{
			SourceAgentID:        sourceAgent,
			TargetAgentID:        s.agentCard.Name,
			Skill:                p.Skill,
			RequiredCapabilities: skill.RequiredCapabilities,
			Destructive:          skill.Destructive,
		}
		decision, err := s.governance.Decide(ctx, gReq)
		if err != nil {
			s.logger.ErrorContext(ctx, "governance error", "error", err)
			return nil, &jsonrpc.ErrorObject{
				Code:    a2a.CodeInternalError,
				Message: "governance engine error",
			}
		}
		switch decision.Decision {
		case "deny":
			return nil, &jsonrpc.ErrorObject{
				Code:    a2a.CodePermissionDenied,
				Message: fmt.Sprintf("governance denied: %s", decision.Reason),
			}
		case "escalate":
			return nil, &jsonrpc.ErrorObject{
				Code:    a2a.CodeApprovalRequired,
				Message: fmt.Sprintf("approval required: %s", decision.Reason),
				Data:    map[string]string{"auditId": decision.AuditID},
			}
		case "allow":
			// proceed
		default:
			return nil, &jsonrpc.ErrorObject{
				Code:    a2a.CodeInternalError,
				Message: fmt.Sprintf("unknown governance decision %q", decision.Decision),
			}
		}
	}

	// Create or retrieve task.
	task := a2a.NewTask()
	if p.TaskID != nil && *p.TaskID != "" {
		task.TaskID = *p.TaskID
	}
	task.ContextID = p.Message.ContextID

	if s.taskStore != nil {
		if err := s.taskStore.CreateTask(ctx, &task); err != nil {
			s.logger.ErrorContext(ctx, "task store create error", "error", err)
			return nil, &jsonrpc.ErrorObject{
				Code:    a2a.CodeInternalError,
				Message: "failed to create task",
			}
		}
	}

	// Add message to history.
	if s.taskStore != nil {
		if err := s.taskStore.AddHistory(ctx, task.TaskID, p.Message); err != nil {
			s.logger.ErrorContext(ctx, "task store add history error", "error", err)
		}
	}
	task.History = append(task.History, p.Message)

	// Update task to working.
	task.Status = a2a.TaskStatus{State: a2a.TaskStateWorking, Timestamp: a2a.Now()}
	if s.taskStore != nil {
		if err := s.taskStore.UpdateTaskStatus(ctx, task.TaskID, task.Status); err != nil {
			s.logger.ErrorContext(ctx, "task store update status error", "error", err)
		}
	}

	// Audit event.
	if s.auditSink != nil {
		_ = s.auditSink.LogTaskEvent(ctx, task.TaskID, "task.started", map[string]string{"skill": p.Skill})
	}

	// Execute skill.
	if s.skillExecutor != nil {
		input, files := extractInputAndFiles(p.Message)
		resultData, resultFiles, err := s.skillExecutor.Execute(ctx, p.Skill, input, files)
		if err != nil {
			task.Status = a2a.TaskStatus{State: a2a.TaskStateFailed, Timestamp: a2a.Now()}
			if s.taskStore != nil {
				_ = s.taskStore.UpdateTaskStatus(ctx, task.TaskID, task.Status)
			}
			if s.auditSink != nil {
				_ = s.auditSink.LogTaskEvent(ctx, task.TaskID, "task.failed", map[string]string{"error": err.Error()})
			}
			return nil, &jsonrpc.ErrorObject{
				Code:    a2a.CodeInternalError,
				Message: fmt.Sprintf("skill execution failed: %v", err),
			}
		}

		// Build artifact from result.
		var parts []a2a.Part
		if resultData != nil {
			parts = append(parts, *resultData)
		}
		for _, f := range resultFiles {
			parts = append(parts, f)
		}
		if len(parts) > 0 {
			art := a2a.NewArtifact(p.Skill+"-result", parts...)
			task.Artifacts = append(task.Artifacts, art)
			if s.taskStore != nil {
				_ = s.taskStore.AddArtifact(ctx, task.TaskID, art)
			}
		}
	}

	// Mark task completed.
	task.Status = a2a.TaskStatus{State: a2a.TaskStateCompleted, Timestamp: a2a.Now()}
	if s.taskStore != nil {
		_ = s.taskStore.UpdateTaskStatus(ctx, task.TaskID, task.Status)
	}
	if s.auditSink != nil {
		_ = s.auditSink.LogTaskEvent(ctx, task.TaskID, "task.completed", nil)
	}

	return &task, nil
}

// handleMessageStream processes the message/stream JSON-RPC method.
// For now this behaves the same as message/send but indicates streaming.
// Full SSE wiring is in A2A.6.
func (s *Server) handleMessageStream(ctx context.Context, method string, params json.RawMessage) (interface{}, *jsonrpc.ErrorObject) {
	result, errObj := s.handleMessageSend(ctx, method, params)
	if errObj != nil {
		return nil, errObj
	}
	type streamResult struct {
		Task      *a2a.Task `json:"task"`
		Streaming bool      `json:"streaming"`
	}
	task, _ := result.(*a2a.Task)
	return &streamResult{Task: task, Streaming: true}, nil
}

// extractInputAndFiles separates a message's parts into a DataPart and FileParts.
func extractInputAndFiles(msg a2a.Message) (*a2a.DataPart, []a2a.FilePart) {
	var input *a2a.DataPart
	var files []a2a.FilePart
	for _, pw := range msg.Parts {
		switch p := pw.Part.(type) {
		case a2a.DataPart:
			cp := p
			input = &cp
		case a2a.FilePart:
			files = append(files, p)
		case a2a.TextPart:
			data, _ := json.Marshal(map[string]string{"text": p.Text})
			dp := a2a.NewDataPart(data)
			input = &dp
		}
	}
	return input, files
}
