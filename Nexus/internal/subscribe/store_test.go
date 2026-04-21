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

package subscribe

import (
	"database/sql"
	"errors"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestStore_AddAndGet(t *testing.T) {
	db := testDB(t)
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}

	sub, err := store.Add("agent-1", "competitive intelligence")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(sub.ID, IDPrefix) {
		t.Errorf("expected ID prefix %q, got %q", IDPrefix, sub.ID)
	}
	if sub.AgentID != "agent-1" {
		t.Errorf("expected agent_id agent-1, got %q", sub.AgentID)
	}
	if sub.Filter != "competitive intelligence" {
		t.Errorf("expected filter 'competitive intelligence', got %q", sub.Filter)
	}
	if !sub.Active {
		t.Error("expected active=true")
	}
	if sub.MatchCount != 0 {
		t.Errorf("expected match_count=0, got %d", sub.MatchCount)
	}

	got, err := store.Get(sub.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != sub.ID {
		t.Errorf("Get returned wrong ID: %q vs %q", got.ID, sub.ID)
	}
}

func TestStore_Remove(t *testing.T) {
	db := testDB(t)
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}

	sub, _ := store.Add("agent-1", "bug reports")
	if err := store.Remove(sub.ID); err != nil {
		t.Fatal(err)
	}

	_, err = store.Get(sub.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound after remove, got %v", err)
	}
}

func TestStore_RemoveNonExistent(t *testing.T) {
	db := testDB(t)
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}

	err = store.Remove("sub_NONEXISTENT")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestStore_ListForAgent(t *testing.T) {
	db := testDB(t)
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}

	store.Add("agent-1", "topic A")
	store.Add("agent-1", "topic B")
	store.Add("agent-2", "topic C")

	subs := store.ListForAgent("agent-1")
	if len(subs) != 2 {
		t.Errorf("expected 2 subs for agent-1, got %d", len(subs))
	}

	subs = store.ListForAgent("agent-2")
	if len(subs) != 1 {
		t.Errorf("expected 1 sub for agent-2, got %d", len(subs))
	}

	subs = store.ListForAgent("agent-3")
	if len(subs) != 0 {
		t.Errorf("expected 0 subs for agent-3, got %d", len(subs))
	}
}

func TestStore_All(t *testing.T) {
	db := testDB(t)
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}

	store.Add("agent-1", "topic A")
	store.Add("agent-2", "topic B")
	store.Add("agent-3", "topic C")

	all := store.All()
	if len(all) != 3 {
		t.Errorf("expected 3 active subs, got %d", len(all))
	}
}

func TestStore_IncrementMatch(t *testing.T) {
	db := testDB(t)
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}

	sub, _ := store.Add("agent-1", "topic A")
	store.IncrementMatch(sub.ID)
	store.IncrementMatch(sub.ID)
	store.IncrementMatch(sub.ID)

	got, err := store.Get(sub.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.MatchCount != 3 {
		t.Errorf("expected match_count=3, got %d", got.MatchCount)
	}
}

func TestStore_PersistAcrossInstances(t *testing.T) {
	db := testDB(t)
	store1, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}

	sub, _ := store1.Add("agent-1", "persisted filter")
	store1.IncrementMatch(sub.ID)

	store2, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}

	got, err := store2.Get(sub.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Filter != "persisted filter" {
		t.Errorf("expected persisted filter, got %q", got.Filter)
	}
	if got.MatchCount != 1 {
		t.Errorf("expected match_count=1 after persist, got %d", got.MatchCount)
	}
}
