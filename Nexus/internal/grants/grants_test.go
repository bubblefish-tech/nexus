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

package grants_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/BubbleFish-Nexus/internal/a2a/registry"
	"github.com/BubbleFish-Nexus/internal/grants"
	_ "modernc.org/sqlite"
)

func newTestStore(t *testing.T) (*grants.Store, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	db.SetMaxOpenConns(1)
	if err := registry.InitSchema(db); err != nil {
		t.Fatalf("InitSchema: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return grants.NewStore(db), db
}

func TestNewID_HasPrefix(t *testing.T) {
	id := grants.NewID()
	if !strings.HasPrefix(id, grants.IDPrefix) {
		t.Fatalf("NewID = %q, missing prefix %q", id, grants.IDPrefix)
	}
	if len(id) != len(grants.IDPrefix)+26 {
		t.Fatalf("NewID = %q, len %d want %d", id, len(id), len(grants.IDPrefix)+26)
	}
}

func TestCreate_AssignsIDWhenEmpty(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	out, err := s.Create(ctx, grants.Grant{
		AgentID:    "agent-1",
		Capability: "nexus_write",
		GrantedBy:  "admin",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !strings.HasPrefix(out.GrantID, grants.IDPrefix) {
		t.Fatalf("GrantID = %q, want prefix %q", out.GrantID, grants.IDPrefix)
	}
}

func TestCreate_RejectsEmptyAgentID(t *testing.T) {
	s, _ := newTestStore(t)
	_, err := s.Create(context.Background(), grants.Grant{Capability: "c", GrantedBy: "x"})
	if err == nil {
		t.Fatal("expected error for empty agent_id")
	}
}

func TestCreate_RejectsEmptyCapability(t *testing.T) {
	s, _ := newTestStore(t)
	_, err := s.Create(context.Background(), grants.Grant{AgentID: "a", GrantedBy: "x"})
	if err == nil {
		t.Fatal("expected error for empty capability")
	}
}

func TestCreate_RejectsEmptyGrantedBy(t *testing.T) {
	s, _ := newTestStore(t)
	_, err := s.Create(context.Background(), grants.Grant{AgentID: "a", Capability: "c"})
	if err == nil {
		t.Fatal("expected error for empty granted_by")
	}
}

func TestCreate_DuplicateIDFails(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	id := grants.NewID()
	g := grants.Grant{GrantID: id, AgentID: "a", Capability: "c", GrantedBy: "admin"}
	if _, err := s.Create(ctx, g); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if _, err := s.Create(ctx, g); err == nil {
		t.Fatal("expected duplicate-key error")
	}
}

func TestCreate_EmptyScopeDefaultsToEmptyJSON(t *testing.T) {
	s, _ := newTestStore(t)
	out, err := s.Create(context.Background(), grants.Grant{
		AgentID: "a", Capability: "c", GrantedBy: "admin",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if string(out.Scope) != "{}" {
		t.Fatalf("Scope = %q, want {}", string(out.Scope))
	}
}

func TestGet_RoundtripsAllFields(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	expiry := time.Now().Add(1 * time.Hour).Truncate(time.Millisecond)
	scope := json.RawMessage(`{"paths":["/a","/b"]}`)
	in := grants.Grant{
		AgentID:    "agent-xyz",
		Capability: "nexus_delete",
		Scope:      scope,
		GrantedBy:  "admin@bubblefish",
		ExpiresAt:  &expiry,
	}
	created, err := s.Create(ctx, in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := s.Get(ctx, created.GrantID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AgentID != in.AgentID || got.Capability != in.Capability {
		t.Fatalf("mismatch: got %+v", got)
	}
	if string(got.Scope) != string(scope) {
		t.Fatalf("Scope = %q, want %q", got.Scope, scope)
	}
	if got.ExpiresAt == nil || !got.ExpiresAt.Equal(expiry) {
		t.Fatalf("ExpiresAt = %v, want %v", got.ExpiresAt, expiry)
	}
	if got.RevokedAt != nil {
		t.Fatalf("RevokedAt should be nil for fresh grant, got %v", got.RevokedAt)
	}
}

func TestGet_NotFoundReturnsSentinel(t *testing.T) {
	s, _ := newTestStore(t)
	_, err := s.Get(context.Background(), "gnt_nonexistent")
	if !errors.Is(err, grants.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestList_FilterByAgent(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	for _, agent := range []string{"a1", "a1", "a2"} {
		if _, err := s.Create(ctx, grants.Grant{AgentID: agent, Capability: "c", GrantedBy: "x"}); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}
	got, err := s.List(ctx, grants.ListFilter{AgentID: "a1"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d grants, want 2", len(got))
	}
	for _, g := range got {
		if g.AgentID != "a1" {
			t.Fatalf("unexpected agent_id %q in filter=a1 result", g.AgentID)
		}
	}
}

func TestList_EmptyFilterReturnsAll(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	for range 3 {
		if _, err := s.Create(ctx, grants.Grant{AgentID: "a", Capability: "c", GrantedBy: "x"}); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}
	got, err := s.List(ctx, grants.ListFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d, want 3", len(got))
	}
}

func TestList_OnlyActiveExcludesRevoked(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	g1, _ := s.Create(ctx, grants.Grant{AgentID: "a", Capability: "c", GrantedBy: "x"})
	_, _ = s.Create(ctx, grants.Grant{AgentID: "a", Capability: "c", GrantedBy: "x"})
	if err := s.Revoke(ctx, g1.GrantID, "policy violation"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	got, err := s.List(ctx, grants.ListFilter{AgentID: "a", OnlyActive: true})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d active grants, want 1", len(got))
	}
	if got[0].GrantID == g1.GrantID {
		t.Fatalf("revoked grant %q appeared in OnlyActive list", g1.GrantID)
	}
}

func TestRevoke_SetsFields(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	g, _ := s.Create(ctx, grants.Grant{AgentID: "a", Capability: "c", GrantedBy: "x"})
	before := time.Now().Add(-1 * time.Millisecond)
	if err := s.Revoke(ctx, g.GrantID, "compromised"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	got, err := s.Get(ctx, g.GrantID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.RevokedAt == nil {
		t.Fatal("RevokedAt not set after Revoke")
	}
	if got.RevokedAt.Before(before) {
		t.Fatalf("RevokedAt = %v, expected after %v", got.RevokedAt, before)
	}
	if got.RevokeReason != "compromised" {
		t.Fatalf("RevokeReason = %q, want 'compromised'", got.RevokeReason)
	}
}

func TestRevoke_NotFound(t *testing.T) {
	s, _ := newTestStore(t)
	err := s.Revoke(context.Background(), "gnt_nonexistent", "reason")
	if !errors.Is(err, grants.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestRevoke_IdempotentKeepsFirstReason(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	g, _ := s.Create(ctx, grants.Grant{AgentID: "a", Capability: "c", GrantedBy: "x"})
	if err := s.Revoke(ctx, g.GrantID, "first"); err != nil {
		t.Fatalf("Revoke 1: %v", err)
	}
	firstRevoked, _ := s.Get(ctx, g.GrantID)
	time.Sleep(2 * time.Millisecond)
	if err := s.Revoke(ctx, g.GrantID, "second"); err != nil {
		t.Fatalf("Revoke 2: %v", err)
	}
	got, _ := s.Get(ctx, g.GrantID)
	if got.RevokeReason != "first" {
		t.Fatalf("reason = %q, want 'first' (idempotent)", got.RevokeReason)
	}
	if !got.RevokedAt.Equal(*firstRevoked.RevokedAt) {
		t.Fatalf("RevokedAt changed on second revoke: was %v, now %v", firstRevoked.RevokedAt, got.RevokedAt)
	}
}

func TestCheckGrant_ReturnsActive(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	created, _ := s.Create(ctx, grants.Grant{AgentID: "a", Capability: "nexus_write", GrantedBy: "x"})
	got, err := s.CheckGrant(ctx, "a", "nexus_write")
	if err != nil {
		t.Fatalf("CheckGrant: %v", err)
	}
	if got == nil {
		t.Fatal("CheckGrant returned nil for active grant")
	}
	if got.GrantID != created.GrantID {
		t.Fatalf("GrantID = %q, want %q", got.GrantID, created.GrantID)
	}
}

func TestCheckGrant_NoMatchReturnsNilNil(t *testing.T) {
	s, _ := newTestStore(t)
	got, err := s.CheckGrant(context.Background(), "ghost", "c")
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if got != nil {
		t.Fatalf("got = %+v, want nil", got)
	}
}

func TestCheckGrant_SkipsRevoked(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	g, _ := s.Create(ctx, grants.Grant{AgentID: "a", Capability: "c", GrantedBy: "x"})
	if err := s.Revoke(ctx, g.GrantID, "test"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	got, err := s.CheckGrant(ctx, "a", "c")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got != nil {
		t.Fatalf("CheckGrant returned revoked grant: %+v", got)
	}
}

func TestCheckGrant_SkipsExpired(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	expired := time.Now().Add(-1 * time.Hour)
	_, _ = s.Create(ctx, grants.Grant{
		AgentID: "a", Capability: "c", GrantedBy: "x", ExpiresAt: &expired,
	})
	got, err := s.CheckGrant(ctx, "a", "c")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got != nil {
		t.Fatalf("CheckGrant returned expired grant: %+v", got)
	}
}

func TestCheckGrant_ReturnsMostRecentWhenMultiple(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	_, _ = s.Create(ctx, grants.Grant{AgentID: "a", Capability: "c", GrantedBy: "x"})
	time.Sleep(2 * time.Millisecond)
	g2, _ := s.Create(ctx, grants.Grant{AgentID: "a", Capability: "c", GrantedBy: "x"})
	got, err := s.CheckGrant(ctx, "a", "c")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got.GrantID != g2.GrantID {
		t.Fatalf("GrantID = %q, want most-recent %q", got.GrantID, g2.GrantID)
	}
}

func TestIsActive_TruthTable(t *testing.T) {
	now := time.Now()
	past := now.Add(-1 * time.Hour)
	future := now.Add(1 * time.Hour)
	cases := []struct {
		name   string
		grant  grants.Grant
		active bool
	}{
		{"fresh_no_expiry", grants.Grant{}, true},
		{"revoked", grants.Grant{RevokedAt: &past}, false},
		{"expired", grants.Grant{ExpiresAt: &past}, false},
		{"expires_future", grants.Grant{ExpiresAt: &future}, true},
		{"revoked_wins_over_future_expiry", grants.Grant{RevokedAt: &past, ExpiresAt: &future}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.grant.IsActive(now); got != tc.active {
				t.Fatalf("IsActive = %v, want %v", got, tc.active)
			}
		})
	}
}
