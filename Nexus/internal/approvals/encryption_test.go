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
	"path/filepath"
	"testing"

	"github.com/bubblefish-tech/nexus/internal/a2a/registry"
	"github.com/bubblefish-tech/nexus/internal/approvals"
	"github.com/bubblefish-tech/nexus/internal/crypto"
)

func newMKMApprovals(t *testing.T, password string) *crypto.MasterKeyManager {
	t.Helper()
	saltPath := filepath.Join(t.TempDir(), "crypto.salt")
	mkm, err := crypto.NewMasterKeyManager(password, saltPath)
	if err != nil {
		t.Fatalf("NewMasterKeyManager: %v", err)
	}
	return mkm
}

func newEncDBApprovals(t *testing.T) *sql.DB {
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
	return db
}

func TestApprovalEncryption_RoundTrip(t *testing.T) {
	db := newEncDBApprovals(t)
	s := approvals.NewStore(db)
	s.SetEncryption(newMKMApprovals(t, "testpw"))
	ctx := context.Background()
	action := json.RawMessage(`{"delete":"memory-123"}`)
	r, err := s.Create(ctx, approvals.Request{
		AgentID:    "agt_test",
		Capability: "nexus_delete",
		Action:     action,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := s.Get(ctx, r.RequestID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got.Action) != string(action) {
		t.Errorf("Action: got %s, want %s", got.Action, action)
	}
}

func TestApprovalEncryption_PlaintextColumnEmpty(t *testing.T) {
	db := newEncDBApprovals(t)
	s := approvals.NewStore(db)
	s.SetEncryption(newMKMApprovals(t, "pw"))
	ctx := context.Background()
	r, err := s.Create(ctx, approvals.Request{
		AgentID:    "agt_test",
		Capability: "nexus_delete",
		Action:     json.RawMessage(`{"target":"mem-1"}`),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	var rawAction string
	if err := db.QueryRowContext(ctx, `SELECT action_json FROM approval_requests WHERE request_id = ?`, r.RequestID).Scan(&rawAction); err != nil {
		t.Fatalf("raw query: %v", err)
	}
	if rawAction != "" {
		t.Errorf("plaintext action_json should be empty, got %q", rawAction)
	}
}

func TestApprovalEncryption_WrongKeyFails(t *testing.T) {
	db := newEncDBApprovals(t)
	sA := approvals.NewStore(db)
	sA.SetEncryption(newMKMApprovals(t, "key-A"))
	ctx := context.Background()
	r, err := sA.Create(ctx, approvals.Request{
		AgentID:    "agt_test",
		Capability: "nexus_write",
		Action:     json.RawMessage(`{"op":"write"}`),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	sB := approvals.NewStore(db)
	sB.SetEncryption(newMKMApprovals(t, "key-B"))
	_, err = sB.Get(ctx, r.RequestID)
	if err == nil {
		t.Fatal("expected decrypt error with wrong key, got nil")
	}
}

func TestApprovalEncryption_BackwardCompat(t *testing.T) {
	db := newEncDBApprovals(t)
	sPlain := approvals.NewStore(db)
	ctx := context.Background()
	r, err := sPlain.Create(ctx, approvals.Request{
		AgentID:    "agt_test",
		Capability: "nexus_write",
		Action:     json.RawMessage(`{"legacy":"true"}`),
	})
	if err != nil {
		t.Fatalf("Create (plaintext): %v", err)
	}
	sEnc := approvals.NewStore(db)
	sEnc.SetEncryption(newMKMApprovals(t, "pw"))
	got, err := sEnc.Get(ctx, r.RequestID)
	if err != nil {
		t.Fatalf("Get (encrypted store, old row): %v", err)
	}
	if string(got.Action) != `{"legacy":"true"}` {
		t.Errorf("Action: got %s", got.Action)
	}
}

func TestApprovalEncryption_DecideReasonRoundTrip(t *testing.T) {
	db := newEncDBApprovals(t)
	s := approvals.NewStore(db)
	s.SetEncryption(newMKMApprovals(t, "decidepw"))
	ctx := context.Background()
	r, err := s.Create(ctx, approvals.Request{
		AgentID:    "agt_test",
		Capability: "nexus_delete",
		Action:     json.RawMessage(`{"op":"delete"}`),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := s.Decide(ctx, r.RequestID, approvals.DecideInput{
		Decision:  approvals.DecisionApprove,
		DecidedBy: "admin",
		Reason:    "approved for maintenance window",
	}); err != nil {
		t.Fatalf("Decide: %v", err)
	}
	got, err := s.Get(ctx, r.RequestID)
	if err != nil {
		t.Fatalf("Get after Decide: %v", err)
	}
	if got.Reason != "approved for maintenance window" {
		t.Errorf("Reason: got %q", got.Reason)
	}
	if got.Status != approvals.StatusApproved {
		t.Errorf("Status: got %q", got.Status)
	}
}

func TestApprovalEncryption_DecideReasonPlaintextEmpty(t *testing.T) {
	db := newEncDBApprovals(t)
	s := approvals.NewStore(db)
	s.SetEncryption(newMKMApprovals(t, "pw"))
	ctx := context.Background()
	r, err := s.Create(ctx, approvals.Request{
		AgentID:    "agt_test",
		Capability: "nexus_write",
		Action:     json.RawMessage(`{"op":"write"}`),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := s.Decide(ctx, r.RequestID, approvals.DecideInput{
		Decision:  approvals.DecisionDeny,
		DecidedBy: "admin",
		Reason:    "not allowed",
	}); err != nil {
		t.Fatalf("Decide: %v", err)
	}
	var rawReason string
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(reason,'') FROM approval_requests WHERE request_id = ?`, r.RequestID).Scan(&rawReason); err != nil {
		t.Fatalf("raw query: %v", err)
	}
	if rawReason != "" {
		t.Errorf("plaintext reason should be empty, got %q", rawReason)
	}
}

func TestApprovalEncryption_DisabledMKMNoOp(t *testing.T) {
	saltPath := filepath.Join(t.TempDir(), "crypto.salt")
	mkm, err := crypto.NewMasterKeyManager("", saltPath)
	if err != nil {
		t.Fatalf("NewMasterKeyManager: %v", err)
	}
	if mkm.IsEnabled() {
		t.Skip("NEXUS_PASSWORD env var set; skipping disabled-MKM test")
	}
	db := newEncDBApprovals(t)
	s := approvals.NewStore(db)
	s.SetEncryption(mkm)
	ctx := context.Background()
	action := json.RawMessage(`{"op":"test"}`)
	r, err := s.Create(ctx, approvals.Request{
		AgentID:    "agt_test",
		Capability: "nexus_write",
		Action:     action,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := s.Get(ctx, r.RequestID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got.Action) != string(action) {
		t.Errorf("Action: got %s, want %s", got.Action, action)
	}
}

func TestApprovalEncryption_ListDecrypts(t *testing.T) {
	db := newEncDBApprovals(t)
	s := approvals.NewStore(db)
	s.SetEncryption(newMKMApprovals(t, "listpw"))
	ctx := context.Background()
	action := json.RawMessage(`{"list":"test"}`)
	if _, err := s.Create(ctx, approvals.Request{
		AgentID:    "agt_list",
		Capability: "nexus_write",
		Action:     action,
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	list, err := s.List(ctx, approvals.ListFilter{AgentID: "agt_list"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("want 1 request, got %d", len(list))
	}
	if string(list[0].Action) != string(action) {
		t.Errorf("Action: got %s, want %s", list[0].Action, action)
	}
}
