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
	"database/sql"
	"encoding/json"
	"path/filepath"

	"github.com/bubblefish-tech/nexus/internal/agent"
	"github.com/bubblefish-tech/nexus/internal/config"
	"github.com/bubblefish-tech/nexus/internal/coordination"
	"github.com/bubblefish-tech/nexus/internal/credentials"
	"github.com/bubblefish-tech/nexus/internal/destination"
	"github.com/bubblefish-tech/nexus/internal/mcp"
	"github.com/bubblefish-tech/nexus/internal/policy"

	_ "modernc.org/sqlite"
)

// startAgentGateway initializes the Agent Gateway subsystems: agent registry,
// credential gateway, tool policy, quotas, coordination, and wires them into
// the MCP server and daemon.
//
// Called from Start() after the WAL and destination are open.
// Reference: AG.1–AG.8.
func (d *Daemon) startAgentGateway() {
	// ── Agent Registry (AG.1) ──────────────────────────────────────────────
	configDir, err := config.ConfigDir()
	if err != nil {
		d.logger.Warn("daemon: agent gateway: resolve config dir failed, agent registry disabled",
			"component", "daemon",
			"error", err,
		)
		return
	}

	agentDBPath := filepath.Join(configDir, "nexus.db")
	agentDB, err := sql.Open("sqlite", agentDBPath)
	if err != nil {
		d.logger.Warn("daemon: agent gateway: open agent database failed",
			"component", "daemon",
			"error", err,
		)
		return
	}
	if pragmaErr := destination.ApplySQLitePRAGMAs(agentDB); pragmaErr != nil {
		d.logger.Warn("daemon: agent gateway: PRAGMAs failed (non-fatal)",
			"component", "daemon", "error", pragmaErr)
	}
	d.agentDB = agentDB

	reg, err := agent.NewRegistry(agentDB)
	if err != nil {
		d.logger.Warn("daemon: agent gateway: init registry failed",
			"component", "daemon",
			"error", err,
		)
		return
	}
	d.agentRegistry = reg

	// ── Quota Manager (AG.6) ──────────────────────────────────────────────
	d.quotaManager = agent.NewQuotaManager(configDir, d.logger)

	// ── Tool Policy Checker (AG.4) ────────────────────────────────────────
	d.toolPolicyChecker = policy.NewToolPolicyChecker(d.logger)

	// ── Signal Queue (AG.5) ────────────────────────────────────────────────
	d.signalQueue = coordination.NewSignalQueue(1000, d.logger)

	// Register known agents in signal queue so broadcasts can reach them.
	agents, err := reg.List()
	if err == nil {
		for _, a := range agents {
			if a.Status == agent.StatusActive {
				d.signalQueue.EnsureQueue(a.ID)
			}
		}
	}

	// ── Credential Gateway (AG.3) ──────────────────────────────────────────
	cfg := d.getConfig()
	var mappings []credentials.Mapping
	if cfg.Credentials.Enabled {
		for _, m := range cfg.Credentials.Mappings {
			mappings = append(mappings, credentials.Mapping{
				SyntheticPrefix: m.SyntheticPrefix,
				Provider:        credentials.Provider(m.Provider),
				RealKeyRef:      m.RealKeyRef,
				AllowedAgents:   m.AllowedAgents,
				AllowedModels:   m.AllowedModels,
				RateLimitRPM:    m.RateLimitRPM,
			})
		}
		d.logger.Info("daemon: credential gateway loaded",
			"component", "agent_gateway",
			"mappings", len(mappings),
		)
	}
	d.credentialGateway = credentials.NewGateway(mappings, d.logger)

	// ── Wire into MCP Server (AG.4, AG.5) ──────────────────────────────────
	if d.mcpServer != nil {
		// Tool policy enforcement.
		adapter := mcp.NewToolPolicyAdapter(d.toolPolicyChecker)
		d.mcpServer.SetToolPolicyChecker(adapter)

		// Coordination provider.
		d.mcpServer.SetCoordinationProvider(d)
	}

	// ── Health Transition Callback (AG.8) ──────────────────────────────────
	d.healthTracker.SetTransitionCallback(func(agentID string, from, to agent.HealthState) {
		d.logger.Info("daemon: agent health transition",
			"component", "agent_gateway",
			"agent_id", agentID,
			"from", string(from),
			"to", string(to),
		)
		// Record as activity event.
		d.activityLog.Record(agent.ActivityEvent{
			AgentID:   agentID,
			EventType: "health_transition",
			Resource:  string(from) + " -> " + string(to),
			Result:    "ok",
		})
	})

	d.logger.Info("daemon: agent gateway started",
		"component", "daemon",
		"agents", len(agents),
	)
}

