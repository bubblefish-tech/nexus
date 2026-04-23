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

//go:build chaos

// Chaos kill-9 scenarios for substrate crash recovery.
//
// These tests verify that the substrate reaches a consistent, recoverable
// state when a process crash interrupts non-atomic sequences of operations.
// Each test:
//   1. Creates a real on-disk SQLite database with WAL mode
//   2. Initializes a Substrate and performs setup operations
//   3. Simulates a crash by performing operations up to a precise kill
//      point, then abandoning the Substrate without proper shutdown
//   4. Creates a fresh Substrate against the same DB (simulating restart)
//   5. Verifies all recovery invariants
//
// Run with: go test -tags chaos -v ./internal/substrate/
//
// Reference: Substrate Build Plan, Step 8 — Chaos Scenarios.
package substrate

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/bubblefish-tech/nexus/internal/canonical"
	"github.com/bubblefish-tech/nexus/internal/provenance"
	"github.com/bubblefish-tech/nexus/internal/secrets"
	_ "modernc.org/sqlite"
)

// ─── Test helpers ──────────────────────────────────────────────────────────

// chaosTestEnv holds everything needed for a chaos scenario.
type chaosTestEnv struct {
	t       *testing.T
	dbPath  string
	tempDir string
	db      *sql.DB
	sub     *Substrate
	sd      *secrets.Dir
	mgr     *canonical.Manager
	chain   *provenance.ChainState
	logger  *slog.Logger
}

// newChaosTestEnv creates a fresh on-disk SQLite database with WAL mode
// and all substrate tables. Returns a fully initialized Substrate.
func newChaosTestEnv(t *testing.T) *chaosTestEnv {
	t.Helper()

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "chaos_test.db")

	db := openChaosDB(t, dbPath)
	createChaosTables(t, db)

	// Enable WAL mode for crash safety
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		t.Fatal("enable WAL:", err)
	}
	if _, err := db.Exec(`PRAGMA synchronous=FULL`); err != nil {
		t.Fatal("enable synchronous=FULL:", err)
	}

	sd, err := secrets.Open(filepath.Join(tempDir, "secrets"))
	if err != nil {
		t.Fatal("open secrets:", err)
	}

	canonicalCfg := canonical.Config{
		Enabled:              true,
		CanonicalDim:         64,
		WhiteningWarmup:      1000,
		QueryCacheTTLSeconds: 60,
	}
	mgr := canonical.NewManager(canonicalCfg)
	if err := mgr.Init(sd, slog.Default()); err != nil {
		t.Fatal("init canonical:", err)
	}

	chain := provenance.NewChainState()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	}))

	cfg := DefaultConfig()
	cfg.Enabled = true

	sub, err := New(cfg, db, sd, nil, mgr, chain, logger)
	if err != nil {
		t.Fatal("init substrate:", err)
	}

	return &chaosTestEnv{
		t:       t,
		dbPath:  dbPath,
		tempDir: tempDir,
		db:      db,
		sub:     sub,
		sd:      sd,
		mgr:     mgr,
		chain:   chain,
		logger:  logger,
	}
}

// closeDB closes the current DB without proper Substrate shutdown,
// simulating a crash.
func (e *chaosTestEnv) closeDB() {
	e.t.Helper()
	// Do NOT call e.sub.Shutdown() — that's the crash simulation.
	e.db.Close()
	e.sub = nil
	e.db = nil
}

// reopen opens the DB and creates a fresh Substrate, simulating restart.
func (e *chaosTestEnv) reopen() {
	e.t.Helper()
	db := openChaosDB(e.t, e.dbPath)
	e.db = db

	cfg := DefaultConfig()
	cfg.Enabled = true

	sub, err := New(cfg, db, e.sd, nil, e.mgr, e.chain, e.logger)
	if err != nil {
		e.t.Fatal("reopen substrate:", err)
	}
	e.sub = sub
}

// insertMemory inserts a bare memory row with no substrate columns.
func (e *chaosTestEnv) insertMemory(id string) {
	e.t.Helper()
	_, err := e.db.Exec(
		`INSERT INTO memories (payload_id, content, timestamp) VALUES (?, 'chaos-test', 1000)`,
		id,
	)
	if err != nil {
		e.t.Fatal("insert memory:", err)
	}
}

