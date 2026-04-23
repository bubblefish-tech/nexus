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

	"github.com/bubblefish-tech/nexus/internal/a2a/registry"
	"github.com/bubblefish-tech/nexus/internal/grants"
	"github.com/bubblefish-tech/nexus/internal/mcp"
	"github.com/bubblefish-tech/nexus/internal/orchestrate"
)

// orchestrateAdapter bridges the daemon's registry and grants stores to the
// mcp.OrchestrateProvider interface consumed by the MCP server.
type orchestrateAdapter struct {
	engine *orchestrate.Engine
}

var _ mcp.OrchestrateProvider = (*orchestrateAdapter)(nil)

func (a *orchestrateAdapter) ListAgents(ctx context.Context) ([]mcp.OrchestrateAgentDTO, error) {
	agents, err := a.engine.ListAgents(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]mcp.OrchestrateAgentDTO, len(agents))
	for i, ag := range agents {
		out[i] = mcp.OrchestrateAgentDTO{
			AgentID:        ag.AgentID,
			Name:           ag.Name,
			DisplayName:    ag.DisplayName,
			Status:         ag.Status,
			TrustTier:      ag.TrustTier,
			Orchestratable: ag.Orchestratable,
			ConnectionKind: ag.ConnectionKind,
			Capabilities:   ag.Capabilities,
		}
	}
	return out, nil
}

func (a *orchestrateAdapter) Orchestrate(ctx context.Context, callerID string, req mcp.OrchestrateRequestDTO) (mcp.OrchestrateResultDTO, error) {
	engReq := orchestrate.OrchestrationRequest{
		Task:         req.Task,
		TargetAgents: req.TargetAgents,
		TimeoutMs:    req.TimeoutMs,
		FailStrategy: req.FailStrategy,
		StoreResults: req.StoreResults,
	}
	res, err := a.engine.Orchestrate(ctx, callerID, engReq)
	if err != nil {
		return mcp.OrchestrateResultDTO{}, err
	}
	return convertOrchestrationResult(res), nil
}

func (a *orchestrateAdapter) Council(ctx context.Context, callerID string, req mcp.CouncilRequestDTO) (mcp.CouncilResultDTO, error) {
	engReq := orchestrate.CouncilRequest{
		Question:     req.Question,
		TargetAgents: req.TargetAgents,
		TimeoutMs:    req.TimeoutMs,
	}
	res, err := a.engine.Council(ctx, callerID, engReq)
	if err != nil {
		return mcp.CouncilResultDTO{}, err
	}
	return mcp.CouncilResultDTO{
		OrchestrationID: res.OrchestrationID,
		Results:         convertAgentResults(res.Results),
		Synthesis:       res.Synthesis,
	}, nil
}

func (a *orchestrateAdapter) Broadcast(ctx context.Context, callerID, signal string, agents []string) error {
	return a.engine.Broadcast(ctx, callerID, signal, agents)
}

func (a *orchestrateAdapter) Collect(ctx context.Context, callerID, orchID string) (mcp.OrchestrateResultDTO, error) {
	res, err := a.engine.Collect(ctx, callerID, orchID)
	if err != nil {
		return mcp.OrchestrateResultDTO{}, err
	}
	return convertOrchestrationResult(res), nil
}

func convertOrchestrationResult(res orchestrate.OrchestrationResult) mcp.OrchestrateResultDTO {
	return mcp.OrchestrateResultDTO{
		OrchestrationID: res.OrchestrationID,
		Results:         convertAgentResults(res.Results),
		FailStrategy:    res.FailStrategy,
		StoredMemoryIDs: res.StoredMemoryIDs,
	}
}

func convertAgentResults(results []orchestrate.AgentResult) []mcp.AgentResultDTO {
	out := make([]mcp.AgentResultDTO, len(results))
	for i, r := range results {
		scanJSON, _ := json.Marshal(r.ScanResult)
		out[i] = mcp.AgentResultDTO{
			AgentID:    r.AgentID,
			Status:     r.Status,
			Output:     r.Output,
			Error:      r.Error,
			LatencyMs:  r.LatencyMs,
			ScanResult: scanJSON,
		}
	}
	return out
}

// --- AgentLister implementation ---

// registryAgentLister wraps the A2A registry.Store to satisfy orchestrate.AgentLister.
type registryAgentLister struct {
	store *registry.Store
}

func (l *registryAgentLister) ListOrchestratableAgents(ctx context.Context) ([]orchestrate.Agent, error) {
	agents, err := l.store.List(ctx, registry.ListFilter{Status: "active"})
	if err != nil {
		return nil, err
	}
	out := make([]orchestrate.Agent, 0, len(agents))
	for _, ag := range agents {
		endpoint := ""
		if ag.TransportConfig.Kind == "http" {
			endpoint = ag.TransportConfig.URL
		}
		// Derive orchestratable from agent card capabilities: agents with streaming
		// or any registered skills are considered orchestratable.
		orchestratable := len(ag.AgentCard.Skills) > 0 || ag.AgentCard.Capabilities.Streaming

		out = append(out, orchestrate.Agent{
			AgentID:        ag.AgentID,
			Name:           ag.Name,
			DisplayName:    ag.DisplayName,
			Status:         ag.Status,
			TrustTier:      "standard",
			Orchestratable: orchestratable,
			ConnectionKind: ag.TransportConfig.Kind,
			Endpoint:       endpoint,
		})
	}
	return out, nil
}

// --- GrantChecker implementation ---

// grantStoreChecker wraps grants.Store to satisfy orchestrate.GrantChecker.
type grantStoreChecker struct {
	store *grants.Store
}

func (g *grantStoreChecker) HasGrant(ctx context.Context, agentID, capability string) (bool, error) {
	grant, err := g.store.CheckGrant(ctx, agentID, capability)
	if err != nil {
		return false, err
	}
	return grant != nil, nil
}
