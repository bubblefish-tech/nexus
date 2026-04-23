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
)


// OrchestrateProvider is the interface the MCP server uses to execute
// multi-agent orchestration tool calls. Implemented by the daemon adapter.
// Nil means orchestration is not enabled; all orchestration tools return an error.
type OrchestrateProvider interface {
	ListAgents(ctx context.Context) ([]OrchestrateAgentDTO, error)
	Orchestrate(ctx context.Context, callerID string, req OrchestrateRequestDTO) (OrchestrateResultDTO, error)
	Council(ctx context.Context, callerID string, req CouncilRequestDTO) (CouncilResultDTO, error)
	Broadcast(ctx context.Context, callerID, signal string, agents []string) error
	Collect(ctx context.Context, callerID, orchID string) (OrchestrateResultDTO, error)
}

// OrchestrateAgentDTO is the MCP-layer view of an orchestratable agent.
type OrchestrateAgentDTO struct {
	AgentID        string   `json:"agent_id"`
	Name           string   `json:"name"`
	DisplayName    string   `json:"display_name,omitempty"`
	Status         string   `json:"status"`
	TrustTier      string   `json:"trust_tier"`
	Orchestratable bool     `json:"orchestratable"`
	ConnectionKind string   `json:"connection_kind"`
	Capabilities   []string `json:"capabilities,omitempty"`
}

// OrchestrateRequestDTO is the MCP-layer input for nexus_orchestrate.
type OrchestrateRequestDTO struct {
	Task         string   `json:"task"`
	TargetAgents []string `json:"target_agents,omitempty"`
	TimeoutMs    int64    `json:"timeout_ms,omitempty"`
	FailStrategy string   `json:"fail_strategy,omitempty"`
	StoreResults bool     `json:"store_results,omitempty"`
}

// AgentResultDTO is the per-agent result in an orchestration.
type AgentResultDTO struct {
	AgentID    string          `json:"agent_id"`
	Status     string          `json:"status"`
	Output     string          `json:"output,omitempty"`
	Error      string          `json:"error,omitempty"`
	LatencyMs  int64           `json:"latency_ms"`
	ScanResult json.RawMessage `json:"scan_result,omitempty"`
}

// OrchestrateResultDTO is the MCP-layer output for nexus_orchestrate / nexus_collect.
type OrchestrateResultDTO struct {
	OrchestrationID string           `json:"orchestration_id"`
	Results         []AgentResultDTO `json:"results"`
	FailStrategy    string           `json:"fail_strategy"`
	StoredMemoryIDs []string         `json:"stored_memory_ids,omitempty"`
}

// CouncilRequestDTO is the MCP-layer input for nexus_council.
type CouncilRequestDTO struct {
	Question     string   `json:"question"`
	TargetAgents []string `json:"target_agents,omitempty"`
	TimeoutMs    int64    `json:"timeout_ms,omitempty"`
}

// CouncilResultDTO is the MCP-layer output for nexus_council.
type CouncilResultDTO struct {
	OrchestrationID string           `json:"orchestration_id"`
	Results         []AgentResultDTO `json:"results"`
	Synthesis       string           `json:"synthesis,omitempty"`
}

// SetOrchestrateProvider configures the orchestration engine adapter.
// Must be called before Start(). Pass nil to disable orchestration tools.
func (s *Server) SetOrchestrateProvider(p OrchestrateProvider) {
	s.orchestrateProvider = p
}

// orchestrateToolDefs returns the MCP tool definitions for the 5 orchestration tools.
func orchestrateToolDefs() []toolDef {
	return []toolDef{
		{
			Name:        "nexus_list_agents",
			Description: "List all connected agents with their status, capabilities, orchestratable flag, and trust tier.",
			InputSchema: inputSchema{Type: "object", Properties: map[string]propDef{}},
		},
		{
			Name:        "nexus_orchestrate",
			Description: "Dispatch a task to multiple agents simultaneously. Requires 'orchestrate' and per-target 'dispatch:<agent_id>' grants.",
			InputSchema: inputSchema{
				Type:     "object",
				Required: []string{"task"},
				Properties: map[string]propDef{
					"task":          {Type: "string", Description: "Task or instruction to send to each target agent."},
					"target_agents": {Type: "array", Description: "Agent IDs to dispatch to. Empty = all orchestratable agents."},
					"timeout_ms":    {Type: "integer", Description: "Per-agent timeout in milliseconds. Default 30000."},
					"fail_strategy": {Type: "string", Description: "Failure mode: wait_all (default), return_partial, or fail_fast."},
					"store_results": {Type: "boolean", Description: "If true, store each successful result as a memory with derived_from lineage."},
				},
			},
		},
		{
			Name:        "nexus_council",
			Description: "Submit a question for multi-agent deliberation. Each agent reasons step-by-step. Returns individual responses and a synthesis.",
			InputSchema: inputSchema{
				Type:     "object",
				Required: []string{"question"},
				Properties: map[string]propDef{
					"question":      {Type: "string", Description: "Question for the council to deliberate on."},
					"target_agents": {Type: "array", Description: "Agent IDs to include. Empty = all orchestratable agents."},
					"timeout_ms":    {Type: "integer", Description: "Per-agent timeout in milliseconds. Default 30000."},
				},
			},
		},
		{
			Name:        "nexus_broadcast",
			Description: "Publish a signal to all (or specified) connected agents as a fire-and-forget notification.",
			InputSchema: inputSchema{
				Type:     "object",
				Required: []string{"signal"},
				Properties: map[string]propDef{
					"signal": {Type: "string", Description: "Signal content to broadcast."},
					"agents": {Type: "array", Description: "Agent IDs to broadcast to. Empty = all orchestratable agents."},
				},
			},
		},
		{
			Name:        "nexus_collect",
			Description: "Gather results from a prior nexus_orchestrate or nexus_council call by orchestration ID.",
			InputSchema: inputSchema{
				Type:     "object",
				Required: []string{"orchestration_id"},
				Properties: map[string]propDef{
					"orchestration_id": {Type: "string", Description: "ID returned by a prior nexus_orchestrate or nexus_council call."},
				},
			},
		},
	}
}

