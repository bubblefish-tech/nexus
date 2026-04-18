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

// Package governance implements the A2A governance engine which decides
// whether inter-agent task dispatches are allowed based on grants and policies.
package governance

import "time"

// Grant is a persistent record authorizing (or denying) a source agent to
// invoke capabilities on a target agent.
type Grant struct {
	GrantID        string
	SourceAgentID  string
	TargetAgentID  string
	CapabilityGlob string
	Scope          string // "SCOPED" or "ALL"
	Decision       string // "allow", "deny", "approve-once"
	ExpiresAt      *time.Time
	IssuedBy       string
	IssuedAt       time.Time
	RevokedAt      *time.Time
	Notes          string
}

// IsExpired returns true if the grant has a non-nil expiry that is before now.
func (g *Grant) IsExpired(now time.Time) bool {
	return g.ExpiresAt != nil && g.ExpiresAt.Before(now)
}

// IsRevoked returns true if the grant has been revoked.
func (g *Grant) IsRevoked() bool {
	return g.RevokedAt != nil
}

// IsActive returns true if the grant is neither expired nor revoked.
func (g *Grant) IsActive(now time.Time) bool {
	return !g.IsExpired(now) && !g.IsRevoked()
}

// PendingApproval represents an approval request that has been escalated
// and is awaiting an administrative decision.
type PendingApproval struct {
	ApprovalID           string
	TaskID               string
	SourceAgentID        string
	TargetAgentID        string
	Skill                string
	RequiredCapabilities []string
	InputPreview         string
	CreatedAt            time.Time
	ResolvedAt           *time.Time
	ResolvedBy           string
	Resolution           string // "approved", "denied", "timeout"
}
