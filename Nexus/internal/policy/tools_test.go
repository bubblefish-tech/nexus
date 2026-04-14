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
	"log/slog"
	"os"
	"strings"
	"testing"
)

func testPolicyChecker(t *testing.T) *ToolPolicyChecker {
	t.Helper()
	return NewToolPolicyChecker(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
}

func TestToolPolicy_NoPolicy(t *testing.T) {
	c := testPolicyChecker(t)
	d := c.Check("unknown-agent", "nexus_write", nil)
	if !d.Allowed {
		t.Fatal("no policy should allow all tools")
	}
}

func TestToolPolicy_AllowlistEnforced(t *testing.T) {
	c := testPolicyChecker(t)
	c.SetPolicy("agent-1", &ToolPolicy{
		Allowed: []string{"nexus_write", "nexus_search"},
	})

	d := c.Check("agent-1", "nexus_write", nil)
	if !d.Allowed {
		t.Fatal("nexus_write should be allowed")
	}

	d = c.Check("agent-1", "nexus_status", nil)
	if d.Allowed {
		t.Fatal("nexus_status should be denied")
	}
	if !strings.Contains(d.Reason, "not in the allow list") {
		t.Fatalf("wrong reason: %s", d.Reason)
	}
}

func TestToolPolicy_DenylistTakesPrecedence(t *testing.T) {
	c := testPolicyChecker(t)
	c.SetPolicy("agent-1", &ToolPolicy{
		Allowed: []string{"nexus_write", "nexus_search", "nexus_status"},
		Denied:  []string{"nexus_write"},
	})

	d := c.Check("agent-1", "nexus_write", nil)
	if d.Allowed {
		t.Fatal("nexus_write should be denied (in deny list)")
	}
	if !strings.Contains(d.Reason, "deny list") {
		t.Fatalf("wrong reason: %s", d.Reason)
	}

	d = c.Check("agent-1", "nexus_search", nil)
	if !d.Allowed {
		t.Fatal("nexus_search should be allowed")
	}
}

func TestToolPolicy_EmptyAllowlist(t *testing.T) {
	c := testPolicyChecker(t)
	c.SetPolicy("agent-1", &ToolPolicy{
		Allowed: []string{}, // empty = all allowed
	})

	d := c.Check("agent-1", "any_tool", nil)
	if !d.Allowed {
		t.Fatal("empty allowlist should allow all tools")
	}
}

func TestToolPolicy_MaxContentBytes(t *testing.T) {
	c := testPolicyChecker(t)
	c.SetPolicy("agent-1", &ToolPolicy{
		ToolLimits: map[string]ToolLimits{
			"nexus_write": {MaxContentBytes: 100},
		},
	})

	// Under limit.
	args := json.RawMessage(`{"content": "short"}`)
	d := c.Check("agent-1", "nexus_write", args)
	if !d.Allowed {
		t.Fatal("short content should be allowed")
	}

	// Over limit.
	long := strings.Repeat("x", 200)
	args = json.RawMessage(`{"content": "` + long + `"}`)
	d = c.Check("agent-1", "nexus_write", args)
	if d.Allowed {
		t.Fatal("long content should be denied")
	}
	if !strings.Contains(d.Reason, "max_content_bytes") {
		t.Fatalf("wrong reason: %s", d.Reason)
	}
}

func TestToolPolicy_MaxLimit(t *testing.T) {
	c := testPolicyChecker(t)
	c.SetPolicy("agent-1", &ToolPolicy{
		ToolLimits: map[string]ToolLimits{
			"nexus_search": {MaxLimit: 50},
		},
	})

	// Within limit.
	args := json.RawMessage(`{"limit": 20}`)
	d := c.Check("agent-1", "nexus_search", args)
	if !d.Allowed {
		t.Fatal("limit 20 should be allowed")
	}

	// Over limit.
	args = json.RawMessage(`{"limit": 100}`)
	d = c.Check("agent-1", "nexus_search", args)
	if d.Allowed {
		t.Fatal("limit 100 should be denied")
	}
	if !strings.Contains(d.Reason, "max_limit") {
		t.Fatalf("wrong reason: %s", d.Reason)
	}
}

func TestToolPolicy_AllowedProfiles(t *testing.T) {
	c := testPolicyChecker(t)
	c.SetPolicy("agent-1", &ToolPolicy{
		ToolLimits: map[string]ToolLimits{
			"nexus_search": {AllowedProfiles: []string{"fast", "balanced"}},
		},
	})

	args := json.RawMessage(`{"profile": "fast"}`)
	d := c.Check("agent-1", "nexus_search", args)
	if !d.Allowed {
		t.Fatal("fast profile should be allowed")
	}

	args = json.RawMessage(`{"profile": "deep"}`)
	d = c.Check("agent-1", "nexus_search", args)
	if d.Allowed {
		t.Fatal("deep profile should be denied")
	}
	if !strings.Contains(d.Reason, "policy_violation_profile") {
		t.Fatalf("wrong reason: %s", d.Reason)
	}
}

func TestToolPolicy_RemovePolicy(t *testing.T) {
	c := testPolicyChecker(t)
	c.SetPolicy("agent-1", &ToolPolicy{
		Denied: []string{"nexus_write"},
	})

	d := c.Check("agent-1", "nexus_write", nil)
	if d.Allowed {
		t.Fatal("should be denied before removal")
	}

	c.RemovePolicy("agent-1")

	d = c.Check("agent-1", "nexus_write", nil)
	if !d.Allowed {
		t.Fatal("should be allowed after policy removal")
	}
}

func TestToolPolicy_NoArgs(t *testing.T) {
	c := testPolicyChecker(t)
	c.SetPolicy("agent-1", &ToolPolicy{
		ToolLimits: map[string]ToolLimits{
			"nexus_write": {MaxContentBytes: 100},
		},
	})

	// nil args should be allowed.
	d := c.Check("agent-1", "nexus_write", nil)
	if !d.Allowed {
		t.Fatal("nil args should be allowed")
	}

	// Empty args should be allowed.
	d = c.Check("agent-1", "nexus_write", json.RawMessage(`{}`))
	if !d.Allowed {
		t.Fatal("empty args should be allowed")
	}
}

func TestToolPolicy_PolicyDenialLogged(t *testing.T) {
	c := testPolicyChecker(t)
	c.SetPolicy("agent-1", &ToolPolicy{
		Denied: []string{"nexus_write"},
	})

	d := c.Check("agent-1", "nexus_write", nil)
	if d.Allowed {
		t.Fatal("should be denied")
	}
	// The reason should start with "policy_denied_tool:"
	if !strings.HasPrefix(d.Reason, "policy_denied_tool:") {
		t.Fatalf("denial reason should start with policy_denied_tool:, got %q", d.Reason)
	}
}

func TestToolPolicy_HotReload(t *testing.T) {
	c := testPolicyChecker(t)

	// Initially no policy.
	d := c.Check("agent-1", "nexus_write", nil)
	if !d.Allowed {
		t.Fatal("should be allowed with no policy")
	}

	// Add restrictive policy.
	c.SetPolicy("agent-1", &ToolPolicy{
		Allowed: []string{"nexus_search"},
	})

	d = c.Check("agent-1", "nexus_write", nil)
	if d.Allowed {
		t.Fatal("should be denied after adding policy")
	}

	// Update policy to allow nexus_write.
	c.SetPolicy("agent-1", &ToolPolicy{
		Allowed: []string{"nexus_write", "nexus_search"},
	})

	d = c.Check("agent-1", "nexus_write", nil)
	if !d.Allowed {
		t.Fatal("should be allowed after policy update")
	}
}
