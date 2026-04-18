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

// Policy determines how the governance engine handles a task dispatch
// when no explicit grant covers the requested capability.
type Policy int

const (
	// PolicyAutoAllow dispatches immediately; audit entry written.
	PolicyAutoAllow Policy = iota

	// PolicyApproveOncePerScope raises an approval prompt the first time;
	// once approved, a cached grant is created with the associated Scope.
	PolicyApproveOncePerScope

	// PolicyAlwaysApprove raises an approval prompt every invocation
	// regardless of prior grants.
	PolicyAlwaysApprove

	// PolicyAlwaysApproveAudit is AlwaysApprove plus the audit entry is
	// flagged for compliance review.
	PolicyAlwaysApproveAudit

	// PolicyDeny blocks the dispatch unconditionally.
	PolicyDeny
)

var policyStrings = [...]string{
	"auto-allow",
	"approve-once-per-scope",
	"always-approve",
	"always-approve-audit",
	"deny",
}

func (p Policy) String() string {
	if p >= 0 && int(p) < len(policyStrings) {
		return policyStrings[p]
	}
	return "unknown"
}

// ParsePolicy converts a string to a Policy. Returns PolicyDeny and false
// for unknown strings.
func ParsePolicy(s string) (Policy, bool) {
	for i, ps := range policyStrings {
		if ps == s {
			return Policy(i), true
		}
	}
	return PolicyDeny, false
}

// Scope determines the scope of a cached grant for approve-once policies.
type Scope int

const (
	ScopeNone    Scope = iota // No scope (used for non-approve-once policies)
	ScopeSession              // per-session
	ScopePath                 // per-path (fs operations)
	ScopeDomain               // per-domain (net operations)
	ScopeChannel              // per-channel (messaging)
	ScopeKind                 // per-kind (media generation)
	ScopeTarget               // per-target (agent invocation)
)

var scopeStrings = [...]string{
	"none",
	"per-session",
	"per-path",
	"per-domain",
	"per-channel",
	"per-kind",
	"per-target",
}

func (s Scope) String() string {
	if s >= 0 && int(s) < len(scopeStrings) {
		return scopeStrings[s]
	}
	return "unknown"
}

// ParseScope converts a string to a Scope. Returns ScopeNone and false
// for unknown strings.
func ParseScope(s string) (Scope, bool) {
	for i, ss := range scopeStrings {
		if ss == s {
			return Scope(i), true
		}
	}
	return ScopeNone, false
}

// ResolvePolicy determines the default policy and scope for a capability.
// This is a convenience wrapper around LookupDefaultPolicy.
func ResolvePolicy(capability string) (Policy, Scope) {
	entry := LookupDefaultPolicy(capability)
	return entry.Policy, entry.Scope
}