func (s *Server) callNexusListAgents(w http.ResponseWriter, r *http.Request, req rpcRequest) {
	if s.orchestrateProvider == nil {
		s.writeToolError(w, r, req.ID, "orchestration is not enabled")
		return
	}
	agents, err := s.orchestrateProvider.ListAgents(r.Context())
	if err != nil {
		s.writeToolError(w, r, req.ID, "list agents failed: "+err.Error())
		return
	}
	if agents == nil {
		agents = []OrchestrateAgentDTO{}
	}
	out, _ := json.Marshal(map[string]interface{}{
		"agents": agents,
		"count":  len(agents),
	})
	s.writeRPCResult(w, r, req.ID, toolCallResult{
		Content: []contentBlock{{Type: "text", Text: string(out)}},
	})
}

func (s *Server) callNexusOrchestrate(w http.ResponseWriter, r *http.Request, req rpcRequest, args json.RawMessage) {
	if s.orchestrateProvider == nil {
		s.writeToolError(w, r, req.ID, "orchestration is not enabled")
		return
	}
	agentID := r.Header.Get("X-Agent-ID")
	if agentID == "" {
		s.writeToolError(w, r, req.ID, "X-Agent-ID header is required for nexus_orchestrate")
		return
	}

	var a OrchestrateRequestDTO
	if len(args) > 0 {
		if err := json.Unmarshal(args, &a); err != nil {
			s.writeRPCError(w, r, req.ID, rpcInvalidParams, "invalid nexus_orchestrate arguments: "+err.Error())
			return
		}
	}

	result, err := s.orchestrateProvider.Orchestrate(r.Context(), agentID, a)
	if err != nil {
		s.writeToolError(w, r, req.ID, err.Error())
		return
	}
	out, _ := json.Marshal(result)
	s.writeRPCResult(w, r, req.ID, toolCallResult{
		Content: []contentBlock{{Type: "text", Text: string(out)}},
	})
}

func (s *Server) callNexusCouncil(w http.ResponseWriter, r *http.Request, req rpcRequest, args json.RawMessage) {
	if s.orchestrateProvider == nil {
		s.writeToolError(w, r, req.ID, "orchestration is not enabled")
		return
	}
	agentID := r.Header.Get("X-Agent-ID")
	if agentID == "" {
		s.writeToolError(w, r, req.ID, "X-Agent-ID header is required for nexus_council")
		return
	}

	var a CouncilRequestDTO
	if len(args) > 0 {
		if err := json.Unmarshal(args, &a); err != nil {
			s.writeRPCError(w, r, req.ID, rpcInvalidParams, "invalid nexus_council arguments: "+err.Error())
			return
		}
	}

	result, err := s.orchestrateProvider.Council(r.Context(), agentID, a)
	if err != nil {
		s.writeToolError(w, r, req.ID, err.Error())
		return
	}
	out, _ := json.Marshal(result)
	s.writeRPCResult(w, r, req.ID, toolCallResult{
		Content: []contentBlock{{Type: "text", Text: string(out)}},
	})
}

func (s *Server) callNexusBroadcast(w http.ResponseWriter, r *http.Request, req rpcRequest, args json.RawMessage) {
	if s.orchestrateProvider == nil {
		s.writeToolError(w, r, req.ID, "orchestration is not enabled")
		return
	}
	agentID := r.Header.Get("X-Agent-ID")
	if agentID == "" {
		s.writeToolError(w, r, req.ID, "X-Agent-ID header is required for nexus_broadcast")
		return
	}

	var a struct {
		Signal string   `json:"signal"`
		Agents []string `json:"agents"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &a); err != nil {
			s.writeRPCError(w, r, req.ID, rpcInvalidParams, "invalid nexus_broadcast arguments: "+err.Error())
			return
		}
	}

	if err := s.orchestrateProvider.Broadcast(r.Context(), agentID, a.Signal, a.Agents); err != nil {
		s.writeToolError(w, r, req.ID, err.Error())
		return
	}
	out, _ := json.Marshal(map[string]string{"status": "dispatched"})
	s.writeRPCResult(w, r, req.ID, toolCallResult{
		Content: []contentBlock{{Type: "text", Text: string(out)}},
	})
}

func (s *Server) callNexusCollect(w http.ResponseWriter, r *http.Request, req rpcRequest, args json.RawMessage) {
	if s.orchestrateProvider == nil {
		s.writeToolError(w, r, req.ID, "orchestration is not enabled")
		return
	}
	agentID := r.Header.Get("X-Agent-ID")
	if agentID == "" {
		s.writeToolError(w, r, req.ID, "X-Agent-ID header is required for nexus_collect")
		return
	}

	var a struct {
		OrchestrationID string `json:"orchestration_id"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &a); err != nil {
			s.writeRPCError(w, r, req.ID, rpcInvalidParams, "invalid nexus_collect arguments: "+err.Error())
			return
		}
	}

	result, err := s.orchestrateProvider.Collect(r.Context(), agentID, a.OrchestrationID)
	if err != nil {
		s.writeToolError(w, r, req.ID, err.Error())
		return
	}
	out, _ := json.Marshal(result)
	s.writeRPCResult(w, r, req.ID, toolCallResult{
		Content: []contentBlock{{Type: "text", Text: string(out)}},
	})
}
