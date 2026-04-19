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
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/bubblefish-tech/nexus/internal/provenance"
	"github.com/bubblefish-tech/nexus/internal/secrets"
	_ "modernc.org/sqlite"
)

// ─── Audit Event Tests ──────────────────────────────────────────────────────

func TestEmitRatchetAdvanced(t *testing.T) {
	chain := provenance.NewChainState()
	log := NewSubstrateAuditLog(chain)

	entry, err := log.EmitRatchetAdvanced(1, 2, "test")
	if err != nil {
		t.Fatal(err)
	}
	if entry.EventType != EventRatchetAdvanced {
		t.Fatalf("expected %s, got %s", EventRatchetAdvanced, entry.EventType)
	}
	if entry.Hash == "" {
		t.Fatal("hash should be set when chain is present")
	}

	// Verify payload parses
	var payload RatchetAdvancedPayload
	if err := json.Unmarshal(entry.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.OldStateID != 1 || payload.NewStateID != 2 {
		t.Fatalf("payload mismatch: %+v", payload)
	}
}

func TestEmitSketchWritten(t *testing.T) {
	log := NewSubstrateAuditLog(provenance.NewChainState())
	entry, err := log.EmitSketchWritten("mem-1", 5, []byte{1, 2, 3}, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if entry.EventType != EventSketchWritten {
		t.Fatal("wrong event type")
	}
	var payload SketchWrittenPayload
	json.Unmarshal(entry.Payload, &payload)
	if payload.MemoryID != "mem-1" || payload.StateID != 5 {
		t.Fatalf("payload mismatch: %+v", payload)
	}
	if payload.SketchHash == "" {
		t.Fatal("sketch hash should be non-empty")
	}
}

func TestEmitMemoryShredded(t *testing.T) {
	log := NewSubstrateAuditLog(provenance.NewChainState())
	entry, err := log.EmitMemoryShredded("mem-del", 1, 2, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if entry.EventType != EventMemoryShredded {
		t.Fatal("wrong event type")
	}
	var payload MemoryShreddedPayload
	json.Unmarshal(entry.Payload, &payload)
	if !payload.CuckooRemoved || !payload.CanonicalRowDeleted {
		t.Fatalf("payload flags wrong: %+v", payload)
	}
}

func TestEmitCuckooRebuild(t *testing.T) {
	log := NewSubstrateAuditLog(provenance.NewChainState())
	entry, err := log.EmitCuckooRebuild("corruption", 500, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if entry.EventType != EventCuckooRebuild {
		t.Fatal("wrong event type")
	}
}

func TestAuditChainContinuity(t *testing.T) {
	chain := provenance.NewChainState()
	log := NewSubstrateAuditLog(chain)

	e1, _ := log.EmitRatchetAdvanced(1, 2, "first")
	e2, _ := log.EmitSketchWritten("mem-1", 2, []byte{1}, 1024)
	e3, _ := log.EmitMemoryShredded("mem-1", 2, 3, true, true)

	// Chain continuity: each entry's PrevHash should match the prior's Hash
	if e2.PrevHash != e1.Hash {
		t.Fatal("e2.PrevHash should equal e1.Hash")
	}
	if e3.PrevHash != e2.Hash {
		t.Fatal("e3.PrevHash should equal e2.Hash")
	}
	if chain.EntryCount() != 3 {
		t.Fatalf("expected 3 chain entries, got %d", chain.EntryCount())
	}
}

func TestAuditNilChain(t *testing.T) {
	log := NewSubstrateAuditLog(nil)
	entry, err := log.EmitRatchetAdvanced(1, 2, "nil-chain")
	if err != nil {
		t.Fatal(err)
	}
	// Should succeed but with empty hashes
	if entry.Hash != "" || entry.PrevHash != "" {
		t.Fatal("nil chain should produce empty hashes")
	}
}

// ─── Deletion Proof Tests ───────────────────────────────────────────────────

func newDeletionProofTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	stmts := []string{
		`CREATE TABLE memories (payload_id TEXT PRIMARY KEY, content TEXT)`,
		`CREATE TABLE substrate_ratchet_states (
			state_id INTEGER PRIMARY KEY AUTOINCREMENT,
			created_at INTEGER NOT NULL,
			shredded_at INTEGER,
			state_bytes BLOB NOT NULL,
			canonical_dim INTEGER NOT NULL,
			sketch_bits INTEGER NOT NULL,
			signature BLOB NOT NULL
		)`,
		`CREATE TABLE substrate_memory_state (
			memory_id TEXT PRIMARY KEY,
			state_id INTEGER NOT NULL
		)`,
		`CREATE TABLE substrate_cuckoo_filter (
			filter_id INTEGER PRIMARY KEY,
			filter_bytes BLOB NOT NULL,
			last_persisted INTEGER NOT NULL
		)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestProveDeletionShreddedMemory(t *testing.T) {
	db := newDeletionProofTestDB(t)
	sd, _ := secrets.Open(t.TempDir())

	// Setup: create ratchet, write a memory, delete + shred
	ratchetMgr, _ := NewRatchetManager(db, sd, nil, 1024, 1, slog.Default())
	cuckooOracle := NewCuckooOracle(1024)
	auditLog := NewSubstrateAuditLog(provenance.NewChainState())

	// Simulate a memory that was written under state 1
	memoryID := "mem-to-shred"
	s0 := ratchetMgr.Current()
	db.Exec(`INSERT INTO substrate_memory_state (memory_id, state_id) VALUES (?, ?)`,
		memoryID, s0.StateID)

	// Advance ratchet (shreds state 1)
	ratchetMgr.Advance("shred")

	// Memory row already absent from memories table (not inserted)
	// Cuckoo doesn't have it either

	proof, err := ProveDeletion(db, cuckooOracle, ratchetMgr, auditLog, nil, memoryID)
	if err != nil {
		t.Fatal(err)
	}

	// Evidence checks
	if proof.CanonicalRowExists {
		t.Fatal("memory row should not exist")
	}
	if proof.CuckooLookupResult {
		t.Fatal("memory should not be in cuckoo filter")
	}
	if proof.OriginalStateID != s0.StateID {
		t.Fatalf("original state_id mismatch: got %d, want %d", proof.OriginalStateID, s0.StateID)
	}
	if !proof.StateBytesZeroed {
		t.Fatal("original state bytes should be zeroed")
	}
	if proof.OriginalStateShreddedAt == nil {
		t.Fatal("original state should have shredded_at set")
	}
	if proof.CurrentStateID <= proof.OriginalStateID {
		t.Fatal("current state_id should be > original")
	}
}

func TestProveDeletionMemoryStillExists(t *testing.T) {
	db := newDeletionProofTestDB(t)
	sd, _ := secrets.Open(t.TempDir())
	ratchetMgr, _ := NewRatchetManager(db, sd, nil, 1024, 1, slog.Default())
	cuckooOracle := NewCuckooOracle(1024)
	auditLog := NewSubstrateAuditLog(nil)

	// Memory still exists
	memoryID := "still-alive"
	db.Exec(`INSERT INTO memories (payload_id, content) VALUES (?, ?)`, memoryID, "data")

	proof, err := ProveDeletion(db, cuckooOracle, ratchetMgr, auditLog, nil, memoryID)
	if err != nil {
		t.Fatal(err)
	}
	// Proof should show memory exists
	if !proof.CanonicalRowExists {
		t.Fatal("proof should show memory still exists")
	}
}

func TestProveDeletionWithSigning(t *testing.T) {
	db := newDeletionProofTestDB(t)
	sd, _ := secrets.Open(t.TempDir())

	pub, priv, _ := ed25519.GenerateKey(nil)
	ratchetMgr, _ := NewRatchetManager(db, sd, priv, 1024, 1, slog.Default())
	cuckooOracle := NewCuckooOracle(1024)
	auditLog := NewSubstrateAuditLog(provenance.NewChainState())

	proof, err := ProveDeletion(db, cuckooOracle, ratchetMgr, auditLog, priv, "nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if len(proof.Signature) == 0 {
		t.Fatal("signature should be present")
	}

	// Verify proof
	if err := VerifyDeletionProof(proof, pub); err != nil {
		t.Fatalf("proof verification failed: %v", err)
	}
}

func TestVerifyDeletionProofTampered(t *testing.T) {
	db := newDeletionProofTestDB(t)
	sd, _ := secrets.Open(t.TempDir())

	pub, priv, _ := ed25519.GenerateKey(nil)
	ratchetMgr, _ := NewRatchetManager(db, sd, priv, 1024, 1, slog.Default())
	cuckooOracle := NewCuckooOracle(1024)
	auditLog := NewSubstrateAuditLog(nil)

	proof, _ := ProveDeletion(db, cuckooOracle, ratchetMgr, auditLog, priv, "test")

	// Tamper with the proof
	proof.MemoryID = "tampered-id"

	err := VerifyDeletionProof(proof, pub)
	if err == nil {
		t.Fatal("tampered proof should fail verification")
	}
}

func TestVerifyDeletionProofMemoryExists(t *testing.T) {
	proof := &DeletionProof{
		MemoryID:           "exists",
		CanonicalRowExists: true,
	}
	err := VerifyDeletionProof(proof, nil)
	if err == nil {
		t.Fatal("proof with existing memory should fail")
	}
}

func TestVerifyDeletionProofNil(t *testing.T) {
	err := VerifyDeletionProof(nil, nil)
	if err == nil {
		t.Fatal("nil proof should fail")
	}
}

func TestProveDeletionNoSubstrateMemoryState(t *testing.T) {
	db := newDeletionProofTestDB(t)
	sd, _ := secrets.Open(t.TempDir())
	ratchetMgr, _ := NewRatchetManager(db, sd, nil, 1024, 1, slog.Default())
	cuckooOracle := NewCuckooOracle(1024)
	auditLog := NewSubstrateAuditLog(nil)

	// Memory never had a substrate_memory_state entry
	proof, err := ProveDeletion(db, cuckooOracle, ratchetMgr, auditLog, nil, "never-sketched")
	if err != nil {
		t.Fatal(err)
	}
	if proof.OriginalStateID != 0 {
		t.Fatal("original_state_id should be 0 for never-sketched memory")
	}
}

// ─── Merkle composition ─────────────────────────────────────────────────────

func TestSubstrateEventsInMerkleRoot(t *testing.T) {
	chain := provenance.NewChainState()
	log := NewSubstrateAuditLog(chain)

	// Emit several substrate events
	for i := 0; i < 5; i++ {
		log.EmitRatchetAdvanced(uint32(i), uint32(i+1), fmt.Sprintf("test-%d", i))
	}

	// Verify chain has 5 entries
	if chain.EntryCount() != 5 {
		t.Fatalf("expected 5 entries, got %d", chain.EntryCount())
	}

	// The Merkle root would be computed from these entries by the Phase 4
	// infrastructure. We verify that substrate events are recorded in the
	// chain and have valid prev_hash linkage.
	if chain.LastHash() == "" {
		t.Fatal("chain should have a non-empty last hash")
	}
}
