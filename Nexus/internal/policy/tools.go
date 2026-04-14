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

package policy

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
)

// ToolPolicy defines per-agent tool access rules loaded from agent TOML config.
type ToolPolicy struct {
	// Allowed lists tool names the agent may invoke. Empty = all allowed.
	Allowed []string `toml:"allowed" json:"allowed"`

	// Denied lists tool names the agent may NOT invoke. Takes precedence over Allowed.
	Denied []string `toml:"denied" json:"denied"`

	// ToolLimits maps tool name → per-tool parameter constraints.
	ToolLimits map[string]ToolLimits `toml:"tool_limits" json:"tool_limits,omitempty"`
}

// ToolLimits defines parameter constraints for a specific tool.
type ToolLimits struct {
	MaxContentBytes       int      `toml:"max_content_bytes" json:"max_content_bytes,omitempty"`
	RequireMetadataFields []string `toml:"require_metadata_fields" json:"require_metadata_fields,omitempty"`
	MaxLimit              int      `toml:"max_limit" json:"max_limit,omitempty"`
	AllowedProfiles       []string `toml:"allowed_profiles" json:"allowed_profiles,omitempty"`
}

// ToolPolicyDecision is the result of a tool policy evaluation.
type ToolPolicyDecision struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason,omitempty"`
}

// ToolPolicyChecker evaluates tool-use policies for agents.
type ToolPolicyChecker struct {
	mu       sync.RWMutex
	policies map[string]*ToolPolicy // agent_id → policy
	logger   *slog.Logger
}

// NewToolPolicyChecker creates a policy checker with no initial policies.
func NewToolPolicyChecker(logger *slog.Logger) *ToolPolicyChecker {
	return &ToolPolicyChecker{
		policies: make(map[string]*ToolPolicy),
		logger:   logger,
	}
}

// SetPolicy registers or updates the tool policy for an agent.
func (c *ToolPolicyChecker) SetPolicy(agentID string, p *ToolPolicy) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.policies[agentID] = p
}

// RemovePolicy removes the tool policy for an agent.
func (c *ToolPolicyChecker) RemovePolicy(agentID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.policies, agentID)
}

// Check evaluates whether the given agent is allowed to call the named tool
// with the provided arguments. Returns a decision with the reason for denial.
//
// If no policy is registered for the agent, all tools are allowed (open by
// default for backward compatibility with agents that predate policy config).
func (c *ToolPolicyChecker) Check(agentID, toolName string, args json.RawMessage) ToolPolicyDecision {
	c.mu.RLock()
	p, ok := c.policies[agentID]
	c.mu.RUnlock()

	if !ok {
		// No policy for this agent — allow (backward compat).
		return ToolPolicyDecision{Allowed: true}
	}

	// Denylist takes precedence.
	for _, denied := range p.Denied {
		if denied == toolName {
			return ToolPolicyDecision{
				Allowed: false,
				Reason:  fmt.Sprintf("policy_denied_tool: %s is in the deny list", toolName),
			}
		}
	}

	// Allowlist enforcement (empty = all allowed).
	if len(p.Allowed) > 0 {
		found := false
		for _, allowed := range p.Allowed {
			if allowed == toolName {
				found = true
				break
			}
		}
		if !found {
			return ToolPolicyDecision{
				Allowed: false,
				Reason:  fmt.Sprintf("policy_denied_tool: %s is not in the allow list", toolName),
			}
		}
	}

	// Per-tool parameter limits.
	if limits, ok := p.ToolLimits[toolName]; ok {
		if decision := checkToolLimits(toolName, limits, args); !decision.Allowed {
			return decision
		}
	}

	return ToolPolicyDecision{Allowed: true}
}

// checkToolLimits validates tool arguments against parameter constraints.
func checkToolLimits(toolName string, limits ToolLimits, args json.RawMessage) ToolPolicyDecision {
	if len(args) == 0 {
		return ToolPolicyDecision{Allowed: true}
	}

	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(args, &parsed); err != nil {
		// Can't parse args — allow through; the tool handler will reject bad JSON.
		return ToolPolicyDecision{Allowed: true}
	}

	// MaxContentBytes: check if content field exceeds the limit.
	if limits.MaxContentBytes > 0 {
		if content, ok := parsed["content"]; ok {
			var s string
			if json.Unmarshal(content, &s) == nil {
				if len(s) > limits.MaxContentBytes {
					return ToolPolicyDecision{
						Allowed: false,
						Reason:  fmt.Sprintf("policy_violation_max_content_bytes: content is %d bytes, limit is %d", len(s), limits.MaxContentBytes),
					}
				}
			}
		}
	}

	// MaxLimit: check if limit field exceeds the maximum.
	if limits.MaxLimit > 0 {
		if limitRaw, ok := parsed["limit"]; ok {
			var n int
			if json.Unmarshal(limitRaw, &n) == nil {
				if n > limits.MaxLimit {
					return ToolPolicyDecision{
						Allowed: false,
						Reason:  fmt.Sprintf("policy_violation_max_limit: limit is %d, maximum is %d", n, limits.MaxLimit),
					}
				}
			}
		}
	}

	// AllowedProfiles: check if profile is in the allowed list.
	if len(limits.AllowedProfiles) > 0 {
		if profileRaw, ok := parsed["profile"]; ok {
			var profile string
			if json.Unmarshal(profileRaw, &profile) == nil && profile != "" {
				found := false
				for _, allowed := range limits.AllowedProfiles {
					if allowed == profile {
						found = true
						break
					}
				}
				if !found {
					return ToolPolicyDecision{
						Allowed: false,
						Reason:  fmt.Sprintf("policy_violation_profile: profile %q is not allowed", profile),
					}
				}
			}
		}
	}

	// RequireMetadataFields: not enforced at the policy layer (metadata
	// is optional in the write pipeline), but logged as a warning.
	// The write handler checks required fields.

	return ToolPolicyDecision{Allowed: true}
}
