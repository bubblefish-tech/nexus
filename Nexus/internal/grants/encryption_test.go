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
	"path/filepath"
	"testing"

	"github.com/bubblefish-tech/nexus/internal/a2a/registry"
	"github.com/bubblefish-tech/nexus/internal/crypto"
	"github.com/bubblefish-tech/nexus/internal/grants"
)

func newMKM(t *testing.T, password string) *crypto.MasterKeyManager {
	t.Helper()
	saltPath := filepath.Join(t.TempDir(), "crypto.salt")
	mkm, err := crypto.NewMasterKeyManager(password, saltPath)
	if err != nil {
		t.Fatalf("NewMasterKeyManager: %v", err)
	}
	return mkm
}

// newEncDB opens an in-memory SQLite DB with the full schema (including CU.0.4
// encrypted columns from SchemaSQL) and returns the *sql.DB.
func newEncDB(t *testing.T) *sql.DB {
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

func TestGrantEncryption_RoundTrip(t *testing.T) {
	db := newEncDB(t)
	s := grants.NewStore(db)
	s.SetEncryption(newMKM(t, "testpw"))
	ctx := context.Background()
	scope := json.RawMessage(`{"env":"prod"}`)
	g, err := s.Create(ctx, grants.Grant{
		AgentID:    "agt_test",
		Capability: "nexus_write",
		Scope:      scope,
		GrantedBy:  "admin",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := s.Get(ctx, g.GrantID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got.Scope) != string(scope) {
		t.Errorf("Scope: got %s, want %s", got.Scope, scope)
	}
}

func TestGrantEncryption_PlaintextColumnEmpty(t *testing.T) {
	db := newEncDB(t)
	s := grants.NewStore(db)
	s.SetEncryption(newMKM(t, "pw"))
	ctx := context.Background()
	g, err := s.Create(ctx, grants.Grant{
		AgentID:    "agt_test",
		Capability: "nexus_write",
		Scope:      json.RawMessage(`{"k":"v"}`),
		GrantedBy:  "admin",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Plaintext scope_json should be the empty placeholder '{}', not the real data.
	var rawScope string
	if err := db.QueryRowContext(ctx, `SELECT scope_json FROM grants WHERE grant_id = ?`, g.GrantID).Scan(&rawScope); err != nil {
		t.Fatalf("raw query: %v", err)
	}
	if rawScope != "{}" {
		t.Errorf("plaintext scope_json should be placeholder '{}', got %q", rawScope)
	}
}

func TestGrantEncryption_WrongKeyFails(t *testing.T) {
	db := newEncDB(t)
	sA := grants.NewStore(db)
	sA.SetEncryption(newMKM(t, "key-A"))
	ctx := context.Background()
	g, err := sA.Create(ctx, grants.Grant{
		AgentID:    "agt_test",
		Capability: "nexus_read",
		Scope:      json.RawMessage(`{"secret":"data"}`),
		GrantedBy:  "admin",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Reader with different password must fail to decrypt.
	sB := grants.NewStore(db)
	sB.SetEncryption(newMKM(t, "key-B"))
	_, err = sB.Get(ctx, g.GrantID)
	if err == nil {
		t.Fatal("expected decrypt error with wrong key, got nil")
	}
}

func TestGrantEncryption_BackwardCompat(t *testing.T) {
	db := newEncDB(t)
	sPlain := grants.NewStore(db) // no encryption
	ctx := context.Background()
	g, err := sPlain.Create(ctx, grants.Grant{
		AgentID:    "agt_test",
		Capability: "nexus_write",
		Scope:      json.RawMessage(`{"legacy":"true"}`),
		GrantedBy:  "admin",
	})
	if err != nil {
		t.Fatalf("Create (plaintext): %v", err)
	}
	// Encrypted store should still read old plaintext rows.
	sEnc := grants.NewStore(db)
	sEnc.SetEncryption(newMKM(t, "pw"))
	got, err := sEnc.Get(ctx, g.GrantID)
	if err != nil {
		t.Fatalf("Get (encrypted store, old row): %v", err)
	}
	if string(got.Scope) != `{"legacy":"true"}` {
		t.Errorf("Scope: got %s, want {\"legacy\":\"true\"}", got.Scope)
	}
}

func TestGrantEncryption_RevokeReasonRoundTrip(t *testing.T) {
	db := newEncDB(t)
	s := grants.NewStore(db)
	s.SetEncryption(newMKM(t, "revpw"))
	ctx := context.Background()
	g, err := s.Create(ctx, grants.Grant{
		AgentID:    "agt_test",
		Capability: "nexus_delete",
		GrantedBy:  "admin",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := s.Revoke(ctx, g.GrantID, "security policy violation"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	got, err := s.Get(ctx, g.GrantID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.RevokeReason != "security policy violation" {
		t.Errorf("RevokeReason: got %q, want %q", got.RevokeReason, "security policy violation")
	}
	if got.RevokedAt == nil {
		t.Error("RevokedAt should be set")
	}
}

func TestGrantEncryption_RevokeReasonPlaintextEmpty(t *testing.T) {
	db := newEncDB(t)
	s := grants.NewStore(db)
	s.SetEncryption(newMKM(t, "pw"))
	ctx := context.Background()
	g, err := s.Create(ctx, grants.Grant{
		AgentID:    "agt_test",
		Capability: "nexus_write",
		GrantedBy:  "admin",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := s.Revoke(ctx, g.GrantID, "reason text"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	var rawReason string
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(revoke_reason,'') FROM grants WHERE grant_id = ?`, g.GrantID).Scan(&rawReason); err != nil {
		t.Fatalf("raw query: %v", err)
	}
	if rawReason != "" {
		t.Errorf("plaintext revoke_reason should be empty, got %q", rawReason)
	}
}

func TestGrantEncryption_DisabledMKMNoOp(t *testing.T) {
	saltPath := filepath.Join(t.TempDir(), "crypto.salt")
	mkm, err := crypto.NewMasterKeyManager("", saltPath) // no password → disabled
	if err != nil {
		t.Fatalf("NewMasterKeyManager: %v", err)
	}
	if mkm.IsEnabled() {
		t.Skip("NEXUS_PASSWORD env var set; skipping disabled-MKM test")
	}
	db := newEncDB(t)
	s := grants.NewStore(db)
	s.SetEncryption(mkm)
	ctx := context.Background()
	scope := json.RawMessage(`{"x":1}`)
	g, err := s.Create(ctx, grants.Grant{
		AgentID:    "agt_test",
		Capability: "nexus_read",
		Scope:      scope,
		GrantedBy:  "admin",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := s.Get(ctx, g.GrantID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got.Scope) != string(scope) {
		t.Errorf("Scope: got %s, want %s", got.Scope, scope)
	}
}

func TestGrantEncryption_ListDecrypts(t *testing.T) {
	db := newEncDB(t)
	s := grants.NewStore(db)
	s.SetEncryption(newMKM(t, "listpw"))
	ctx := context.Background()
	scope := json.RawMessage(`{"tier":"gold"}`)
	if _, err := s.Create(ctx, grants.Grant{
		AgentID:    "agt_list",
		Capability: "nexus_write",
		Scope:      scope,
		GrantedBy:  "admin",
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	list, err := s.List(ctx, grants.ListFilter{AgentID: "agt_list"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("want 1 grant, got %d", len(list))
	}
	if string(list[0].Scope) != string(scope) {
		t.Errorf("Scope: got %s, want %s", list[0].Scope, scope)
	}
}

func TestGrantEncryption_CheckGrantDecrypts(t *testing.T) {
	db := newEncDB(t)
	s := grants.NewStore(db)
	s.SetEncryption(newMKM(t, "chkpw"))
	ctx := context.Background()
	scope := json.RawMessage(`{"check":"ok"}`)
	if _, err := s.Create(ctx, grants.Grant{
		AgentID:    "agt_check",
		Capability: "nexus_check",
		Scope:      scope,
		GrantedBy:  "admin",
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	g, err := s.CheckGrant(ctx, "agt_check", "nexus_check")
	if err != nil {
		t.Fatalf("CheckGrant: %v", err)
	}
	if g == nil {
		t.Fatal("CheckGrant returned nil")
	}
	if string(g.Scope) != string(scope) {
		t.Errorf("Scope: got %s, want %s", g.Scope, scope)
	}
}

func TestGrantEncryption_DifferentRowsDifferentCiphertext(t *testing.T) {
	db := newEncDB(t)
	s := grants.NewStore(db)
	s.SetEncryption(newMKM(t, "diffpw"))
	ctx := context.Background()
	scope := json.RawMessage(`{"identical":"scope"}`)
	g1, err := s.Create(ctx, grants.Grant{AgentID: "agt_test", Capability: "cap1", Scope: scope, GrantedBy: "admin"})
	if err != nil {
		t.Fatalf("Create g1: %v", err)
	}
	g2, err := s.Create(ctx, grants.Grant{AgentID: "agt_test", Capability: "cap2", Scope: scope, GrantedBy: "admin"})
	if err != nil {
		t.Fatalf("Create g2: %v", err)
	}
	var blob1, blob2 []byte
	if err := db.QueryRowContext(ctx, `SELECT scope_json_encrypted FROM grants WHERE grant_id = ?`, g1.GrantID).Scan(&blob1); err != nil {
		t.Fatalf("raw query g1: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT scope_json_encrypted FROM grants WHERE grant_id = ?`, g2.GrantID).Scan(&blob2); err != nil {
		t.Fatalf("raw query g2: %v", err)
	}
	if string(blob1) == string(blob2) {
		t.Error("different rows with identical scope should produce different ciphertext (per-row key)")
	}
}
