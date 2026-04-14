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

package agent

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func newTestRegistry(t *testing.T) *Registry {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	reg, err := NewRegistry(db)
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

func TestNewRegistry_NilDB(t *testing.T) {
	_, err := NewRegistry(nil)
	if err == nil {
		t.Fatal("expected error for nil db")
	}
}

func TestRegisterAndGet(t *testing.T) {
	reg := newTestRegistry(t)

	id, err := reg.Register("test-agent", "a test agent")
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		t.Fatal("expected non-empty agent ID")
	}
	if len(id) != 32 {
		t.Fatalf("expected 32-char hex ID, got %d chars: %s", len(id), id)
	}

	a, err := reg.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if a == nil {
		t.Fatal("expected agent, got nil")
	}
	if a.Name != "test-agent" {
		t.Fatalf("expected name %q, got %q", "test-agent", a.Name)
	}
	if a.Description != "a test agent" {
		t.Fatalf("expected description %q, got %q", "a test agent", a.Description)
	}
	if a.Status != StatusActive {
		t.Fatalf("expected status %q, got %q", StatusActive, a.Status)
	}
	if a.CreatedAt.IsZero() {
		t.Fatal("expected non-zero CreatedAt")
	}
}

func TestRegister_EmptyName(t *testing.T) {
	reg := newTestRegistry(t)
	_, err := reg.Register("", "desc")
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestRegister_DuplicateName(t *testing.T) {
	reg := newTestRegistry(t)

	_, err := reg.Register("dup", "first")
	if err != nil {
		t.Fatal(err)
	}
	_, err = reg.Register("dup", "second")
	if err == nil {
		t.Fatal("expected error for duplicate name")
	}
}

func TestGetByName(t *testing.T) {
	reg := newTestRegistry(t)

	id, err := reg.Register("named-agent", "")
	if err != nil {
		t.Fatal(err)
	}

	a, err := reg.GetByName("named-agent")
	if err != nil {
		t.Fatal(err)
	}
	if a == nil {
		t.Fatal("expected agent")
	}
	if a.ID != id {
		t.Fatalf("expected ID %q, got %q", id, a.ID)
	}
}

func TestGetByName_NotFound(t *testing.T) {
	reg := newTestRegistry(t)

	a, err := reg.GetByName("nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if a != nil {
		t.Fatal("expected nil for nonexistent agent")
	}
}

func TestGet_NotFound(t *testing.T) {
	reg := newTestRegistry(t)

	a, err := reg.Get("0000000000000000")
	if err != nil {
		t.Fatal(err)
	}
	if a != nil {
		t.Fatal("expected nil for nonexistent agent")
	}
}

func TestList(t *testing.T) {
	reg := newTestRegistry(t)

	_, err := reg.Register("alpha", "first")
	if err != nil {
		t.Fatal(err)
	}
	_, err = reg.Register("bravo", "second")
	if err != nil {
		t.Fatal(err)
	}

	agents, err := reg.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(agents))
	}
	if agents[0].Name != "alpha" {
		t.Fatalf("expected first agent %q, got %q", "alpha", agents[0].Name)
	}
	if agents[1].Name != "bravo" {
		t.Fatalf("expected second agent %q, got %q", "bravo", agents[1].Name)
	}
}

func TestList_Empty(t *testing.T) {
	reg := newTestRegistry(t)

	agents, err := reg.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 0 {
		t.Fatalf("expected 0 agents, got %d", len(agents))
	}
}

func TestSuspend(t *testing.T) {
	reg := newTestRegistry(t)

	id, _ := reg.Register("suspendable", "")
	if err := reg.Suspend(id); err != nil {
		t.Fatal(err)
	}

	a, _ := reg.Get(id)
	if a.Status != StatusSuspended {
		t.Fatalf("expected status %q, got %q", StatusSuspended, a.Status)
	}
}

func TestSuspend_NotFound(t *testing.T) {
	reg := newTestRegistry(t)
	if err := reg.Suspend("nonexistent"); err == nil {
		t.Fatal("expected error for nonexistent agent")
	}
}

func TestRetire(t *testing.T) {
	reg := newTestRegistry(t)

	id, _ := reg.Register("retirable", "")
	if err := reg.Retire(id); err != nil {
		t.Fatal(err)
	}

	a, _ := reg.Get(id)
	if a.Status != StatusRetired {
		t.Fatalf("expected status %q, got %q", StatusRetired, a.Status)
	}
}

func TestReactivate(t *testing.T) {
	reg := newTestRegistry(t)

	id, _ := reg.Register("reactivatable", "")
	_ = reg.Suspend(id)
	if err := reg.Reactivate(id); err != nil {
		t.Fatal(err)
	}

	a, _ := reg.Get(id)
	if a.Status != StatusActive {
		t.Fatalf("expected status %q, got %q", StatusActive, a.Status)
	}
}

func TestTouchLastSeen(t *testing.T) {
	reg := newTestRegistry(t)

	id, _ := reg.Register("touchable", "")

	a, _ := reg.Get(id)
	if !a.LastSeenAt.IsZero() {
		t.Fatal("expected zero LastSeenAt before touch")
	}

	if err := reg.TouchLastSeen(id); err != nil {
		t.Fatal(err)
	}

	a, _ = reg.Get(id)
	if a.LastSeenAt.IsZero() {
		t.Fatal("expected non-zero LastSeenAt after touch")
	}
}

func TestTouchLastSeen_NotFound(t *testing.T) {
	reg := newTestRegistry(t)
	if err := reg.TouchLastSeen("nonexistent"); err == nil {
		t.Fatal("expected error for nonexistent agent")
	}
}

func TestLifecycleFlow(t *testing.T) {
	reg := newTestRegistry(t)

	// Register → active
	id, _ := reg.Register("lifecycle", "lifecycle test")
	a, _ := reg.Get(id)
	if a.Status != StatusActive {
		t.Fatalf("step 1: expected active, got %s", a.Status)
	}

	// Suspend
	_ = reg.Suspend(id)
	a, _ = reg.Get(id)
	if a.Status != StatusSuspended {
		t.Fatalf("step 2: expected suspended, got %s", a.Status)
	}

	// Reactivate
	_ = reg.Reactivate(id)
	a, _ = reg.Get(id)
	if a.Status != StatusActive {
		t.Fatalf("step 3: expected active, got %s", a.Status)
	}

	// Retire
	_ = reg.Retire(id)
	a, _ = reg.Get(id)
	if a.Status != StatusRetired {
		t.Fatalf("step 4: expected retired, got %s", a.Status)
	}

	// Retired agent still appears in list (audit history preserved)
	agents, _ := reg.List()
	if len(agents) != 1 {
		t.Fatalf("step 5: expected 1 agent in list, got %d", len(agents))
	}
}
