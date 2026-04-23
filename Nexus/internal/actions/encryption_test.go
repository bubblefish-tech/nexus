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
	"path/filepath"
	"testing"

	"github.com/bubblefish-tech/nexus/internal/a2a/registry"
	"github.com/bubblefish-tech/nexus/internal/actions"
	"github.com/bubblefish-tech/nexus/internal/crypto"
)

func newMKMActions(t *testing.T, password string) *crypto.MasterKeyManager {
	t.Helper()
	saltPath := filepath.Join(t.TempDir(), "crypto.salt")
	mkm, err := crypto.NewMasterKeyManager(password, saltPath)
	if err != nil {
		t.Fatalf("NewMasterKeyManager: %v", err)
	}
	return mkm
}

func newEncDBActions(t *testing.T) *sql.DB {
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

func TestActionEncryption_RoundTrip(t *testing.T) {
	db := newEncDBActions(t)
	s := actions.NewStore(db)
	s.SetEncryption(newMKMActions(t, "actpw"))
	ctx := context.Background()
	a, err := s.Record(ctx, actions.Action{
		AgentID:        "agt_test",
		Capability:     "nexus_write",
		PolicyDecision: actions.DecisionAllow,
		PolicyReason:   "grant gnt_abc active",
		Result:         "memory written",
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	got, err := s.Get(ctx, a.ActionID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.PolicyReason != "grant gnt_abc active" {
		t.Errorf("PolicyReason: got %q, want %q", got.PolicyReason, "grant gnt_abc active")
	}
	if got.Result != "memory written" {
		t.Errorf("Result: got %q, want %q", got.Result, "memory written")
	}
}

func TestActionEncryption_PlaintextColumnsNull(t *testing.T) {
	db := newEncDBActions(t)
	s := actions.NewStore(db)
	s.SetEncryption(newMKMActions(t, "pw"))
	ctx := context.Background()
	a, err := s.Record(ctx, actions.Action{
		AgentID:        "agt_test",
		Capability:     "nexus_delete",
		PolicyDecision: actions.DecisionAllow,
		PolicyReason:   "sensitive reason",
		Result:         "deleted",
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	var rawReason, rawResult sql.NullString
	if err := db.QueryRowContext(ctx,
		`SELECT policy_reason, result FROM action_log WHERE action_id = ?`, a.ActionID,
	).Scan(&rawReason, &rawResult); err != nil {
		t.Fatalf("raw query: %v", err)
	}
	if rawReason.Valid && rawReason.String != "" {
		t.Errorf("plaintext policy_reason should be NULL, got %q", rawReason.String)
	}
	if rawResult.Valid && rawResult.String != "" {
		t.Errorf("plaintext result should be NULL, got %q", rawResult.String)
	}
}

func TestActionEncryption_WrongKeyFails(t *testing.T) {
	db := newEncDBActions(t)
	sA := actions.NewStore(db)
	sA.SetEncryption(newMKMActions(t, "key-A"))
	ctx := context.Background()
	a, err := sA.Record(ctx, actions.Action{
		AgentID:        "agt_test",
		Capability:     "nexus_write",
		PolicyDecision: actions.DecisionAllow,
		PolicyReason:   "secret reason",
		Result:         "ok",
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	sB := actions.NewStore(db)
	sB.SetEncryption(newMKMActions(t, "key-B"))
	_, err = sB.Get(ctx, a.ActionID)
	if err == nil {
		t.Fatal("expected decrypt error with wrong key, got nil")
	}
}

func TestActionEncryption_BackwardCompat(t *testing.T) {
	db := newEncDBActions(t)
	sPlain := actions.NewStore(db)
	ctx := context.Background()
	a, err := sPlain.Record(ctx, actions.Action{
		AgentID:        "agt_test",
		Capability:     "nexus_read",
		PolicyDecision: actions.DecisionAllow,
		PolicyReason:   "legacy reason",
		Result:         "ok",
	})
	if err != nil {
		t.Fatalf("Record (plaintext): %v", err)
	}
	sEnc := actions.NewStore(db)
	sEnc.SetEncryption(newMKMActions(t, "pw"))
	got, err := sEnc.Get(ctx, a.ActionID)
	if err != nil {
		t.Fatalf("Get (encrypted store, old row): %v", err)
	}
	if got.PolicyReason != "legacy reason" {
		t.Errorf("PolicyReason: got %q", got.PolicyReason)
	}
	if got.Result != "ok" {
		t.Errorf("Result: got %q", got.Result)
	}
}

func TestActionEncryption_EmptyFieldsNoBlob(t *testing.T) {
	db := newEncDBActions(t)
	s := actions.NewStore(db)
	s.SetEncryption(newMKMActions(t, "pw"))
	ctx := context.Background()
	a, err := s.Record(ctx, actions.Action{
		AgentID:        "agt_test",
		Capability:     "nexus_write",
		PolicyDecision: actions.DecisionDeny,
		// PolicyReason and Result intentionally empty.
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	var blob1, blob2 []byte
	if err := db.QueryRowContext(ctx,
		`SELECT policy_reason_encrypted, result_encrypted FROM action_log WHERE action_id = ?`, a.ActionID,
	).Scan(&blob1, &blob2); err != nil {
		t.Fatalf("raw query: %v", err)
	}
	if blob1 != nil {
		t.Error("policy_reason_encrypted should be NULL for empty reason")
	}
	if blob2 != nil {
		t.Error("result_encrypted should be NULL for empty result")
	}
}

func TestActionEncryption_DisabledMKMNoOp(t *testing.T) {
	saltPath := filepath.Join(t.TempDir(), "crypto.salt")
	mkm, err := crypto.NewMasterKeyManager("", saltPath)
	if err != nil {
		t.Fatalf("NewMasterKeyManager: %v", err)
	}
	if mkm.IsEnabled() {
		t.Skip("NEXUS_PASSWORD env var set; skipping disabled-MKM test")
	}
	db := newEncDBActions(t)
	s := actions.NewStore(db)
	s.SetEncryption(mkm)
	ctx := context.Background()
	a, err := s.Record(ctx, actions.Action{
		AgentID:        "agt_test",
		Capability:     "nexus_read",
		PolicyDecision: actions.DecisionAllow,
		PolicyReason:   "plaintext ok",
		Result:         "read done",
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	got, err := s.Get(ctx, a.ActionID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.PolicyReason != "plaintext ok" {
		t.Errorf("PolicyReason: got %q", got.PolicyReason)
	}
}

func TestActionEncryption_QueryDecrypts(t *testing.T) {
	db := newEncDBActions(t)
	s := actions.NewStore(db)
	s.SetEncryption(newMKMActions(t, "querypw"))
	ctx := context.Background()
	a, err := s.Record(ctx, actions.Action{
		AgentID:        "agt_query",
		Capability:     "nexus_write",
		PolicyDecision: actions.DecisionAllow,
		PolicyReason:   "query test reason",
		Result:         "query test result",
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	_ = a
	list, err := s.Query(ctx, actions.QueryFilter{AgentID: "agt_query"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("want 1 action, got %d", len(list))
	}
	if list[0].PolicyReason != "query test reason" {
		t.Errorf("PolicyReason: got %q", list[0].PolicyReason)
	}
	if list[0].Result != "query test result" {
		t.Errorf("Result: got %q", list[0].Result)
	}
}
