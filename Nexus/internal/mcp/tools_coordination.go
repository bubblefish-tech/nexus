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
	"encoding/json"
	"net/http"

	"github.com/bubblefish-tech/nexus/internal/coordination"
)

// CoordinationProvider is the interface the MCP server uses to dispatch
// agent coordination tool calls. Implemented by the daemon.
type CoordinationProvider interface {
	// Broadcast sends a signal to target agents (or all if targets is empty).
	Broadcast(fromAgent string, signalType string, payload json.RawMessage, persistent bool, targets []string) (int64, error)

	// PullSignals retrieves pending signals for an agent.
	PullSignals(agentID string, maxN int) []coordination.Signal

	// AgentStatus returns status info for a named agent.
	AgentStatus(agentID string) (*AgentStatusInfo, error)
}

// AgentStatusInfo is returned by agent_status_query.
type AgentStatusInfo struct {
	AgentID      string `json:"agent_id"`
	Status       string `json:"status"`
	LastSeenAt   string `json:"last_seen_at"`
	SessionCount int    `json:"session_count"`
}

// SetCoordinationProvider configures the agent coordination MCP tools.
// Must be called before Start(). Reference: AG.5.
func (s *Server) SetCoordinationProvider(p CoordinationProvider) {
	s.coordinationProvider = p
}

// callAgentBroadcast handles the agent_broadcast MCP tool.
func (s *Server) callAgentBroadcast(w http.ResponseWriter, r *http.Request, req rpcRequest, args json.RawMessage) {
	if s.coordinationProvider == nil {
		s.writeToolError(w, r, req.ID, "coordination not enabled")
		return
	}

	var a struct {
		Type       string          `json:"type"`
		Payload    json.RawMessage `json:"payload"`
		Persistent bool            `json:"persistent"`
		Targets    []string        `json:"targets"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &a); err != nil {
			s.writeRPCError(w, r, req.ID, rpcInvalidParams, "invalid agent_broadcast arguments: "+err.Error())
			return
		}
	}

	if a.Type == "" {
		s.writeToolError(w, r, req.ID, "agent_broadcast requires 'type' argument")
		return
	}

	fromAgent := r.Header.Get("X-Agent-ID")

	seq, err := s.coordinationProvider.Broadcast(fromAgent, a.Type, a.Payload, a.Persistent, a.Targets)
	if err != nil {
		s.writeToolError(w, r, req.ID, "broadcast failed: "+err.Error())
		return
	}

	out, _ := json.Marshal(map[string]interface{}{
		"status":   "sent",
		"sequence": seq,
	})
	s.writeRPCResult(w, r, req.ID, toolCallResult{
		Content: []contentBlock{{Type: "text", Text: string(out)}},
	})
}

// callAgentPullSignals handles the agent_pull_signals MCP tool.
func (s *Server) callAgentPullSignals(w http.ResponseWriter, r *http.Request, req rpcRequest, args json.RawMessage) {
	if s.coordinationProvider == nil {
		s.writeToolError(w, r, req.ID, "coordination not enabled")
		return
	}

	var a struct {
		MaxN int `json:"max_n"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &a); err != nil {
			s.writeRPCError(w, r, req.ID, rpcInvalidParams, "invalid agent_pull_signals arguments: "+err.Error())
			return
		}
	}
	if a.MaxN <= 0 {
		a.MaxN = 100
	}

	agentID := r.Header.Get("X-Agent-ID")
	signals := s.coordinationProvider.PullSignals(agentID, a.MaxN)

	out, _ := json.Marshal(map[string]interface{}{
		"signals": signals,
		"count":   len(signals),
	})
	s.writeRPCResult(w, r, req.ID, toolCallResult{
		Content: []contentBlock{{Type: "text", Text: string(out)}},
	})
}

// callAgentStatusQuery handles the agent_status_query MCP tool.
func (s *Server) callAgentStatusQuery(w http.ResponseWriter, r *http.Request, req rpcRequest, args json.RawMessage) {
	if s.coordinationProvider == nil {
		s.writeToolError(w, r, req.ID, "coordination not enabled")
		return
	}

	var a struct {
		AgentID string `json:"agent_id"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &a); err != nil {
			s.writeRPCError(w, r, req.ID, rpcInvalidParams, "invalid agent_status_query arguments: "+err.Error())
			return
		}
	}

	if a.AgentID == "" {
		s.writeToolError(w, r, req.ID, "agent_status_query requires 'agent_id' argument")
		return
	}

	info, err := s.coordinationProvider.AgentStatus(a.AgentID)
	if err != nil {
		s.writeToolError(w, r, req.ID, "status query failed: "+err.Error())
		return
	}

	out, _ := json.Marshal(info)
	s.writeRPCResult(w, r, req.ID, toolCallResult{
		Content: []contentBlock{{Type: "text", Text: string(out)}},
	})
}