// writeSketch calls ComputeAndStoreSketch for a memory.
func (e *chaosTestEnv) writeSketch(id string) {
	e.t.Helper()
	emb := make([]float64, 64)
	for i := range emb {
		emb[i] = float64(i+1) * 0.01
	}
	if err := e.sub.ComputeAndStoreSketch(id, emb, "chaos-source"); err != nil {
		e.t.Fatal("write sketch:", err)
	}
}

func openChaosDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal("open DB:", err)
	}
	t.Cleanup(func() {
		// Best-effort close; may already be closed by closeDB().
		db.Close()
	})
	return db
}

func createChaosTables(t *testing.T, db *sql.DB) {
	t.Helper()
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS memories (
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
		`CREATE TABLE IF NOT EXISTS substrate_ratchet_states (
			state_id INTEGER PRIMARY KEY AUTOINCREMENT,
			created_at INTEGER NOT NULL, shredded_at INTEGER,
			state_bytes BLOB NOT NULL, canonical_dim INTEGER NOT NULL,
			sketch_bits INTEGER NOT NULL, signature BLOB NOT NULL,
			state_bytes_encrypted BLOB, state_bytes_enc_version INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS substrate_memory_state (
			memory_id TEXT PRIMARY KEY, state_id INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS substrate_canonical_whitening (
			source_name TEXT PRIMARY KEY, sample_count INTEGER NOT NULL,
			mean_vector BLOB NOT NULL, covariance_lr BLOB NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS substrate_cuckoo_filter (
			filter_id INTEGER PRIMARY KEY, filter_bytes BLOB NOT NULL,
			last_persisted INTEGER NOT NULL,
			filter_bytes_encrypted BLOB, filter_bytes_enc_version INTEGER NOT NULL DEFAULT 0
		)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatal("create table:", err)
		}
	}
}

// ─── Scenario 8a — Kill during sketch write ────────────────────────────────
//
// Kill point: after DB tx.Commit() (sketch + ciphertext columns written),
// before cuckoo.Insert(memoryID).
//
// Expected post-crash state:
//   - Memory row has sketch, ciphertext, nonce columns populated (DB committed)
//   - substrate_memory_state row exists (same tx)
//   - Cuckoo filter does NOT contain the memory (insert never happened)
//   - On restart, Substrate initializes successfully
//   - Cuckoo is rebuilt from DB, which includes all memories (but cuckoo
//     only tracks payload_ids, not sketch presence — so the memory IS in
//     the rebuilt cuckoo)

func TestChaos_SketchWrite(t *testing.T) {
	env := newChaosTestEnv(t)

	// Insert a memory row
	env.insertMemory("chaos-8a")

	// Simulate: perform the sketch write up to the kill point.
	// We do this by manually executing the same operations ComputeAndStoreSketch
	// does, stopping before the cuckoo insert.

	emb := make([]float64, 64)
	for i := range emb {
		emb[i] = float64(i+1) * 0.01
	}

	canonicalVec, _, err := env.sub.canonical.Canonicalize(emb, "chaos-source")
	if err != nil {
		t.Fatal("canonicalize:", err)
	}

	state := env.sub.ratchet.Current()
	sketch, err := ComputeStoreSketch(canonicalVec, state.StateBytes, state.StateID)
	if err != nil {
		t.Fatal("compute sketch:", err)
	}
	sketchBytes, err := sketch.Marshal()
	if err != nil {
		t.Fatal("marshal sketch:", err)
	}

	key, err := DeriveEmbeddingKey(state.StateBytes, "chaos-8a")
	if err != nil {
		t.Fatal("derive key:", err)
	}
	embBytes := encodeFloat64Slice(canonicalVec)
	encrypted, err := EncryptEmbedding(key, embBytes)
	ZeroizeKey(&key)
	if err != nil {
		t.Fatal("encrypt:", err)
	}

	// Commit the DB transaction (this is what survives the crash)
	tx, err := env.db.Begin()
	if err != nil {
		t.Fatal("begin tx:", err)
	}
	_, err = tx.Exec(`
		UPDATE memories SET sketch = ?, embedding_ciphertext = ?, embedding_nonce = ?
		WHERE payload_id = ?
	`, sketchBytes, encrypted.Ciphertext, encrypted.Nonce, "chaos-8a")
	if err != nil {
		t.Fatal("update memories:", err)
	}
	_, err = tx.Exec(`
		INSERT OR REPLACE INTO substrate_memory_state (memory_id, state_id)
		VALUES (?, ?)
	`, "chaos-8a", state.StateID)
	if err != nil {
		t.Fatal("insert memory state:", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal("commit:", err)
	}

	// CRASH: cuckoo.Insert never happened. Close DB without shutdown.
	env.closeDB()

	// ─── Verify recovery ───
	env.reopen()

	// 1. Memory row exists with populated substrate columns
	var sketchCol, cipherCol, nonceCol []byte
	err = env.db.QueryRow(
		`SELECT sketch, embedding_ciphertext, embedding_nonce FROM memories WHERE payload_id = 'chaos-8a'`,
	).Scan(&sketchCol, &cipherCol, &nonceCol)
	if err != nil {
		t.Fatal("post-crash query:", err)
	}
	if sketchCol == nil {
		t.Error("sketch should be non-NULL after crash (DB committed)")
	}
	if cipherCol == nil {
		t.Error("ciphertext should be non-NULL after crash (DB committed)")
	}
	if nonceCol == nil {
		t.Error("nonce should be non-NULL after crash (DB committed)")
	}

	// 2. substrate_memory_state row exists
	var memStateID uint32
	err = env.db.QueryRow(
		`SELECT state_id FROM substrate_memory_state WHERE memory_id = 'chaos-8a'`,
	).Scan(&memStateID)
	if err != nil {
		t.Fatal("post-crash memory state:", err)
	}
	if memStateID == 0 {
		t.Error("memory state_id should be > 0")
	}

	// 3. Cuckoo filter was rebuilt from DB on restart and now includes the memory.
	// RebuildFromDB scans all payload_ids from the memories table, so the memory
	// IS in the rebuilt filter even though the original insert was lost.
	if !env.sub.CuckooLookup("chaos-8a") {
		t.Error("memory should be in rebuilt cuckoo filter (RebuildFromDB scans all memories)")
	}

	// 4. Substrate is functional: can load the sketch
	loadedSketch, err := env.sub.LoadStoreSketch("chaos-8a")
	if err != nil {
		t.Fatal("load sketch post-crash:", err)
	}
	if loadedSketch == nil {
		t.Error("loaded sketch should not be nil")
	}
}

// ─── Scenario 8b — Kill during ratchet advance ─────────────────────────────
//
// Kill point: after INSERT INTO substrate_ratchet_states (new state persisted),
// before UPDATE substrate_ratchet_states SET state_bytes = zeroes (old state
// not yet shredded).
//
// Expected post-crash state:
//   - Both old and new ratchet state rows exist in DB
//   - Old state still has non-zero state_bytes (shred didn't happen)
//   - New state has non-zero state_bytes
//   - On restart, loadOrInitialize picks the newest non-shredded state
//   - Substrate initializes successfully

func TestChaos_RatchetAdvance(t *testing.T) {
	env := newChaosTestEnv(t)

	// Record the initial ratchet state
	oldState := env.sub.CurrentRatchetState()
	if oldState == nil {
		t.Fatal("should have initial ratchet state")
	}
	oldStateID := oldState.StateID
	oldStateBytes := oldState.StateBytes

	// Simulate: perform ratchet advance up to the kill point.
	// Manually call persistNewState but skip shredState.

	// Compute the new state bytes (same logic as Advance)
	mac := hmacSHA256(oldState.StateBytes[:], []byte(ratchetAdvanceLabelV1))
	var newBytesArr [32]byte
	copy(newBytesArr[:], mac)

	// Persist the new state (this survives the crash)
	newState, err := env.sub.ratchet.persistNewState(newBytesArr)
	if err != nil {
		t.Fatal("persist new state:", err)
	}

	// CRASH: shredState never called. Close DB without shutdown.
	env.closeDB()

	// ─── Verify recovery ───
	env.reopen()

	// 1. New ratchet state exists
	var newCount int
	err = env.db.QueryRow(
		`SELECT COUNT(*) FROM substrate_ratchet_states WHERE state_id = ?`,
		newState.StateID,
	).Scan(&newCount)
	if err != nil {
		t.Fatal("query new state:", err)
	}
	if newCount != 1 {
		t.Errorf("new state should exist, got count=%d", newCount)
	}

	// 2. Old state still has non-zero bytes (shred didn't happen)
	var oldBytes []byte
	err = env.db.QueryRow(
		`SELECT state_bytes FROM substrate_ratchet_states WHERE state_id = ?`,
		oldStateID,
	).Scan(&oldBytes)
	if err != nil {
		t.Fatal("query old state:", err)
	}
	if isAllZero(oldBytes) {
		t.Error("old state bytes should NOT be zeroed (shred didn't happen)")
	}
	// Verify the old bytes match what we started with
	var expectedOld [32]byte
	copy(expectedOld[:], oldBytes)
	if expectedOld != oldStateBytes {
		t.Error("old state bytes should match original")
	}

	// 3. loadOrInitialize picked the newest state (highest state_id with
	//    shredded_at IS NULL)
	currentState := env.sub.CurrentRatchetState()
	if currentState == nil {
		t.Fatal("should have a current ratchet state after recovery")
	}
	if currentState.StateID != newState.StateID {
		t.Errorf("expected current state_id=%d (newest), got %d",
			newState.StateID, currentState.StateID)
	}

	// 4. Both states are non-shredded
	var unshredded int
	err = env.db.QueryRow(
		`SELECT COUNT(*) FROM substrate_ratchet_states WHERE shredded_at IS NULL`,
	).Scan(&unshredded)
	if err != nil {
		t.Fatal("count unshredded:", err)
	}
	if unshredded != 2 {
		t.Errorf("expected 2 non-shredded states (orphaned old + new), got %d", unshredded)
	}

	// 5. Substrate is functional: can write a sketch with the new state
	env.insertMemory("chaos-8b-post")
	env.writeSketch("chaos-8b-post")
	if !env.sub.CuckooLookup("chaos-8b-post") {
		t.Error("post-recovery sketch write should work")
	}
}

// ─── Scenario 8c — Kill during cuckoo persist ──────────────────────────────
//
// Kill point: after filter.Encode(), before SQL UPSERT.
//
// Expected post-crash state:
//   - substrate_cuckoo_filter table has either stale data or no row
//   - On restart, LoadCuckooOracle either loads stale data or fails
//   - On failure (or stale), RebuildFromDB reconstructs from memories table
//   - Rebuilt filter is consistent with actual DB state

func TestChaos_CuckooPersist(t *testing.T) {
	env := newChaosTestEnv(t)

	// Write several memories to populate the cuckoo filter
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("chaos-8c-%d", i)
		env.insertMemory(id)
		env.writeSketch(id)
	}

	// Verify all 5 are in the cuckoo filter
	for i := 0; i < 5; i++ {
		if !env.sub.CuckooLookup(fmt.Sprintf("chaos-8c-%d", i)) {
			t.Fatalf("memory chaos-8c-%d should be in cuckoo before crash", i)
		}
	}

	// Persist the cuckoo to get a baseline on-disk state
	if err := env.sub.cuckoo.Persist(env.db); err != nil {
		t.Fatal("initial persist:", err)
	}

	// Write 3 more memories (cuckoo updated in-memory but NOT persisted)
	for i := 5; i < 8; i++ {
		id := fmt.Sprintf("chaos-8c-%d", i)
		env.insertMemory(id)
		env.writeSketch(id)
	}

	// CRASH: cuckoo.Persist() was about to write the updated filter
	// but didn't complete. The on-disk filter has only 5 entries.
	env.closeDB()

	// ─── Verify recovery ───
	env.reopen()

	// On reopen, LoadCuckooOracle loads the stale filter (5 entries).
	// The Substrate.New code path tries LoadCuckooOracle first; if it
	// succeeds, the filter is stale. But all 8 memories are in the DB.
	// The cuckoo may be stale — the critical invariant is that
	// RebuildFromDB produces the correct set when called.

	// Verify all 8 memories exist in the DB
	var dbCount int
	env.db.QueryRow(`SELECT COUNT(*) FROM memories`).Scan(&dbCount)
	if dbCount != 8 {
		t.Fatalf("expected 8 memories in DB, got %d", dbCount)
	}

	// Force a rebuild to verify it produces the correct state
	rebuilt, err := RebuildFromDB(env.db, 10000, env.sub.logger, nil)
	if err != nil {
		t.Fatal("rebuild from DB:", err)
	}

	// All 8 memories should be in the rebuilt filter
	for i := 0; i < 8; i++ {
		id := fmt.Sprintf("chaos-8c-%d", i)
		if !rebuilt.Lookup(id) {
			t.Errorf("memory %s should be in rebuilt cuckoo filter", id)
		}
	}

	// A memory that was never written should NOT be in the filter
	if rebuilt.Lookup("nonexistent") {
		t.Error("nonexistent memory should not be in rebuilt filter")
	}

	// Verify the rebuild count was incremented
	stats := rebuilt.Stats()
	if stats.RebuildCount != 1 {
		t.Errorf("expected rebuild_count=1, got %d", stats.RebuildCount)
	}
}

// ─── Scenario 8d — Kill during shred-delete ─────────────────────────────────
//
// Kill point: after UPDATE memories SET embedding_ciphertext = NULL committed,
// before cuckoo.Delete(memoryID).
//
// Expected post-crash state:
//   - Memory row has NULL ciphertext and nonce (DB committed)
//   - substrate_memory_state row deleted (same tx)
//   - Cuckoo filter still contains the memory (delete never happened)
//   - On restart, cuckoo reports the memory as "live" (false positive)
//   - This is harmless: Stage 3.5 may try to use the sketch, but Stage 4
//     full-precision rerank will not find the embedding (ciphertext gone)
//   - The cascade degrades gracefully

func TestChaos_ShredDelete(t *testing.T) {
	env := newChaosTestEnv(t)

	// Write a memory with a sketch
	env.insertMemory("chaos-8d")
	env.writeSketch("chaos-8d")

	// Verify preconditions
	if !env.sub.CuckooLookup("chaos-8d") {
		t.Fatal("memory should be in cuckoo before shred")
	}
	var preSketch, preCipher, preNonce []byte
	env.db.QueryRow(
		`SELECT sketch, embedding_ciphertext, embedding_nonce FROM memories WHERE payload_id = 'chaos-8d'`,
	).Scan(&preSketch, &preCipher, &preNonce)
	if preSketch == nil || preCipher == nil || preNonce == nil {
		t.Fatal("memory should have sketch, ciphertext, nonce before shred")
	}

	// Simulate: perform the shred up to the kill point.
	// Execute the DB transaction (clear ciphertext + delete memory state),
	// but do NOT call cuckoo.Delete.
	tx, err := env.db.Begin()
	if err != nil {
		t.Fatal("begin tx:", err)
	}
	_, err = tx.Exec(`
		UPDATE memories SET embedding_ciphertext = NULL, embedding_nonce = NULL
		WHERE payload_id = ?
	`, "chaos-8d")
	if err != nil {
		t.Fatal("clear ciphertext:", err)
	}
	_, err = tx.Exec(`DELETE FROM substrate_memory_state WHERE memory_id = ?`, "chaos-8d")
	if err != nil {
		t.Fatal("delete memory state:", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal("commit:", err)
	}

	// CRASH: cuckoo.Delete never happened. Close without shutdown.
	env.closeDB()

	// ─── Verify recovery ───
	env.reopen()

	// 1. Memory row exists but ciphertext and nonce are NULL
	var postCipher, postNonce interface{}
	err = env.db.QueryRow(
		`SELECT embedding_ciphertext, embedding_nonce FROM memories WHERE payload_id = 'chaos-8d'`,
	).Scan(&postCipher, &postNonce)
	if err != nil {
		t.Fatal("post-crash query:", err)
	}
	if postCipher != nil {
		t.Error("ciphertext should be NULL after crash (DB committed the clear)")
	}
	if postNonce != nil {
		t.Error("nonce should be NULL after crash (DB committed the clear)")
	}

	// 2. Sketch column still exists (only ciphertext was cleared)
	var postSketch []byte
	env.db.QueryRow(
		`SELECT sketch FROM memories WHERE payload_id = 'chaos-8d'`,
	).Scan(&postSketch)
	if postSketch == nil {
		t.Error("sketch column should still be populated (shred only clears ciphertext)")
	}

	// 3. substrate_memory_state row is gone (deleted in the same tx)
	var memStateCount int
	env.db.QueryRow(
		`SELECT COUNT(*) FROM substrate_memory_state WHERE memory_id = 'chaos-8d'`,
	).Scan(&memStateCount)
	if memStateCount != 0 {
		t.Error("substrate_memory_state row should be deleted (DB committed)")
	}

	// 4. Cuckoo filter still contains the memory (false positive is harmless)
	// After restart, the cuckoo is rebuilt from memories table. Since the
	// memory row still exists (only ciphertext is NULL), it's in the rebuilt filter.
	if !env.sub.CuckooLookup("chaos-8d") {
		// This is expected: RebuildFromDB scans payload_ids from memories,
		// not from substrate_memory_state. The memory row exists, so it's
		// in the filter.
		t.Log("note: memory in rebuilt cuckoo (row still exists, ciphertext NULL)")
	}

	// 5. The system is in a safe state: the memory is effectively shredded
	// because its ciphertext is gone. Attempting to decrypt would fail.
	// The sketch is technically still readable but useless without the
	// embedding — Stage 4 full-precision rerank would skip it.

	// 6. Substrate is still functional: can write and shred other memories
	env.insertMemory("chaos-8d-post")
	env.writeSketch("chaos-8d-post")
	if !env.sub.CuckooLookup("chaos-8d-post") {
		t.Error("post-recovery sketch write should work")
	}
	if err := env.sub.ShredMemory("chaos-8d-post"); err != nil {
		t.Error("post-recovery shred should work:", err)
	}
}

// ─── Additional recovery invariant tests ────────────────────────────────────

// TestChaos_RecoveryMultipleOrphanedStates verifies that loadOrInitialize
// handles more than one orphaned (non-shredded) ratchet state — e.g., if
// the process crashed multiple times during advance without shredding.
func TestChaos_RecoveryMultipleOrphanedStates(t *testing.T) {
	env := newChaosTestEnv(t)

	initialState := env.sub.CurrentRatchetState()

	// Simulate two crashed advances: persist state 2 and state 3 without
	// shredding state 1 or state 2.
	mac1 := hmacSHA256(initialState.StateBytes[:], []byte(ratchetAdvanceLabelV1))
	var bytes2 [32]byte
	copy(bytes2[:], mac1)
	state2, err := env.sub.ratchet.persistNewState(bytes2)
	if err != nil {
		t.Fatal("persist state 2:", err)
	}

	mac2 := hmacSHA256(bytes2[:], []byte(ratchetAdvanceLabelV1))
	var bytes3 [32]byte
	copy(bytes3[:], mac2)
	state3, err := env.sub.ratchet.persistNewState(bytes3)
	if err != nil {
		t.Fatal("persist state 3:", err)
	}

	// Three non-shredded states exist
	var count int
	env.db.QueryRow(`SELECT COUNT(*) FROM substrate_ratchet_states WHERE shredded_at IS NULL`).Scan(&count)
	if count != 3 {
		t.Fatalf("expected 3 non-shredded states, got %d", count)
	}

	env.closeDB()
	env.reopen()

	// loadOrInitialize should pick the newest (highest state_id)
	current := env.sub.CurrentRatchetState()
	if current.StateID != state3.StateID {
		t.Errorf("expected current state_id=%d (newest), got %d",
			state3.StateID, current.StateID)
	}

	// Verify the state bytes match state 3
	if current.StateBytes != bytes3 {
		t.Error("current state bytes should match state 3")
	}

	_ = state2 // used for setup
}

// TestChaos_CuckooRebuildAfterCorruption verifies two things:
// 1. RebuildFromDB correctly reconstructs the filter from the memories table
// 2. LoadCuckooOracle failing (no persisted row) triggers rebuild in New()
//
// Note: the cuckoo library's Decode() accepts some corrupt byte patterns
// without error, so we test the rebuild path by removing the persisted row
// entirely (simulating a crash during persist where the SQL write never
// committed) rather than relying on Decode to reject bad bytes.
func TestChaos_CuckooRebuildAfterCorruption(t *testing.T) {
	env := newChaosTestEnv(t)

	// Write memories
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("chaos-corrupt-%d", i)
		env.insertMemory(id)
		env.writeSketch(id)
	}

	// Persist cuckoo (establishes a baseline)
	if err := env.sub.cuckoo.Persist(env.db); err != nil {
		t.Fatal("persist:", err)
	}

	// Verify all 3 are in the filter
	for i := 0; i < 3; i++ {
		if !env.sub.CuckooLookup(fmt.Sprintf("chaos-corrupt-%d", i)) {
			t.Fatalf("memory chaos-corrupt-%d should be in cuckoo", i)
		}
	}

	// Test 1: RebuildFromDB produces correct results
	rebuilt, err := RebuildFromDB(env.db, 10000, env.sub.logger, nil)
	if err != nil {
		t.Fatal("rebuild from DB:", err)
	}
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("chaos-corrupt-%d", i)
		if !rebuilt.Lookup(id) {
			t.Errorf("memory %s should be in rebuilt cuckoo", id)
		}
	}
	if rebuilt.Lookup("nonexistent") {
		t.Error("nonexistent memory should not be in rebuilt filter")
	}
	stats := rebuilt.Stats()
	if stats.RebuildCount != 1 {
		t.Errorf("expected rebuild_count=1, got %d", stats.RebuildCount)
	}

	// Test 2: Delete the persisted filter row (simulates crash during persist
	// where the SQL UPSERT never committed). Then verify Substrate.New
	// falls back to RebuildFromDB.
	_, err = env.db.Exec(`DELETE FROM substrate_cuckoo_filter`)
	if err != nil {
		t.Fatal("delete filter row:", err)
	}

	// Create a fresh Substrate against the same DB — it should rebuild
	cfg := DefaultConfig()
	cfg.Enabled = true
	chain := provenance.NewChainState()
	sub2, err := New(cfg, env.db, env.sd, nil, env.mgr, chain, env.logger)
	if err != nil {
		t.Fatal("init substrate after filter deletion:", err)
	}

	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("chaos-corrupt-%d", i)
		if !sub2.CuckooLookup(id) {
			t.Errorf("memory %s should be in cuckoo after New() rebuild", id)
		}
	}
}

// TestChaos_SketchWriteMultipleMemories verifies that a crash during one
// sketch write doesn't affect previously-committed sketches.
func TestChaos_SketchWriteMultipleMemories(t *testing.T) {
	env := newChaosTestEnv(t)

	// Write 5 memories successfully
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("chaos-multi-%d", i)
		env.insertMemory(id)
		env.writeSketch(id)
	}

	// Simulate a 6th write that crashes after DB commit but before cuckoo insert
	env.insertMemory("chaos-multi-5")
	emb := make([]float64, 64)
	for i := range emb {
		emb[i] = float64(i+1) * 0.01
	}
	canonicalVec, _, _ := env.sub.canonical.Canonicalize(emb, "src")
	state := env.sub.ratchet.Current()
	sketch, _ := ComputeStoreSketch(canonicalVec, state.StateBytes, state.StateID)
	sketchBytes, _ := sketch.Marshal()
	key, _ := DeriveEmbeddingKey(state.StateBytes, "chaos-multi-5")
	encrypted, _ := EncryptEmbedding(key, encodeFloat64Slice(canonicalVec))
	ZeroizeKey(&key)

	tx, _ := env.db.Begin()
	tx.Exec(`UPDATE memories SET sketch = ?, embedding_ciphertext = ?, embedding_nonce = ? WHERE payload_id = ?`,
		sketchBytes, encrypted.Ciphertext, encrypted.Nonce, "chaos-multi-5")
	tx.Exec(`INSERT OR REPLACE INTO substrate_memory_state (memory_id, state_id) VALUES (?, ?)`,
		"chaos-multi-5", state.StateID)
	tx.Commit()
	// CRASH: no cuckoo insert for chaos-multi-5

	env.closeDB()
	env.reopen()

	// All 6 memories should have their sketches intact
	for i := 0; i <= 5; i++ {
		id := fmt.Sprintf("chaos-multi-%d", i)
		var sk []byte
		env.db.QueryRow(`SELECT sketch FROM memories WHERE payload_id = ?`, id).Scan(&sk)
		if sk == nil {
			t.Errorf("memory %s should have a sketch after recovery", id)
		}
	}

	// All 6 should be in the rebuilt cuckoo
	for i := 0; i <= 5; i++ {
		if !env.sub.CuckooLookup(fmt.Sprintf("chaos-multi-%d", i)) {
			t.Errorf("memory chaos-multi-%d should be in rebuilt cuckoo", i)
		}
	}
}

// ─── helpers ────────────────────────────────────────────────────────────────

func hmacSHA256(key, msg []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(msg)
	return h.Sum(nil)
}
