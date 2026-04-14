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

import "strings"

// Reserved capability prefixes (§4.1).
const (
	CapMemoryRead  = "memory.read"
	CapMemoryWrite = "memory.write"
	CapMemoryDelete = "memory.delete"

	CapFSRead   = "fs.read"
	CapFSWrite  = "fs.write"
	CapFSDelete = "fs.delete"

	CapShellExec = "shell.exec"

	CapNetFetch = "net.fetch"
	CapNetPost  = "net.post"

	CapMessagingSendPrefix   = "messaging.send:"
	CapMessagingReadPrefix   = "messaging.read:"
	CapMessagingDeletePrefix = "messaging.delete:"

	CapMediaGeneratePrefix = "media.generate:"

	CapBrowserNavigate = "browser.navigate"

	CapAgentInvokePrefix = "agent.invoke:"

	CapSystemInfo = "system.info"
	CapSystemRun  = "system.run"

	// CapAll is the ALL capability. Covers everything.
	CapAll = "*"
)

// reservedPrefixes maps each reserved prefix to its default policy.
// Parameterized capabilities (with ":") are stored as their prefix.
var reservedPrefixes = map[string]DefaultPolicyEntry{
	CapMemoryRead:   {PolicyAutoAllow, ScopeNone},
	CapMemoryWrite:  {PolicyApproveOncePerScope, ScopeSession},
	CapMemoryDelete: {PolicyAlwaysApproveAudit, ScopeNone},

	CapFSRead:   {PolicyApproveOncePerScope, ScopePath},
	CapFSWrite:  {PolicyAlwaysApproveAudit, ScopeNone},
	CapFSDelete: {PolicyAlwaysApproveAudit, ScopeNone},

	CapShellExec: {PolicyAlwaysApproveAudit, ScopeNone},

	CapNetFetch: {PolicyApproveOncePerScope, ScopeDomain},
	CapNetPost:  {PolicyApproveOncePerScope, ScopeDomain},

	CapMessagingSendPrefix:   {PolicyApproveOncePerScope, ScopeChannel},
	CapMessagingReadPrefix:   {PolicyApproveOncePerScope, ScopeChannel},
	CapMessagingDeletePrefix: {PolicyAlwaysApproveAudit, ScopeNone},

	CapMediaGeneratePrefix: {PolicyApproveOncePerScope, ScopeKind},

	CapBrowserNavigate: {PolicyAlwaysApproveAudit, ScopeNone},

	CapAgentInvokePrefix: {PolicyApproveOncePerScope, ScopeTarget},

	CapSystemInfo: {PolicyAutoAllow, ScopeNone},
	CapSystemRun:  {PolicyAlwaysApproveAudit, ScopeNone},
}

// DefaultPolicyEntry pairs a policy with its default scope.
type DefaultPolicyEntry struct {
	Policy Policy
	Scope  Scope
}

// LookupDefaultPolicy returns the default policy for a capability string.
// For parameterized capabilities like "messaging.send:signal", it looks up
// the prefix "messaging.send:". For custom (unknown) capabilities, it returns
// AlwaysApproveAudit with no scope.
func LookupDefaultPolicy(capability string) DefaultPolicyEntry {
	// Exact match first
	if entry, ok := reservedPrefixes[capability]; ok {
		return entry
	}
	// Prefix match for parameterized capabilities
	for prefix, entry := range reservedPrefixes {
		if strings.HasSuffix(prefix, ":") && strings.HasPrefix(capability, prefix) {
			return entry
		}
	}
	// Unknown/custom capability: default to always-approve + audit
	return DefaultPolicyEntry{PolicyAlwaysApproveAudit, ScopeNone}
}

// MatchCapabilityGlob checks if a glob pattern matches a capability string.
//
// Glob rules (§4.2):
//   - "*" matches everything (ALL capability)
//   - "prefix.*" matches "prefix.X" for any X, but NOT "prefix" alone
//     and NOT "prefixfoo" (dot is required before wildcard)
//   - "prefix:" + "*" (like "messaging.send:*") matches "messaging.send:X"
//   - Exact string matches exactly
func MatchCapabilityGlob(pattern, capability string) bool {
	if pattern == CapAll {
		return true
	}
	if pattern == capability {
		return true
	}
	// "foo.*" pattern: matches "foo.bar" but not "foo" or "foobar"
	if strings.HasSuffix(pattern, ".*") {
		prefix := pattern[:len(pattern)-1] // "foo."
		return strings.HasPrefix(capability, prefix)
	}
	// "foo:*" pattern: matches "foo:bar" but not "foo" or "foobar"
	if strings.HasSuffix(pattern, ":*") {
		prefix := pattern[:len(pattern)-1] // "foo:"
		return strings.HasPrefix(capability, prefix)
	}
	return false
}

// IsReservedCapability returns true if the capability uses a known reserved prefix.
func IsReservedCapability(capability string) bool {
	if _, ok := reservedPrefixes[capability]; ok {
		return true
	}
	for prefix := range reservedPrefixes {
		if strings.HasSuffix(prefix, ":") && strings.HasPrefix(capability, prefix) {
			return true
		}
	}
	return false
}

// IsALLCapability returns true if the capability or glob is the ALL wildcard.
func IsALLCapability(capability string) bool {
	return capability == CapAll
}

// IsDestructiveCapability returns true if the capability is inherently
// destructive per the reserved prefix definitions.
func IsDestructiveCapability(capability string) bool {
	destructive := []string{
		CapMemoryDelete,
		CapFSWrite, CapFSDelete,
		CapShellExec,
		CapBrowserNavigate,
		CapSystemRun,
	}
	for _, d := range destructive {
		if capability == d {
			return true
		}
	}
	// Parameterized destructive prefixes
	if strings.HasPrefix(capability, CapMessagingDeletePrefix) {
		return true
	}
	return false
}

// ExtractParameter returns the parameter portion of a parameterized
// capability. For "messaging.send:signal", returns "signal".
// Returns "" for non-parameterized capabilities.
func ExtractParameter(capability string) string {
	if idx := strings.LastIndex(capability, ":"); idx >= 0 {
		return capability[idx+1:]
	}
	return ""
}