// stopAgentGateway shuts down agent gateway subsystems.
func (d *Daemon) stopAgentGateway() {
	if d.quotaManager != nil {
		d.quotaManager.Stop()
	}
	if d.agentDB != nil {
		d.agentDB.Close()
	}
}

// ── Credential proxy factories (AG.3) ──────────────────────────────────────

// credentialOpenAIProxy returns an http.Handler for POST /v1/chat/completions.
func (d *Daemon) credentialOpenAIProxy() *credentials.OpenAIProxy {
	return credentials.NewOpenAIProxy(d.credentialGateway, d.logger, func(e credentials.AuditEntry) {
		d.activityLog.Record(agent.ActivityEvent{
			AgentID:   e.AgentID,
			EventType: "credential_proxy",
			Resource:  string(e.Provider) + "/" + e.Model,
			LatencyMs: e.Latency.Milliseconds(),
			Result:    "ok",
		})
		if d.healthTracker != nil {
			d.healthTracker.Touch(e.AgentID)
		}
	})
}

// credentialAnthropicProxy returns an http.Handler for POST /v1/messages.
func (d *Daemon) credentialAnthropicProxy() *credentials.AnthropicProxy {
	return credentials.NewAnthropicProxy(d.credentialGateway, d.logger, func(e credentials.AuditEntry) {
		d.activityLog.Record(agent.ActivityEvent{
			AgentID:   e.AgentID,
			EventType: "credential_proxy",
			Resource:  string(e.Provider) + "/" + e.Model,
			LatencyMs: e.Latency.Milliseconds(),
			Result:    "ok",
		})
		if d.healthTracker != nil {
			d.healthTracker.Touch(e.AgentID)
		}
	})
}

// ── CoordinationProvider implementation (AG.5) ─────────────────────────────

// Broadcast implements mcp.CoordinationProvider.
func (d *Daemon) Broadcast(fromAgent string, signalType string, payload json.RawMessage, persistent bool, targets []string) (int64, error) {
	if d.signalQueue == nil {
		return 0, nil
	}

	seq := d.signalQueue.Broadcast(fromAgent, signalType, payload, persistent, targets)

	// Record activity.
	d.activityLog.Record(agent.ActivityEvent{
		AgentID:   fromAgent,
		EventType: "broadcast",
		Resource:  signalType,
		Result:    "ok",
	})

	return seq, nil
}

// PullSignals implements mcp.CoordinationProvider.
func (d *Daemon) PullSignals(agentID string, maxN int) []coordination.Signal {
	if d.signalQueue == nil {
		return nil
	}

	signals := d.signalQueue.Pull(agentID, maxN)

	// Record activity.
	d.activityLog.Record(agent.ActivityEvent{
		AgentID:   agentID,
		EventType: "pull_signals",
		Result:    "ok",
	})

	// Touch health tracker.
	d.healthTracker.Touch(agentID)

	return signals
}

// AgentStatus implements mcp.CoordinationProvider.
func (d *Daemon) AgentStatus(agentID string) (*mcp.AgentStatusInfo, error) {
	if d.agentRegistry == nil {
		return nil, nil
	}

	a, err := d.agentRegistry.Get(agentID)
	if err != nil {
		return nil, err
	}
	if a == nil {
		// Try by name.
		a, err = d.agentRegistry.GetByName(agentID)
		if err != nil {
			return nil, err
		}
	}
	if a == nil {
		return nil, nil
	}

	lastSeen := ""
	if !a.LastSeenAt.IsZero() {
		lastSeen = a.LastSeenAt.Format("2006-01-02T15:04:05Z")
	}

	sessionCount := 0
	if d.sessionMgr != nil {
		sessionCount = d.sessionMgr.AgentSessionCount(a.ID)
	}

	return &mcp.AgentStatusInfo{
		AgentID:      a.ID,
		Status:       string(a.Status),
		LastSeenAt:   lastSeen,
		SessionCount: sessionCount,
	}, nil
}
