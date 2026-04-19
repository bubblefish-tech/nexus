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

package substrate

import (
	"database/sql"
	"log/slog"
	"testing"

	"github.com/bubblefish-tech/nexus/internal/canonical"
	"github.com/bubblefish-tech/nexus/internal/provenance"
	"github.com/bubblefish-tech/nexus/internal/secrets"
	_ "modernc.org/sqlite"
)

// newFullTestDB creates a DB with all substrate tables for coordinator tests.
func newFullTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	stmts := []string{
		`CREATE TABLE memories (
			payload_id TEXT PRIMARY KEY,
			idempotency_key TEXT, namespace TEXT, source TEXT, destination TEXT,
			subject TEXT, content TEXT, metadata TEXT, timestamp INTEGER,
			sensitivity_labels TEXT DEFAULT '', classification_tier TEXT DEFAULT 'public',
			tier INTEGER DEFAULT 1, lsh_bucket INTEGER, cluster_id TEXT DEFAULT '',
			cluster_role TEXT DEFAULT '', embedding BLOB,
			signature TEXT DEFAULT '', signing_key_id TEXT DEFAULT '',
			signature_alg TEXT DEFAULT '',
			sketch BLOB, embedding_ciphertext BLOB, embedding_nonce BLOB
		)`,
		`CREATE TABLE substrate_ratchet_states (
			state_id INTEGER PRIMARY KEY AUTOINCREMENT,
			created_at INTEGER NOT NULL, shredded_at INTEGER,
			state_bytes BLOB NOT NULL, canonical_dim INTEGER NOT NULL,
			sketch_bits INTEGER NOT NULL, signature BLOB NOT NULL,
			state_bytes_encrypted BLOB, state_bytes_enc_version INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE substrate_memory_state (
			memory_id TEXT PRIMARY KEY, state_id INTEGER NOT NULL
		)`,
		`CREATE TABLE substrate_canonical_whitening (
			source_name TEXT PRIMARY KEY, sample_count INTEGER NOT NULL,
			mean_vector BLOB NOT NULL, covariance_lr BLOB NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE TABLE substrate_cuckoo_filter (
			filter_id INTEGER PRIMARY KEY, filter_bytes BLOB NOT NULL,
			last_persisted INTEGER NOT NULL,
			filter_bytes_encrypted BLOB, filter_bytes_enc_version INTEGER NOT NULL DEFAULT 0
		)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func newTestSubstrate(t *testing.T) (*Substrate, *sql.DB) {
	t.Helper()
	db := newFullTestDB(t)
	sd, err := secrets.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	canonicalCfg := canonical.Config{
		Enabled:              true,
		CanonicalDim:         64,
		WhiteningWarmup:      1000,
		QueryCacheTTLSeconds: 60,
	}
	mgr := canonical.NewManager(canonicalCfg)
	if err := mgr.Init(sd, slog.Default()); err != nil {
		t.Fatal(err)
	}

	cfg := DefaultConfig()
	cfg.Enabled = true

	chain := provenance.NewChainState()

	sub, err := New(cfg, db, sd, nil, mgr, chain, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	return sub, db
}

func TestNewSubstrateDisabled(t *testing.T) {
	cfg := DefaultConfig()
	s, err := New(cfg, nil, nil, nil, nil, nil, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if s.Enabled() {
		t.Fatal("disabled config should produce disabled substrate")
	}
}

func TestNewSubstrateFailsWithoutCanonical(t *testing.T) {
	db := newFullTestDB(t)
	sd, _ := secrets.Open(t.TempDir())
	cfg := DefaultConfig()
	cfg.Enabled = true

	_, err := New(cfg, db, sd, nil, nil, nil, slog.Default())
	if err == nil {
		t.Fatal("should fail without canonical")
	}
}

func TestNewSubstrateInitializesAllComponents(t *testing.T) {
	sub, _ := newTestSubstrate(t)
	if !sub.Enabled() {
		t.Fatal("should be enabled")
	}
	if sub.ratchet == nil {
		t.Fatal("ratchet should be initialized")
	}
	if sub.cuckoo == nil {
		t.Fatal("cuckoo should be initialized")
	}
	if sub.canonical == nil {
		t.Fatal("canonical should be initialized")
	}
}

func TestComputeAndStoreSketchWritesAllColumns(t *testing.T) {
	sub, db := newTestSubstrate(t)

	// Insert a memory row first
	db.Exec(`INSERT INTO memories (payload_id, content, timestamp) VALUES ('mem-1', 'test', 1000)`)

	// Create a 64-dim embedding
	emb := make([]float64, 64)
	for i := range emb {
		emb[i] = float64(i+1) * 0.1
	}

	err := sub.ComputeAndStoreSketch("mem-1", emb, "test-source")
	if err != nil {
		t.Fatal(err)
	}

	// Verify columns populated
	var sketch, ciphertext, nonce []byte
	err = db.QueryRow(
		"SELECT sketch, embedding_ciphertext, embedding_nonce FROM memories WHERE payload_id = 'mem-1'",
	).Scan(&sketch, &ciphertext, &nonce)
	if err != nil {
		t.Fatal(err)
	}
	if sketch == nil || len(sketch) == 0 {
		t.Fatal("sketch should be non-NULL")
	}
	if ciphertext == nil || len(ciphertext) == 0 {
		t.Fatal("embedding_ciphertext should be non-NULL")
	}
	if nonce == nil || len(nonce) == 0 {
		t.Fatal("embedding_nonce should be non-NULL")
	}
}

func TestComputeAndStoreSketchInsertsCuckoo(t *testing.T) {
	sub, db := newTestSubstrate(t)
	db.Exec(`INSERT INTO memories (payload_id, content, timestamp) VALUES ('mem-cuckoo', 'test', 1000)`)

	emb := make([]float64, 64)
	for i := range emb {
		emb[i] = float64(i) * 0.01
	}
	sub.ComputeAndStoreSketch("mem-cuckoo", emb, "src")

	if !sub.CuckooLookup("mem-cuckoo") {
		t.Fatal("memory should be in cuckoo filter after sketch write")
	}
}

func TestComputeAndStoreSketchCreatesMemoryState(t *testing.T) {
	sub, db := newTestSubstrate(t)
	db.Exec(`INSERT INTO memories (payload_id, content, timestamp) VALUES ('mem-state', 'test', 1000)`)

	emb := make([]float64, 64)
	for i := range emb {
		emb[i] = float64(i) * 0.01
	}
	sub.ComputeAndStoreSketch("mem-state", emb, "src")

	var stateID uint32
	err := db.QueryRow(
		"SELECT state_id FROM substrate_memory_state WHERE memory_id = 'mem-state'",
	).Scan(&stateID)
	if err != nil {
		t.Fatal(err)
	}
	if stateID == 0 {
		t.Fatal("state_id should be non-zero")
	}
}

func TestLoadStoreSketchRoundTrip(t *testing.T) {
	sub, db := newTestSubstrate(t)
	db.Exec(`INSERT INTO memories (payload_id, content, timestamp) VALUES ('mem-load', 'test', 1000)`)

	emb := make([]float64, 64)
	for i := range emb {
		emb[i] = float64(i+1) * 0.01
	}
	sub.ComputeAndStoreSketch("mem-load", emb, "src")

	sketch, err := sub.LoadStoreSketch("mem-load")
	if err != nil {
		t.Fatal(err)
	}
	if sketch == nil {
		t.Fatal("loaded sketch should not be nil")
	}
	if sketch.CanonicalDim != 64 {
		t.Fatalf("expected dim=64, got %d", sketch.CanonicalDim)
	}
}

func TestShredMemoryClearsColumns(t *testing.T) {
	sub, db := newTestSubstrate(t)
	db.Exec(`INSERT INTO memories (payload_id, content, timestamp) VALUES ('mem-shred', 'test', 1000)`)

	emb := make([]float64, 64)
	for i := range emb {
		emb[i] = float64(i) * 0.01
	}
	sub.ComputeAndStoreSketch("mem-shred", emb, "src")

	err := sub.ShredMemory("mem-shred")
	if err != nil {
		t.Fatal(err)
	}

	// Verify ciphertext and nonce are cleared
	var ciphertext, nonce interface{}
	db.QueryRow(
		"SELECT embedding_ciphertext, embedding_nonce FROM memories WHERE payload_id = 'mem-shred'",
	).Scan(&ciphertext, &nonce)
	if ciphertext != nil {
		t.Fatal("embedding_ciphertext should be NULL after shred")
	}
	if nonce != nil {
		t.Fatal("embedding_nonce should be NULL after shred")
	}
}

func TestShredMemoryDeletesFromCuckoo(t *testing.T) {
	sub, db := newTestSubstrate(t)
	db.Exec(`INSERT INTO memories (payload_id, content, timestamp) VALUES ('mem-cuck-shred', 'test', 1000)`)

	emb := make([]float64, 64)
	for i := range emb {
		emb[i] = float64(i) * 0.01
	}
	sub.ComputeAndStoreSketch("mem-cuck-shred", emb, "src")

	if !sub.CuckooLookup("mem-cuck-shred") {
		t.Fatal("should be in cuckoo before shred")
	}

	sub.ShredMemory("mem-cuck-shred")

	if sub.CuckooLookup("mem-cuck-shred") {
		t.Fatal("should NOT be in cuckoo after shred")
	}
}

func TestRotateRatchetAdvancesState(t *testing.T) {
	sub, _ := newTestSubstrate(t)
	oldState := sub.CurrentRatchetState()

	newState, err := sub.RotateRatchet("test-rotate")
	if err != nil {
		t.Fatal(err)
	}
	if newState.StateID <= oldState.StateID {
		t.Fatal("new state ID should be greater")
	}
	if sub.CurrentRatchetState().StateID != newState.StateID {
		t.Fatal("current state should be the new state")
	}
}

func TestStatusReflectsState(t *testing.T) {
	sub, db := newTestSubstrate(t)
	status := sub.Status()

	if !status.Enabled {
		t.Fatal("status should show enabled")
	}
	if status.RatchetStateID == 0 {
		t.Fatal("ratchet state ID should be > 0")
	}

	// Write a memory and check sketch count
	db.Exec(`INSERT INTO memories (payload_id, content, timestamp) VALUES ('mem-status', 'test', 1000)`)
	emb := make([]float64, 64)
	for i := range emb {
		emb[i] = float64(i) * 0.01
	}
	sub.ComputeAndStoreSketch("mem-status", emb, "src")

	status = sub.Status()
	if status.SketchCount != 1 {
		t.Fatalf("expected sketch_count=1, got %d", status.SketchCount)
	}
	if status.CuckooCount != 1 {
		t.Fatalf("expected cuckoo_count=1, got %d", status.CuckooCount)
	}
}

func TestShutdownPersistsCuckoo(t *testing.T) {
	sub, db := newTestSubstrate(t)
	db.Exec(`INSERT INTO memories (payload_id, content, timestamp) VALUES ('mem-shut', 'test', 1000)`)

	emb := make([]float64, 64)
	for i := range emb {
		emb[i] = float64(i) * 0.01
	}
	sub.ComputeAndStoreSketch("mem-shut", emb, "src")

	err := sub.Shutdown()
	if err != nil {
		t.Fatal(err)
	}

	// Verify cuckoo was persisted
	var filterBytes []byte
	err = db.QueryRow("SELECT filter_bytes FROM substrate_cuckoo_filter WHERE filter_id = 1").Scan(&filterBytes)
	if err != nil {
		t.Fatal("cuckoo should be persisted after shutdown")
	}
	if len(filterBytes) == 0 {
		t.Fatal("persisted filter should be non-empty")
	}
}
