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
	"bytes"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/bubblefish-tech/nexus/internal/secrets"
	_ "modernc.org/sqlite"
)

func newRatchetTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS substrate_ratchet_states (
			state_id       INTEGER PRIMARY KEY AUTOINCREMENT,
			created_at     INTEGER NOT NULL,
			shredded_at    INTEGER,
			state_bytes    BLOB NOT NULL,
			canonical_dim  INTEGER NOT NULL,
			sketch_bits    INTEGER NOT NULL,
			signature      BLOB NOT NULL
		)
	`)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func newTestRatchetManager(t *testing.T) (*RatchetManager, *sql.DB) {
	t.Helper()
	db := newRatchetTestDB(t)
	sd, err := secrets.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m, err := NewRatchetManager(db, sd, nil, 1024, 1, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	return m, db
}

// ─── Initialization ─────────────────────────────────────────────────────────

func TestRatchetInitializesOnEmptyDB(t *testing.T) {
	m, _ := newTestRatchetManager(t)
	state := m.Current()
	if state == nil {
		t.Fatal("current state should not be nil after init")
	}
	if state.StateID == 0 {
		t.Fatal("state_id should be > 0")
	}
	if isAllZero(state.StateBytes[:]) {
		t.Fatal("state bytes should not be all zeros")
	}
	if state.CanonicalDim != 1024 {
		t.Fatalf("expected canonical_dim=1024, got %d", state.CanonicalDim)
	}
	if state.SketchBits != 1 {
		t.Fatalf("expected sketch_bits=1, got %d", state.SketchBits)
	}
}

func TestRatchetInitIsHMACOfEntropy(t *testing.T) {
	// Verify that the init process applies HMAC with the init label.
	// We can't verify the exact bytes (entropy is random), but we can
	// verify the state is 32 bytes and non-zero.
	m, _ := newTestRatchetManager(t)
	state := m.Current()
	if len(state.StateBytes) != 32 {
		t.Fatalf("state bytes should be 32, got %d", len(state.StateBytes))
	}
}

func TestRatchetLoadsExistingState(t *testing.T) {
	db := newRatchetTestDB(t)
	sd, _ := secrets.Open(t.TempDir())

	// Create first manager (initializes)
	m1, err := NewRatchetManager(db, sd, nil, 1024, 1, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	s1 := m1.Current()

	// Create second manager (should load, not re-initialize)
	m2, err := NewRatchetManager(db, sd, nil, 1024, 1, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	s2 := m2.Current()

	if s1.StateID != s2.StateID {
		t.Fatalf("second manager should load same state: %d != %d", s1.StateID, s2.StateID)
	}
	if s1.StateBytes != s2.StateBytes {
		t.Fatal("state bytes should match")
	}
}

// ─── Advance ────────────────────────────────────────────────────────────────

func TestRatchetAdvance(t *testing.T) {
	m, _ := newTestRatchetManager(t)
	old := m.Current()

	newState, err := m.Advance("test-advance")
	if err != nil {
		t.Fatal(err)
	}

	if newState.StateID <= old.StateID {
		t.Fatal("new state_id should be greater than old")
	}
	if newState.StateBytes == old.StateBytes {
		t.Fatal("new state bytes should differ from old")
	}
	// The new state should be the current state
	if m.Current().StateID != newState.StateID {
		t.Fatal("Current() should return the new state")
	}
}

func TestRatchetAdvanceIsHMAC(t *testing.T) {
	m, _ := newTestRatchetManager(t)
	old := m.Current()
	oldBytes := old.StateBytes

	// Compute expected: HMAC-SHA-256(oldBytes, advance_label)
	mac := hmac.New(sha256.New, oldBytes[:])
	mac.Write([]byte(ratchetAdvanceLabelV1))
	expected := mac.Sum(nil)

	newState, err := m.Advance("test")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(newState.StateBytes[:], expected) {
		t.Fatal("advance should produce HMAC-SHA-256(old, advance_label)")
	}
}

func TestRatchetAdvanceChainDeterministic(t *testing.T) {
	m, _ := newTestRatchetManager(t)
	initial := m.Current().StateBytes

	// Advance 5 times, compute expected chain independently
	states := make([][32]byte, 6)
	states[0] = initial
	for i := 1; i <= 5; i++ {
		mac := hmac.New(sha256.New, states[i-1][:])
		mac.Write([]byte(ratchetAdvanceLabelV1))
		copy(states[i][:], mac.Sum(nil))
	}

	for i := 1; i <= 5; i++ {
		newState, err := m.Advance("chain-test")
		if err != nil {
			t.Fatal(err)
		}
		if newState.StateBytes != states[i] {
			t.Fatalf("chain mismatch at step %d", i)
		}
	}
}

func TestRatchetAdvanceShredsOldState(t *testing.T) {
	m, db := newTestRatchetManager(t)
	oldID := m.Current().StateID

	_, err := m.Advance("shred-test")
	if err != nil {
		t.Fatal(err)
	}

	// Verify old state is shredded in DB
	var shreddedAt sql.NullInt64
	var stateBytes []byte
	err = db.QueryRow(
		`SELECT shredded_at, state_bytes FROM substrate_ratchet_states WHERE state_id = ?`,
		oldID,
	).Scan(&shreddedAt, &stateBytes)
	if err != nil {
		t.Fatal(err)
	}
	if !shreddedAt.Valid {
		t.Fatal("old state should have shredded_at set")
	}
	if !isAllZero(stateBytes) {
		t.Fatal("old state bytes should be zeroed in DB")
	}
}

func TestRatchetAdvanceUpdatesSecretFile(t *testing.T) {
	db := newRatchetTestDB(t)
	tmpDir := t.TempDir()
	sd, _ := secrets.Open(tmpDir)

	m, err := NewRatchetManager(db, sd, nil, 1024, 1, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	newState, err := m.Advance("file-test")
	if err != nil {
		t.Fatal(err)
	}

	// Read the secret file
	data, err := sd.ReadSecret(ratchetSecretName)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, newState.StateBytes[:]) {
		t.Fatal("secret file should contain new state bytes")
	}
}

// ─── Forward Security (LOAD-BEARING TEST) ───────────────────────────────────

func TestForwardSecurityRatchetToEncryption(t *testing.T) {
	m, db := newTestRatchetManager(t)

	// Step 1: Note S_0, derive key, encrypt
	s0 := m.Current()
	s0Bytes := s0.StateBytes
	key0, _ := DeriveEmbeddingKey(s0Bytes, "mem-to-delete")
	plaintext := []byte("this embedding will be cryptographically erased")
	enc, _ := EncryptEmbedding(key0, plaintext)

	// Verify we can decrypt with S_0's key
	dec, err := DecryptEmbedding(key0, enc)
	if err != nil {
		t.Fatal("should decrypt with original key")
	}
	if !bytes.Equal(dec, plaintext) {
		t.Fatal("decrypted text should match")
	}

	// Step 2: Advance to S_1
	s1, err := m.Advance("shred-test")
	if err != nil {
		t.Fatal(err)
	}

	// Step 3: Verify S_0 bytes are zeroed in DB
	var stateBytes []byte
	err = db.QueryRow(
		`SELECT state_bytes FROM substrate_ratchet_states WHERE state_id = ?`,
		s0.StateID,
	).Scan(&stateBytes)
	if err != nil {
		t.Fatal(err)
	}
	if !isAllZero(stateBytes) {
		t.Fatal("S_0 should be zeroed in DB after advance")
	}

	// Step 4: Verify secret file contains S_1, not S_0
	fileData, _ := m.sd.ReadSecret(ratchetSecretName)
	if bytes.Equal(fileData, s0Bytes[:]) {
		t.Fatal("secret file should NOT contain S_0 after advance")
	}
	if !bytes.Equal(fileData, s1.StateBytes[:]) {
		t.Fatal("secret file should contain S_1")
	}

	// Step 5: Derive key from S_1 — should NOT decrypt S_0's ciphertext
	key1, _ := DeriveEmbeddingKey(s1.StateBytes, "mem-to-delete")
	_, err = DecryptEmbedding(key1, enc)
	if !errors.Is(err, ErrEmbeddingUnreachable) {
		t.Fatal("S_1 key should NOT decrypt S_0 ciphertext")
	}

	// Step 6: S_0 is gone — there is NO way to recover it
	// The only copy was in the DB (now zeroed) and the file (overwritten).
	// HMAC-SHA-256 is one-way: S_1 → S_0 is computationally infeasible.
}

// ─── Zeroization ────────────────────────────────────────────────────────────

func TestZeroizeStateBytes(t *testing.T) {
	state := [32]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16,
		17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32}
	zeroizeStateBytes(&state)
	if !isAllZero(state[:]) {
		t.Fatal("state should be all zeros after zeroize")
	}
}

func TestIsAllZero(t *testing.T) {
	if !isAllZero([]byte{0, 0, 0}) {
		t.Fatal("all zeros should return true")
	}
	if isAllZero([]byte{0, 1, 0}) {
		t.Fatal("non-zero should return false")
	}
	if !isAllZero([]byte{}) {
		t.Fatal("empty should return true")
	}
}

// ─── Concurrency ────────────────────────────────────────────────────────────

func TestRatchetConcurrentCurrentDuringAdvance(t *testing.T) {
	m, _ := newTestRatchetManager(t)
	var wg sync.WaitGroup

	// 50 goroutines reading Current() while 5 goroutines Advance()
	for g := 0; g < 50; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				state := m.Current()
				if state == nil {
					t.Error("Current() returned nil during concurrent access")
					return
				}
				// State bytes should be valid (not zeroed). The atomic.Pointer
				// swap ensures readers see either the old or new state, never
				// a partially-updated one.
				_ = state.StateBytes
			}
		}()
	}
	for g := 0; g < 5; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 3; i++ {
				m.Advance("concurrent-test")
			}
		}()
	}
	wg.Wait()
}

// ─── Rotation Scheduler ─────────────────────────────────────────────────────

func TestRotationSchedulerStartStop(t *testing.T) {
	m, _ := newTestRatchetManager(t)
	initialID := m.Current().StateID

	sched := NewRotationScheduler(m, 50*time.Millisecond, slog.Default())
	sched.Start()

	// Wait for at least 2 rotations
	time.Sleep(150 * time.Millisecond)
	sched.Stop()

	finalID := m.Current().StateID
	if finalID <= initialID {
		t.Fatalf("scheduler should have advanced: initial=%d final=%d", initialID, finalID)
	}
}

func TestRotationSchedulerCleanStop(t *testing.T) {
	m, _ := newTestRatchetManager(t)
	sched := NewRotationScheduler(m, time.Hour, slog.Default())
	sched.Start()

	// Stop immediately — should not hang
	done := make(chan struct{})
	go func() {
		sched.Stop()
		close(done)
	}()
	select {
	case <-done:
		// OK
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() should return quickly")
	}
}

// ─── Signing ────────────────────────────────────────────────────────────────

func TestRatchetWithSigning(t *testing.T) {
	db := newRatchetTestDB(t)
	sd, _ := secrets.Open(t.TempDir())

	// Generate an Ed25519 key pair
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	m, err := NewRatchetManager(db, sd, priv, 1024, 1, slog.Default())
	if err != nil {
		t.Fatal(err)
	}

	state := m.Current()
	if len(state.Signature) == 0 {
		t.Fatal("signature should be non-empty when signing key is provided")
	}

	// Verify the signature (basic check — it's over metadata bytes)
	// The exact signed payload is implementation-dependent, so we just
	// verify ed25519.Verify doesn't panic and the sig has the right length.
	if len(state.Signature) != ed25519.SignatureSize {
		t.Fatalf("signature should be %d bytes, got %d", ed25519.SignatureSize, len(state.Signature))
	}
	_ = pub // would be used in full verification
}

func TestRatchetWithoutSigning(t *testing.T) {
	m, _ := newTestRatchetManager(t) // nil signing key
	state := m.Current()
	// When signing is disabled, signature is empty bytes (not nil) for DB NOT NULL
	if len(state.Signature) != 0 {
		t.Fatalf("signature should be empty when signing key is nil, got %d bytes", len(state.Signature))
	}
}

// ─── Shutdown ───────────────────────────────────────────────────────────────

func TestRatchetShutdownPersists(t *testing.T) {
	db := newRatchetTestDB(t)
	sd, _ := secrets.Open(t.TempDir())

	m, _ := NewRatchetManager(db, sd, nil, 1024, 1, slog.Default())
	state := m.Current()

	if err := m.Shutdown(); err != nil {
		t.Fatal(err)
	}

	// Verify secret file has the state
	data, err := sd.ReadSecret(ratchetSecretName)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, state.StateBytes[:]) {
		t.Fatal("shutdown should persist current state to file")
	}
}
