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

import "testing"

func TestMatchCapabilityGlob(t *testing.T) {
	tests := []struct {
		pattern    string
		capability string
		want       bool
	}{
		// ALL capability
		{"*", "memory.read", true},
		{"*", "messaging.send:signal", true},
		{"*", "anything.at.all", true},
		{"*", "*", true},

		// Exact match
		{"memory.read", "memory.read", true},
		{"memory.read", "memory.write", false},
		{"memory.read", "memory.reading", false},

		// Dot-star glob
		{"memory.*", "memory.read", true},
		{"memory.*", "memory.write", true},
		{"memory.*", "memory.delete", true},
		{"memory.*", "memory", false},
		{"memory.*", "memoryfoo", false},
		{"memory.*", "memory.", true},
		{"fs.*", "fs.read", true},
		{"fs.*", "fs.write", true},
		{"fs.*", "fs.delete", true},
		{"fs.*", "fsread", false},

		// Colon-star glob
		{"messaging.send:*", "messaging.send:signal", true},
		{"messaging.send:*", "messaging.send:slack", true},
		{"messaging.send:*", "messaging.send:telegram", true},
		{"messaging.send:*", "messaging.send:", true},
		{"messaging.send:*", "messaging.send", false},
		{"messaging.send:*", "messaging.read:signal", false},
		{"messaging.read:*", "messaging.read:slack", true},
		{"messaging.delete:*", "messaging.delete:whatsapp", true},
		{"media.generate:*", "media.generate:image", true},
		{"media.generate:*", "media.generate:audio", true},
		{"agent.invoke:*", "agent.invoke:openclaw", true},
		{"agent.invoke:*", "agent.invoke:", true},

		// Non-matching patterns
		{"memory.read", "fs.read", false},
		{"shell.*", "memory.read", false},
		{"messaging.send:signal", "messaging.send:slack", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_vs_"+tt.capability, func(t *testing.T) {
			got := MatchCapabilityGlob(tt.pattern, tt.capability)
			if got != tt.want {
				t.Errorf("MatchCapabilityGlob(%q, %q) = %v, want %v",
					tt.pattern, tt.capability, got, tt.want)
			}
		})
	}
}

func TestLookupDefaultPolicy(t *testing.T) {
	tests := []struct {
		capability string
		wantPolicy Policy
		wantScope  Scope
	}{
		// Auto-allow
		{"memory.read", PolicyAutoAllow, ScopeNone},
		{"system.info", PolicyAutoAllow, ScopeNone},

		// Approve-once-per-scope
		{"memory.write", PolicyApproveOncePerScope, ScopeSession},
		{"fs.read", PolicyApproveOncePerScope, ScopePath},
		{"net.fetch", PolicyApproveOncePerScope, ScopeDomain},
		{"net.post", PolicyApproveOncePerScope, ScopeDomain},

		// Parameterized approve-once
		{"messaging.send:signal", PolicyApproveOncePerScope, ScopeChannel},
		{"messaging.send:slack", PolicyApproveOncePerScope, ScopeChannel},
		{"messaging.read:telegram", PolicyApproveOncePerScope, ScopeChannel},
		{"media.generate:image", PolicyApproveOncePerScope, ScopeKind},
		{"media.generate:audio", PolicyApproveOncePerScope, ScopeKind},
		{"agent.invoke:openclaw", PolicyApproveOncePerScope, ScopeTarget},

		// Always-approve + audit
		{"memory.delete", PolicyAlwaysApproveAudit, ScopeNone},
		{"fs.write", PolicyAlwaysApproveAudit, ScopeNone},
		{"fs.delete", PolicyAlwaysApproveAudit, ScopeNone},
		{"shell.exec", PolicyAlwaysApproveAudit, ScopeNone},
		{"messaging.delete:signal", PolicyAlwaysApproveAudit, ScopeNone},
		{"browser.navigate", PolicyAlwaysApproveAudit, ScopeNone},
		{"system.run", PolicyAlwaysApproveAudit, ScopeNone},

		// Custom/unknown capabilities default to always-approve-audit
		{"custom.something", PolicyAlwaysApproveAudit, ScopeNone},
		{"vendor.ai.tool", PolicyAlwaysApproveAudit, ScopeNone},
	}

	for _, tt := range tests {
		t.Run(tt.capability, func(t *testing.T) {
			entry := LookupDefaultPolicy(tt.capability)
			if entry.Policy != tt.wantPolicy {
				t.Errorf("policy for %q: got %v, want %v",
					tt.capability, entry.Policy, tt.wantPolicy)
			}
			if entry.Scope != tt.wantScope {
				t.Errorf("scope for %q: got %v, want %v",
					tt.capability, entry.Scope, tt.wantScope)
			}
		})
	}
}

