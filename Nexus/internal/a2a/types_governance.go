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

package a2a

// GovernanceDecision represents the outcome of a governance evaluation.
type GovernanceDecision string

const (
	GovernanceAllow    GovernanceDecision = "allow"
	GovernanceDeny     GovernanceDecision = "deny"
	GovernanceEscalate GovernanceDecision = "escalate"
)

// ValidGovernanceDecision returns true if d is a known decision value.
func ValidGovernanceDecision(d GovernanceDecision) bool {
	return d == GovernanceAllow || d == GovernanceDeny || d == GovernanceEscalate
}

// GovernanceExtension is the wire type for the
// sh.bubblefish.nexus.governance/v1 extension payload.
type GovernanceExtension struct {
	SourceAgentID        string             `json:"sourceAgentId"`
	TargetAgentID        string             `json:"targetAgentId"`
	GrantID              string             `json:"grantId,omitempty"`
	Decision             GovernanceDecision `json:"decision"`
	RequiredCapabilities []string           `json:"requiredCapabilities,omitempty"`
	Scope                string             `json:"scope,omitempty"`
	AuditID              string             `json:"auditId,omitempty"`
	ChainDepth           int                `json:"chainDepth,omitempty"`
	MaxChainDepth        int                `json:"maxChainDepth,omitempty"`
}
