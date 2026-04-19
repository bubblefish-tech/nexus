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

package approvals_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/bubblefish-tech/nexus/internal/a2a/registry"
	"github.com/bubblefish-tech/nexus/internal/approvals"
	_ "modernc.org/sqlite"
)

func newTestStore(t *testing.T) *approvals.Store {
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
	return approvals.NewStore(db)
}

func sampleAction() json.RawMessage {
	return json.RawMessage(`{"target":"memory-123","op":"delete"}`)
}

func TestNewID_HasPrefix(t *testing.T) {
	id := approvals.NewID()
	if !strings.HasPrefix(id, approvals.IDPrefix) {
		t.Fatalf("NewID = %q, missing prefix %q", id, approvals.IDPrefix)
	}
}

func TestCreate_AssignsIDAndDefaultsPending(t *testing.T) {
	s := newTestStore(t)
	r, err := s.Create(context.Background(), approvals.Request{
		AgentID:    "a1",
		Capability: "nexus_delete",
		Action:     sampleAction(),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !strings.HasPrefix(r.RequestID, approvals.IDPrefix) {
		t.Fatalf("RequestID = %q, want prefix %q", r.RequestID, approvals.IDPrefix)
	}
	if r.Status != approvals.StatusPending {
		t.Fatalf("Status = %q, want %q", r.Status, approvals.StatusPending)
	}
}

func TestCreate_RejectsEmptyAgentID(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Create(context.Background(), approvals.Request{Capability: "c", Action: sampleAction()})
	if err == nil {
		t.Fatal("expected error for empty agent_id")
	}
}

func TestCreate_RejectsEmptyCapability(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Create(context.Background(), approvals.Request{AgentID: "a", Action: sampleAction()})
	if err == nil {
		t.Fatal("expected error for empty capability")
	}
}

func TestCreate_RejectsEmptyAction(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Create(context.Background(), approvals.Request{AgentID: "a", Capability: "c"})
	if err == nil {
		t.Fatal("expected error for empty action")
	}
}

func TestCreate_RejectsInvalidJSON(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Create(context.Background(), approvals.Request{
		AgentID: "a", Capability: "c", Action: json.RawMessage(`{not json`),
	})
	if err == nil {
		t.Fatal("expected error for malformed JSON action")
	}
}

func TestGet_RoundtripsFields(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	in := approvals.Request{
		AgentID:    "a1",
		Capability: "nexus_delete",
		Action:     sampleAction(),
	}
	created, err := s.Create(ctx, in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := s.Get(ctx, created.RequestID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AgentID != in.AgentID || got.Capability != in.Capability {
		t.Fatalf("mismatch: %+v", got)
	}
	if string(got.Action) != string(in.Action) {
		t.Fatalf("Action = %q, want %q", got.Action, in.Action)
	}
	if got.DecidedAt != nil {
		t.Fatalf("DecidedAt should be nil on fresh request, got %v", got.DecidedAt)
	}
}

func TestGet_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Get(context.Background(), "apr_nonexistent")
	if !errors.Is(err, approvals.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestList_FilterByStatus(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	r1, _ := s.Create(ctx, approvals.Request{AgentID: "a", Capability: "c", Action: sampleAction()})
	_, _ = s.Create(ctx, approvals.Request{AgentID: "a", Capability: "c", Action: sampleAction()})
	_ = s.Decide(ctx, r1.RequestID, approvals.DecideInput{
		Decision: approvals.DecisionApprove, DecidedBy: "admin",
	})
	pending, err := s.List(ctx, approvals.ListFilter{Status: approvals.StatusPending})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("got %d pending, want 1", len(pending))
	}
	approved, _ := s.List(ctx, approvals.ListFilter{Status: approvals.StatusApproved})
	if len(approved) != 1 {
		t.Fatalf("got %d approved, want 1", len(approved))
	}
}

func TestListPending_ReturnsOnlyPending(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	r1, _ := s.Create(ctx, approvals.Request{AgentID: "a", Capability: "c", Action: sampleAction()})
	_, _ = s.Create(ctx, approvals.Request{AgentID: "a", Capability: "c", Action: sampleAction()})
	_ = s.Decide(ctx, r1.RequestID, approvals.DecideInput{
		Decision: approvals.DecisionDeny, DecidedBy: "admin", Reason: "unsafe",
	})
	got, err := s.ListPending(ctx)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d pending, want 1", len(got))
	}
}

func TestList_FilterByAgent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for _, a := range []string{"a", "a", "b"} {
		_, _ = s.Create(ctx, approvals.Request{AgentID: a, Capability: "c", Action: sampleAction()})
	}
	got, _ := s.List(ctx, approvals.ListFilter{AgentID: "a"})
	if len(got) != 2 {
		t.Fatalf("got %d, want 2", len(got))
	}
}

func TestDecide_Approve(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	r, _ := s.Create(ctx, approvals.Request{AgentID: "a", Capability: "c", Action: sampleAction()})
	if err := s.Decide(ctx, r.RequestID, approvals.DecideInput{
		Decision: approvals.DecisionApprove, DecidedBy: "admin", Reason: "ok",
	}); err != nil {
		t.Fatalf("Decide: %v", err)
	}
	got, _ := s.Get(ctx, r.RequestID)
	if got.Status != approvals.StatusApproved {
		t.Fatalf("Status = %q, want approved", got.Status)
	}
	if got.Decision != approvals.DecisionApprove {
		t.Fatalf("Decision = %q, want %q", got.Decision, approvals.DecisionApprove)
	}
	if got.DecidedBy != "admin" {
		t.Fatalf("DecidedBy = %q, want admin", got.DecidedBy)
	}
	if got.DecidedAt == nil {
		t.Fatal("DecidedAt not set")
	}
}

func TestDecide_Deny(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	r, _ := s.Create(ctx, approvals.Request{AgentID: "a", Capability: "c", Action: sampleAction()})
	if err := s.Decide(ctx, r.RequestID, approvals.DecideInput{
		Decision: approvals.DecisionDeny, DecidedBy: "admin", Reason: "risk",
	}); err != nil {
		t.Fatalf("Decide: %v", err)
	}
	got, _ := s.Get(ctx, r.RequestID)
	if got.Status != approvals.StatusDenied {
		t.Fatalf("Status = %q, want denied", got.Status)
	}
	if got.Reason != "risk" {
		t.Fatalf("Reason = %q, want risk", got.Reason)
	}
}

func TestDecide_RejectsInvalidDecision(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	r, _ := s.Create(ctx, approvals.Request{AgentID: "a", Capability: "c", Action: sampleAction()})
	err := s.Decide(ctx, r.RequestID, approvals.DecideInput{Decision: "maybe", DecidedBy: "admin"})
	if err == nil {
		t.Fatal("expected error for invalid decision")
	}
}

func TestDecide_RejectsEmptyDecidedBy(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	r, _ := s.Create(ctx, approvals.Request{AgentID: "a", Capability: "c", Action: sampleAction()})
	err := s.Decide(ctx, r.RequestID, approvals.DecideInput{Decision: approvals.DecisionApprove})
	if err == nil {
		t.Fatal("expected error for missing decided_by")
	}
}

func TestDecide_NotFound(t *testing.T) {
	s := newTestStore(t)
	err := s.Decide(context.Background(), "apr_nope", approvals.DecideInput{
		Decision: approvals.DecisionApprove, DecidedBy: "admin",
	})
	if !errors.Is(err, approvals.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestDecide_AlreadyDecided(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	r, _ := s.Create(ctx, approvals.Request{AgentID: "a", Capability: "c", Action: sampleAction()})
	if err := s.Decide(ctx, r.RequestID, approvals.DecideInput{
		Decision: approvals.DecisionApprove, DecidedBy: "admin",
	}); err != nil {
		t.Fatalf("Decide 1: %v", err)
	}
	err := s.Decide(ctx, r.RequestID, approvals.DecideInput{
		Decision: approvals.DecisionDeny, DecidedBy: "admin",
	})
	if !errors.Is(err, approvals.ErrAlreadyDecided) {
		t.Fatalf("err = %v, want ErrAlreadyDecided", err)
	}
}

func TestExpire_Transitions(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	r, _ := s.Create(ctx, approvals.Request{AgentID: "a", Capability: "c", Action: sampleAction()})
	if err := s.Expire(ctx, r.RequestID); err != nil {
		t.Fatalf("Expire: %v", err)
	}
	got, _ := s.Get(ctx, r.RequestID)
	if got.Status != approvals.StatusExpired {
		t.Fatalf("Status = %q, want expired", got.Status)
	}
	if got.DecidedAt == nil {
		t.Fatal("DecidedAt not set on expire")
	}
}

func TestExpire_NotFound(t *testing.T) {
	s := newTestStore(t)
	err := s.Expire(context.Background(), "apr_nope")
	if !errors.Is(err, approvals.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestExpire_AlreadyDecided(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	r, _ := s.Create(ctx, approvals.Request{AgentID: "a", Capability: "c", Action: sampleAction()})
	_ = s.Decide(ctx, r.RequestID, approvals.DecideInput{
		Decision: approvals.DecisionApprove, DecidedBy: "admin",
	})
	err := s.Expire(ctx, r.RequestID)
	if !errors.Is(err, approvals.ErrAlreadyDecided) {
		t.Fatalf("err = %v, want ErrAlreadyDecided", err)
	}
}

func TestList_Limit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for range 5 {
		_, _ = s.Create(ctx, approvals.Request{AgentID: "a", Capability: "c", Action: sampleAction()})
	}
	got, err := s.List(ctx, approvals.ListFilter{Limit: 3})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d, want 3 (limit)", len(got))
	}
}

func TestList_OrderedNewestFirst(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	r1, _ := s.Create(ctx, approvals.Request{AgentID: "a", Capability: "c", Action: sampleAction()})
	time.Sleep(2 * time.Millisecond)
	r2, _ := s.Create(ctx, approvals.Request{AgentID: "a", Capability: "c", Action: sampleAction()})
	got, _ := s.List(ctx, approvals.ListFilter{})
	if len(got) != 2 {
		t.Fatalf("got %d, want 2", len(got))
	}
	if got[0].RequestID != r2.RequestID || got[1].RequestID != r1.RequestID {
		t.Fatalf("ordering mismatch: got [%s, %s], want [%s, %s]",
			got[0].RequestID, got[1].RequestID, r2.RequestID, r1.RequestID)
	}
}
