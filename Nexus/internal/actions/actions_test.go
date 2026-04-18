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

package actions_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/BubbleFish-Nexus/internal/a2a/registry"
	"github.com/BubbleFish-Nexus/internal/actions"
	_ "modernc.org/sqlite"
)

func newTestStore(t *testing.T) *actions.Store {
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
	return actions.NewStore(db)
}

func TestNewID_HasPrefix(t *testing.T) {
	id := actions.NewID()
	if !strings.HasPrefix(id, actions.IDPrefix) {
		t.Fatalf("NewID = %q, missing prefix %q", id, actions.IDPrefix)
	}
}

func TestRecord_AssignsID(t *testing.T) {
	s := newTestStore(t)
	out, err := s.Record(context.Background(), actions.Action{
		AgentID: "a1", Capability: "nexus_write", PolicyDecision: actions.DecisionAllow,
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if !strings.HasPrefix(out.ActionID, actions.IDPrefix) {
		t.Fatalf("ActionID = %q", out.ActionID)
	}
}

func TestRecord_RejectsMissingRequired(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	cases := []actions.Action{
		{Capability: "c", PolicyDecision: "allow"},
		{AgentID: "a", PolicyDecision: "allow"},
		{AgentID: "a", Capability: "c"},
	}
	for i, a := range cases {
		if _, err := s.Record(ctx, a); err == nil {
			t.Errorf("case %d: expected error, got nil", i)
		}
	}
}

func TestRecord_RoundtripsAllFields(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	in := actions.Action{
		AgentID:        "a1",
		Capability:     "nexus_delete",
		Target:         "memory-abc",
		GrantID:        "gnt_01",
		ApprovalID:     "apr_01",
		PolicyDecision: actions.DecisionAllow,
		PolicyReason:   "grant match",
		Result:         "ok",
		AuditHash:      "sha256:beef",
	}
	out, err := s.Record(ctx, in)
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	got, err := s.Get(ctx, out.ActionID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AgentID != in.AgentID || got.Capability != in.Capability {
		t.Fatalf("mismatch: %+v", got)
	}
	if got.Target != in.Target || got.GrantID != in.GrantID || got.ApprovalID != in.ApprovalID {
		t.Fatalf("references not preserved: %+v", got)
	}
	if got.PolicyReason != in.PolicyReason || got.Result != in.Result || got.AuditHash != in.AuditHash {
		t.Fatalf("aux fields not preserved: %+v", got)
	}
}

func TestRecord_EmptyOptionalsStoredAsEmpty(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	out, err := s.Record(ctx, actions.Action{
		AgentID: "a", Capability: "c", PolicyDecision: actions.DecisionDeny,
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	got, err := s.Get(ctx, out.ActionID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Target != "" || got.GrantID != "" || got.ApprovalID != "" {
		t.Fatalf("expected empty optional fields, got %+v", got)
	}
}

func TestGet_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Get(context.Background(), "act_nope")
	if !errors.Is(err, actions.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestQuery_FilterByAgent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for _, a := range []string{"a", "a", "b"} {
		_, _ = s.Record(ctx, actions.Action{AgentID: a, Capability: "c", PolicyDecision: "allow"})
	}
	got, _ := s.Query(ctx, actions.QueryFilter{AgentID: "a"})
	if len(got) != 2 {
		t.Fatalf("got %d, want 2", len(got))
	}
}

func TestQuery_FilterByCapability(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for _, c := range []string{"write", "write", "delete"} {
		_, _ = s.Record(ctx, actions.Action{AgentID: "a", Capability: c, PolicyDecision: "allow"})
	}
	got, _ := s.Query(ctx, actions.QueryFilter{Capability: "write"})
	if len(got) != 2 {
		t.Fatalf("got %d write, want 2", len(got))
	}
}

func TestQuery_FilterByDecision(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_, _ = s.Record(ctx, actions.Action{AgentID: "a", Capability: "c", PolicyDecision: actions.DecisionAllow})
	_, _ = s.Record(ctx, actions.Action{AgentID: "a", Capability: "c", PolicyDecision: actions.DecisionDeny})
	got, _ := s.Query(ctx, actions.QueryFilter{PolicyDecision: actions.DecisionDeny})
	if len(got) != 1 {
		t.Fatalf("got %d deny, want 1", len(got))
	}
}

func TestQuery_TimeRange(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	t0, _ := s.Record(ctx, actions.Action{AgentID: "a", Capability: "c", PolicyDecision: "allow"})
	time.Sleep(5 * time.Millisecond)
	between := time.Now()
	time.Sleep(5 * time.Millisecond)
	t1, _ := s.Record(ctx, actions.Action{AgentID: "a", Capability: "c", PolicyDecision: "allow"})

	// Since `between`: only t1.
	got, err := s.Query(ctx, actions.QueryFilter{Since: between})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 1 || got[0].ActionID != t1.ActionID {
		t.Fatalf("Since filter wrong: got %v, want [%s]", got, t1.ActionID)
	}

	// Until `between`: only t0.
	got, err = s.Query(ctx, actions.QueryFilter{Until: between})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 1 || got[0].ActionID != t0.ActionID {
		t.Fatalf("Until filter wrong: got %v, want [%s]", got, t0.ActionID)
	}
}

func TestQuery_Limit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for range 5 {
		_, _ = s.Record(ctx, actions.Action{AgentID: "a", Capability: "c", PolicyDecision: "allow"})
	}
	got, _ := s.Query(ctx, actions.QueryFilter{Limit: 2})
	if len(got) != 2 {
		t.Fatalf("got %d, want 2 (limit)", len(got))
	}
}

func TestQuery_OrderedNewestFirst(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	t0, _ := s.Record(ctx, actions.Action{AgentID: "a", Capability: "c", PolicyDecision: "allow"})
	time.Sleep(2 * time.Millisecond)
	t1, _ := s.Record(ctx, actions.Action{AgentID: "a", Capability: "c", PolicyDecision: "allow"})
	got, _ := s.Query(ctx, actions.QueryFilter{})
	if len(got) != 2 {
		t.Fatalf("got %d, want 2", len(got))
	}
	if got[0].ActionID != t1.ActionID || got[1].ActionID != t0.ActionID {
		t.Fatalf("order = [%s, %s], want [%s, %s]",
			got[0].ActionID, got[1].ActionID, t1.ActionID, t0.ActionID)
	}
}

func TestQuery_EmptyResultSet(t *testing.T) {
	s := newTestStore(t)
	got, err := s.Query(context.Background(), actions.QueryFilter{AgentID: "ghost"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d rows, want 0", len(got))
	}
}
