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

	"github.com/bubblefish-tech/nexus/internal/policy"
)

// ToolPolicyAdapter adapts policy.ToolPolicyChecker to the
// ToolPolicyCheckerIface used by the MCP server.
type ToolPolicyAdapter struct {
	checker *policy.ToolPolicyChecker
}

// NewToolPolicyAdapter wraps a policy.ToolPolicyChecker for use with the MCP server.
func NewToolPolicyAdapter(checker *policy.ToolPolicyChecker) *ToolPolicyAdapter {
	return &ToolPolicyAdapter{checker: checker}
}

// Check delegates to the wrapped policy checker and converts the result type.
func (a *ToolPolicyAdapter) Check(agentID, toolName string, args json.RawMessage) ToolPolicyDecision {
	d := a.checker.Check(agentID, toolName, args)
	return ToolPolicyDecision{
		Allowed: d.Allowed,
		Reason:  d.Reason,
	}
}
