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

// ClientPool resolves a remote agent by ID and dispatches a message/send to it.
// Implemented by client.Pool or a test double.
type ClientPool interface {
	// SendMessage dispatches a message to the named agent and returns the task.
	SendMessage(ctx context.Context, targetAgentID string, msg *a2a.Message, skill string) (*a2a.Task, error)
}

// WithClientPool sets the optional ClientPool for agent/invoke forwarding.
func WithClientPool(cp ClientPool) ServerOption {
	return func(s *Server) { s.clientPool = cp }
}

// invokeParams is the JSON-RPC params for agent/invoke.
type invokeParams struct {
	TargetAgentID string       `json:"targetAgentId"`
	Skill         string       `json:"skill,omitempty"`
	Message       *a2a.Message `json:"message"`
}

// handleAgentInvoke dispatches a bidirectional agent-to-agent invocation.
//
//  1. Parse params: targetAgentId, skill, message
//  2. Verify calling agent declared bidirectional (has agent/invoke in card skills or capabilities)
//  3. Check chain depth from governance extension
//  4. If depth >= maxChainDepth (default 4), fail with INTERNAL_ERROR
//  5. Run governance for (source=caller, target=target, cap=agent.invoke:<target>)
//  6. Dispatch via client pool (if available) or return METHOD_NOT_FOUND if no client wired
//  7. Return the final task
func (s *Server) handleAgentInvoke(ctx context.Context, _ string, params json.RawMessage) (interface{}, *jsonrpc.ErrorObject) {
	// Parse parameters.
	var p invokeParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeInvalidParams,
			Message: fmt.Sprintf("invalid params: %v", err),
		}
	}

	if p.TargetAgentID == "" {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeInvalidParams,
			Message: "targetAgentId is required",
		}
	}

	if p.Message == nil {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeInvalidParams,
			Message: "message is required",
		}
	}

	// Extract source agent from context.
	sourceAgent, _ := ctx.Value(CtxKeySourceAgent).(string)
	if sourceAgent == "" {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeUnauthenticated,
			Message: "source agent identity required for agent/invoke",
		}
	}

	// Check chain depth to prevent infinite loops.
	depth := ExtractChainDepth(p.Message)
	if depth >= DefaultMaxChainDepth {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeInternalError,
			Message: fmt.Sprintf("chain depth %d exceeds maximum %d", depth, DefaultMaxChainDepth),
		}
	}

	// Run governance check.
	if s.governance != nil {
		capability := "agent.invoke:" + p.TargetAgentID
		govReq := GovernanceReq{
			SourceAgentID:        sourceAgent,
			TargetAgentID:        p.TargetAgentID,
			Skill:                p.Skill,
			RequiredCapabilities: []string{capability},
		}

		result, err := s.governance.Decide(ctx, govReq)
		if err != nil {
			s.logger.Error("agent/invoke: governance error",
				"source", sourceAgent,
				"target", p.TargetAgentID,
				"error", err,
			)
			return nil, &jsonrpc.ErrorObject{
				Code:    a2a.CodeInternalError,
				Message: fmt.Sprintf("governance error: %v", err),
			}
		}

		switch result.Decision {
		case "deny":
			return nil, &jsonrpc.ErrorObject{
				Code:    a2a.CodePermissionDenied,
				Message: fmt.Sprintf("governance denied: %s", result.Reason),
			}
		case "escalate":
			return nil, &jsonrpc.ErrorObject{
				Code:    a2a.CodeApprovalRequired,
				Message: fmt.Sprintf("governance escalated: %s", result.Reason),
			}
		}
	}

	// Check if client pool is available.
	if s.clientPool == nil {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeMethodNotFound,
			Message: "agent/invoke: no client pool configured",
		}
	}

	// Increment chain depth before forwarding.
	fwdMsg, err := IncrementChainDepth(p.Message, DefaultMaxChainDepth)
	if err != nil {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeInternalError,
			Message: fmt.Sprintf("chain depth error: %v", err),
		}
	}

	// Dispatch to target agent.
	s.logger.Info("agent/invoke: dispatching",
		"source", sourceAgent,
		"target", p.TargetAgentID,
		"skill", p.Skill,
		"depth", depth+1,
	)

	task, err := s.clientPool.SendMessage(ctx, p.TargetAgentID, fwdMsg, p.Skill)
	if err != nil {
		s.logger.Error("agent/invoke: dispatch failed",
			"source", sourceAgent,
			"target", p.TargetAgentID,
			"error", err,
		)
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeInternalError,
			Message: fmt.Sprintf("invoke failed: %v", err),
		}
	}

	// Log audit event if sink is available.
	if s.auditSink != nil {
		_ = s.auditSink.LogTaskEvent(ctx, task.TaskID, "agent.invoke", map[string]string{
			"source": sourceAgent,
			"target": p.TargetAgentID,
			"skill":  p.Skill,
		})
	}

	return task, nil
}
