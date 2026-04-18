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

import (
	"strings"
	"testing"
	"time"
)

func TestNewMessageID(t *testing.T) {
	id := NewMessageID()
	if !strings.HasPrefix(id, PrefixMessage) {
		t.Errorf("expected prefix %q, got %q", PrefixMessage, id)
	}
	if err := ValidateID(id); err != nil {
		t.Errorf("generated ID should be valid: %v", err)
	}
}

func TestNewTaskID(t *testing.T) {
	id := NewTaskID()
	if !strings.HasPrefix(id, PrefixTask) {
		t.Errorf("expected prefix %q, got %q", PrefixTask, id)
	}
	if err := ValidateID(id); err != nil {
		t.Errorf("generated ID should be valid: %v", err)
	}
}

func TestNewContextID(t *testing.T) {
	id := NewContextID()
	if !strings.HasPrefix(id, PrefixContext) {
		t.Errorf("expected prefix %q, got %q", PrefixContext, id)
	}
	if err := ValidateID(id); err != nil {
		t.Errorf("generated ID should be valid: %v", err)
	}
}

func TestNewArtifactID(t *testing.T) {
	id := NewArtifactID()
	if !strings.HasPrefix(id, PrefixArtifact) {
		t.Errorf("expected prefix %q, got %q", PrefixArtifact, id)
	}
}

func TestNewGrantID(t *testing.T) {
	id := NewGrantID()
	if !strings.HasPrefix(id, PrefixGrant) {
		t.Errorf("expected prefix %q, got %q", PrefixGrant, id)
	}
}

func TestNewAuditID(t *testing.T) {
	id := NewAuditID()
	if !strings.HasPrefix(id, PrefixAudit) {
		t.Errorf("expected prefix %q, got %q", PrefixAudit, id)
	}
}

func TestNewApprovalID(t *testing.T) {
	id := NewApprovalID()
	if !strings.HasPrefix(id, PrefixApproval) {
		t.Errorf("expected prefix %q, got %q", PrefixApproval, id)
	}
}

func TestIDUniqueness(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		id := NewTaskID()
		if seen[id] {
			t.Fatalf("duplicate ID generated on iteration %d: %s", i, id)
		}
		seen[id] = true
	}
}

func TestIDTimestampOrdering(t *testing.T) {
	// IDs generated across millisecond boundaries should have increasing
	// timestamp components. Within the same millisecond, the random
	// component may not be monotonic across pool reuse.
	id1 := NewTaskID()
	// The ULID timestamp is the first 10 characters of the suffix.
	ts1 := id1[len(PrefixTask) : len(PrefixTask)+10]

	// Sleep to guarantee a different millisecond
	time.Sleep(2 * time.Millisecond)
	id2 := NewTaskID()
	ts2 := id2[len(PrefixTask) : len(PrefixTask)+10]

	if ts2 < ts1 {
		t.Errorf("later ID should have >= timestamp: %q vs %q", ts1, ts2)
	}
}

func TestIDPrefixLength(t *testing.T) {
	// All prefixes should be 4 characters (3 letters + underscore)
	for _, p := range AllPrefixes() {
		if len(p) != 4 {
			t.Errorf("prefix %q should be 4 characters, got %d", p, len(p))
		}
		if p[3] != '_' {
			t.Errorf("prefix %q should end with underscore", p)
		}
	}
}

func TestIDLength(t *testing.T) {
	// Each ID should be prefix (4) + ULID (26) = 30 characters
	generators := []func() string{
		NewMessageID, NewTaskID, NewContextID,
		NewArtifactID, NewGrantID, NewAuditID, NewApprovalID,
	}
	for _, gen := range generators {
		id := gen()
		if len(id) != 30 {
			t.Errorf("ID %q should be 30 characters, got %d", id, len(id))
		}
	}
}

func TestValidateIDValid(t *testing.T) {
	tests := []func() string{
		NewMessageID, NewTaskID, NewContextID,
		NewArtifactID, NewGrantID, NewAuditID, NewApprovalID,
	}
	for _, gen := range tests {
		id := gen()
		if err := ValidateID(id); err != nil {
			t.Errorf("valid ID %q rejected: %v", id, err)
		}
	}
}

func TestValidateIDInvalid(t *testing.T) {
	tests := []struct {
		name string
		id   string
	}{
		{"empty", ""},
		{"no prefix", "01JBQZ8F0F3C9Z2Y7K4X8M5N6P"},
		{"unknown prefix", "xyz_01JBQZ8F0F3C9Z2Y7K4X8M5N6P"},
		{"short ulid", "msg_01JBQZ"},
		{"bad ulid chars", "msg_!@#$%^&*()+-=[]{}|;:',.<>"},
		{"just prefix", "msg_"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateID(tt.id); err == nil {
				t.Errorf("expected error for invalid ID %q", tt.id)
			}
		})
	}
}

func TestIDPrefix(t *testing.T) {
	tests := []struct {
		id     string
		expect string
	}{
		{NewMessageID(), PrefixMessage},
		{NewTaskID(), PrefixTask},
		{NewContextID(), PrefixContext},
		{"unknown_foo", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := IDPrefix(tt.id); got != tt.expect {
			t.Errorf("IDPrefix(%q) = %q, want %q", tt.id, got, tt.expect)
		}
	}
}