func TestIsReservedCapability(t *testing.T) {
	tests := []struct {
		cap  string
		want bool
	}{
		{"memory.read", true},
		{"memory.write", true},
		{"memory.delete", true},
		{"fs.read", true},
		{"shell.exec", true},
		{"messaging.send:signal", true},
		{"messaging.read:slack", true},
		{"agent.invoke:openclaw", true},
		{"media.generate:image", true},
		{"system.info", true},
		{"custom.something", false},
		{"vendor.tool", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := IsReservedCapability(tt.cap); got != tt.want {
			t.Errorf("IsReservedCapability(%q) = %v, want %v", tt.cap, got, tt.want)
		}
	}
}

func TestIsALLCapability(t *testing.T) {
	if !IsALLCapability("*") {
		t.Error("* should be ALL")
	}
	if IsALLCapability("memory.*") {
		t.Error("memory.* is not ALL")
	}
	if IsALLCapability("") {
		t.Error("empty is not ALL")
	}
}

func TestIsDestructiveCapability(t *testing.T) {
	tests := []struct {
		cap  string
		want bool
	}{
		{"memory.delete", true},
		{"fs.write", true},
		{"fs.delete", true},
		{"shell.exec", true},
		{"browser.navigate", true},
		{"system.run", true},
		{"messaging.delete:signal", true},
		{"messaging.delete:slack", true},

		{"memory.read", false},
		{"memory.write", false},
		{"fs.read", false},
		{"net.fetch", false},
		{"messaging.send:signal", false},
		{"messaging.read:slack", false},
		{"system.info", false},
	}
	for _, tt := range tests {
		if got := IsDestructiveCapability(tt.cap); got != tt.want {
			t.Errorf("IsDestructiveCapability(%q) = %v, want %v", tt.cap, got, tt.want)
		}
	}
}

func TestExtractParameter(t *testing.T) {
	tests := []struct {
		cap  string
		want string
	}{
		{"messaging.send:signal", "signal"},
		{"messaging.send:slack", "slack"},
		{"messaging.read:telegram", "telegram"},
		{"agent.invoke:openclaw", "openclaw"},
		{"media.generate:image", "image"},
		{"memory.read", ""},
		{"fs.write", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := ExtractParameter(tt.cap); got != tt.want {
			t.Errorf("ExtractParameter(%q) = %q, want %q", tt.cap, got, tt.want)
		}
	}
}

func TestResolvePolicy(t *testing.T) {
	// ResolvePolicy is a convenience wrapper; spot-check a few
	policy, scope := ResolvePolicy("memory.read")
	if policy != PolicyAutoAllow || scope != ScopeNone {
		t.Errorf("memory.read: got (%v, %v), want (AutoAllow, None)", policy, scope)
	}

	policy, scope = ResolvePolicy("messaging.send:signal")
	if policy != PolicyApproveOncePerScope || scope != ScopeChannel {
		t.Errorf("messaging.send:signal: got (%v, %v)", policy, scope)
	}

	policy, scope = ResolvePolicy("shell.exec")
	if policy != PolicyAlwaysApproveAudit || scope != ScopeNone {
		t.Errorf("shell.exec: got (%v, %v)", policy, scope)
	}
}

func TestPolicyString(t *testing.T) {
	tests := []struct {
		p    Policy
		want string
	}{
		{PolicyAutoAllow, "auto-allow"},
		{PolicyApproveOncePerScope, "approve-once-per-scope"},
		{PolicyAlwaysApprove, "always-approve"},
		{PolicyAlwaysApproveAudit, "always-approve-audit"},
		{PolicyDeny, "deny"},
		{Policy(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.p.String(); got != tt.want {
			t.Errorf("Policy(%d).String() = %q, want %q", tt.p, got, tt.want)
		}
	}
}

func TestParsePolicy(t *testing.T) {
	tests := []struct {
		s    string
		want Policy
		ok   bool
	}{
		{"auto-allow", PolicyAutoAllow, true},
		{"approve-once-per-scope", PolicyApproveOncePerScope, true},
		{"always-approve", PolicyAlwaysApprove, true},
		{"always-approve-audit", PolicyAlwaysApproveAudit, true},
		{"deny", PolicyDeny, true},
		{"unknown", PolicyDeny, false},
		{"", PolicyDeny, false},
	}
	for _, tt := range tests {
		got, ok := ParsePolicy(tt.s)
		if got != tt.want || ok != tt.ok {
			t.Errorf("ParsePolicy(%q) = (%v, %v), want (%v, %v)",
				tt.s, got, ok, tt.want, tt.ok)
		}
	}
}

func TestScopeString(t *testing.T) {
	tests := []struct {
		s    Scope
		want string
	}{
		{ScopeNone, "none"},
		{ScopeSession, "per-session"},
		{ScopePath, "per-path"},
		{ScopeDomain, "per-domain"},
		{ScopeChannel, "per-channel"},
		{ScopeKind, "per-kind"},
		{ScopeTarget, "per-target"},
		{Scope(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.s.String(); got != tt.want {
			t.Errorf("Scope(%d).String() = %q, want %q", tt.s, got, tt.want)
		}
	}
}

func TestParseScope(t *testing.T) {
	tests := []struct {
		s    string
		want Scope
		ok   bool
	}{
		{"none", ScopeNone, true},
		{"per-session", ScopeSession, true},
		{"per-path", ScopePath, true},
		{"per-domain", ScopeDomain, true},
		{"per-channel", ScopeChannel, true},
		{"per-kind", ScopeKind, true},
		{"per-target", ScopeTarget, true},
		{"invalid", ScopeNone, false},
		{"", ScopeNone, false},
	}
	for _, tt := range tests {
		got, ok := ParseScope(tt.s)
		if got != tt.want || ok != tt.ok {
			t.Errorf("ParseScope(%q) = (%v, %v), want (%v, %v)",
				tt.s, got, ok, tt.want, tt.ok)
		}
	}
}
