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
	"database/sql"
	"log/slog"
	"testing"

	nexuscrypto "github.com/bubblefish-tech/nexus/internal/crypto"
	"github.com/bubblefish-tech/nexus/internal/secrets"
	_ "modernc.org/sqlite"
)

// newTestMKM creates a MasterKeyManager with a fixed password for testing.
func newTestMKM(t *testing.T) *nexuscrypto.MasterKeyManager {
	t.Helper()
	saltPath := t.TempDir() + "/crypto.salt"
	mkm, err := nexuscrypto.NewMasterKeyManager("test-password-cu09", saltPath)
	if err != nil {
		t.Fatal(err)
	}
	if !mkm.IsEnabled() {
		t.Fatal("MKM should be enabled with a password")
	}
	return mkm
}

// newTestMKMWithPassword creates a MKM with the given password.
func newTestMKMWithPassword(t *testing.T, password string) *nexuscrypto.MasterKeyManager {
	t.Helper()
	saltPath := t.TempDir() + "/crypto.salt"
	mkm, err := nexuscrypto.NewMasterKeyManager(password, saltPath)
	if err != nil {
		t.Fatal(err)
	}
	return mkm
}

// newEncryptedRatchetDB creates a test DB with the full ratchet table schema.
func newEncryptedRatchetDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	_, err = db.Exec(`
		CREATE TABLE substrate_ratchet_states (
			state_id                INTEGER PRIMARY KEY AUTOINCREMENT,
			created_at              INTEGER NOT NULL,
			shredded_at             INTEGER,
			state_bytes             BLOB NOT NULL,
			canonical_dim           INTEGER NOT NULL,
			sketch_bits             INTEGER NOT NULL,
			signature               BLOB NOT NULL,
			state_bytes_encrypted   BLOB,
			state_bytes_enc_version INTEGER NOT NULL DEFAULT 0
		)
	`)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

// newEncryptedCuckooDB creates a test DB with the full cuckoo table schema.
func newEncryptedCuckooDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	_, err = db.Exec(`
		CREATE TABLE substrate_cuckoo_filter (
			filter_id                INTEGER PRIMARY KEY,
			filter_bytes             BLOB NOT NULL,
			last_persisted           INTEGER NOT NULL,
			filter_bytes_encrypted   BLOB,
			filter_bytes_enc_version INTEGER NOT NULL DEFAULT 0
		)
	`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE memories (payload_id TEXT PRIMARY KEY, content TEXT)`)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

// TestSubstrateEncryptor_NilMKM verifies NewSubstrateEncryptor returns nil for
// a disabled MKM.
func TestSubstrateEncryptor_NilMKM(t *testing.T) {
	t.Helper()
	if NewSubstrateEncryptor(nil) != nil {
		t.Fatal("expected nil for nil mkm")
	}
	mkm, _ := nexuscrypto.NewMasterKeyManager("", t.TempDir()+"/s")
	if NewSubstrateEncryptor(mkm) != nil {
		t.Fatal("expected nil for disabled mkm")
	}
}

// TestRatchetState_EncryptRoundTrip verifies that a ratchet state written with
// encryption is correctly decrypted on reload.
func TestRatchetState_EncryptRoundTrip(t *testing.T) {
	t.Helper()
	mkm := newTestMKM(t)
	enc := NewSubstrateEncryptor(mkm)
	db := newEncryptedRatchetDB(t)
	sd, _ := secrets.Open(t.TempDir())

	// Write with encryption
	m, err := NewRatchetManager(db, sd, nil, 1024, 1, enc, slog.Default())
	if err != nil {
		t.Fatal("NewRatchetManager:", err)
	}
	original := m.Current()
	if original == nil {
		t.Fatal("expected active state")
	}

	// Reload with same encryptor — should decrypt successfully
	m2, err := NewRatchetManager(db, sd, nil, 1024, 1, enc, slog.Default())
	if err != nil {
		t.Fatal("reload:", err)
	}
	reloaded := m2.Current()
	if reloaded.StateBytes != original.StateBytes {
		t.Fatal("decrypted state bytes differ from original")
	}
	if reloaded.StateID != original.StateID {
		t.Fatalf("expected state_id %d, got %d", original.StateID, reloaded.StateID)
	}
}

// TestRatchetState_PlaintextNotInDB verifies that when encryption is enabled,
// the state_bytes column holds zeros (not the real key) and enc_version is 1.
func TestRatchetState_PlaintextNotInDB(t *testing.T) {
	t.Helper()
	mkm := newTestMKM(t)
	enc := NewSubstrateEncryptor(mkm)
	db := newEncryptedRatchetDB(t)
	sd, _ := secrets.Open(t.TempDir())

	m, err := NewRatchetManager(db, sd, nil, 1024, 1, enc, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	stateID := m.Current().StateID

	var stateBytes []byte
	var encVersion int
	err = db.QueryRow(
		`SELECT state_bytes, state_bytes_enc_version FROM substrate_ratchet_states WHERE state_id = ?`,
		stateID,
	).Scan(&stateBytes, &encVersion)
	if err != nil {
		t.Fatal(err)
	}
	// The plaintext column must not hold the real key (enc_version=1 means encrypted).
	if encVersion != 1 {
		t.Fatalf("expected enc_version=1, got %d", encVersion)
	}
	// state_bytes is kept for NOT NULL constraint but should be treated as non-authoritative.
	// The real bytes are in state_bytes_encrypted.
	var encBlob []byte
	err = db.QueryRow(
		`SELECT state_bytes_encrypted FROM substrate_ratchet_states WHERE state_id = ?`,
		stateID,
	).Scan(&encBlob)
	if err != nil {
		t.Fatal(err)
	}
	if len(encBlob) == 0 {
		t.Fatal("expected non-empty encrypted blob")
	}
}

// TestRatchetState_WrongKeyFails verifies that decryption fails with a different key.
func TestRatchetState_WrongKeyFails(t *testing.T) {
	t.Helper()
	mkm1 := newTestMKMWithPassword(t, "password-one")
	mkm2 := newTestMKMWithPassword(t, "password-two")
	enc1 := NewSubstrateEncryptor(mkm1)
	enc2 := NewSubstrateEncryptor(mkm2)

	db := newEncryptedRatchetDB(t)
	sd, _ := secrets.Open(t.TempDir())

	// Write with enc1
	_, err := NewRatchetManager(db, sd, nil, 1024, 1, enc1, slog.Default())
	if err != nil {
		t.Fatal(err)
	}

	// Reload with enc2 — must fail
	_, err = NewRatchetManager(db, sd, nil, 1024, 1, enc2, slog.Default())
	if err == nil {
		t.Fatal("expected decryption failure with wrong key")
	}
}

// TestRatchetState_BackwardCompat verifies that an unencrypted row (enc_version=0)
// loads correctly without an encryptor.
func TestRatchetState_BackwardCompat(t *testing.T) {
	t.Helper()
	db := newEncryptedRatchetDB(t)
	sd, _ := secrets.Open(t.TempDir())

	// Write without encryption
	m1, err := NewRatchetManager(db, sd, nil, 1024, 1, nil, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	original := m1.Current().StateBytes

	// Reload without encryption — should work
	m2, err := NewRatchetManager(db, sd, nil, 1024, 1, nil, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if m2.Current().StateBytes != original {
		t.Fatal("backward compat: state bytes differ")
	}
}

// TestRatchetState_ShredClearsEncryptedColumn verifies that shredding a state
// nullifies both the plaintext and encrypted columns.
func TestRatchetState_ShredClearsEncryptedColumn(t *testing.T) {
	t.Helper()
	mkm := newTestMKM(t)
	enc := NewSubstrateEncryptor(mkm)
	db := newEncryptedRatchetDB(t)
	sd, _ := secrets.Open(t.TempDir())

	m, err := NewRatchetManager(db, sd, nil, 1024, 1, enc, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	oldID := m.Current().StateID

	// Advance ratchet — this shreds the old state
	if _, err := m.Advance("test-shred"); err != nil {
		t.Fatal(err)
	}

	var encBlob []byte
	var encVersion int
	err = db.QueryRow(
		`SELECT state_bytes_encrypted, state_bytes_enc_version FROM substrate_ratchet_states WHERE state_id = ?`,
		oldID,
	).Scan(&encBlob, &encVersion)
	if err != nil {
		t.Fatal(err)
	}
	if encBlob != nil {
		t.Fatal("expected encrypted column to be NULL after shred")
	}
	if encVersion != 0 {
		t.Fatalf("expected enc_version=0 after shred, got %d", encVersion)
	}
}

// TestCuckooFilter_EncryptRoundTrip verifies that a cuckoo filter persisted
// with encryption is correctly decrypted on reload.
func TestCuckooFilter_EncryptRoundTrip(t *testing.T) {
	t.Helper()
	mkm := newTestMKM(t)
	enc := NewSubstrateEncryptor(mkm)
	db := newEncryptedCuckooDB(t)

	oracle := NewCuckooOracle(1024)
	oracle.encryptor = enc

	if err := oracle.Insert("mem-alpha"); err != nil {
		t.Fatal(err)
	}
	if err := oracle.Insert("mem-beta"); err != nil {
		t.Fatal(err)
	}
	if err := oracle.Persist(db); err != nil {
		t.Fatal("persist:", err)
	}

	loaded, err := LoadCuckooOracle(db, 1024, enc)
	if err != nil {
		t.Fatal("load:", err)
	}
	if !loaded.Lookup("mem-alpha") {
		t.Fatal("mem-alpha should be in loaded filter")
	}
	if !loaded.Lookup("mem-beta") {
		t.Fatal("mem-beta should be in loaded filter")
	}
}

// TestCuckooFilter_PlaintextIsPlaceholder verifies the plaintext filter_bytes
// column holds only a placeholder when encryption is enabled.
func TestCuckooFilter_PlaintextIsPlaceholder(t *testing.T) {
	t.Helper()
	mkm := newTestMKM(t)
	enc := NewSubstrateEncryptor(mkm)
	db := newEncryptedCuckooDB(t)

	oracle := NewCuckooOracle(1024)
	oracle.encryptor = enc
	oracle.Insert("x")
	oracle.Persist(db)

	var plainBytes []byte
	var encVersion int
	db.QueryRow(`SELECT filter_bytes, filter_bytes_enc_version FROM substrate_cuckoo_filter WHERE filter_id = 1`).
		Scan(&plainBytes, &encVersion)

	if encVersion != 1 {
		t.Fatalf("expected enc_version=1, got %d", encVersion)
	}
	if !bytes.Equal(plainBytes, []byte{0}) {
		t.Fatalf("expected placeholder byte, got %d bytes", len(plainBytes))
	}
}

// TestCuckooFilter_WrongKeyFails verifies that loading with a different key fails.
func TestCuckooFilter_WrongKeyFails(t *testing.T) {
	t.Helper()
	mkm1 := newTestMKMWithPassword(t, "cuckoo-key-one")
	mkm2 := newTestMKMWithPassword(t, "cuckoo-key-two")
	enc1 := NewSubstrateEncryptor(mkm1)
	enc2 := NewSubstrateEncryptor(mkm2)
	db := newEncryptedCuckooDB(t)

	oracle := NewCuckooOracle(1024)
	oracle.encryptor = enc1
	oracle.Insert("mem-1")
	oracle.Persist(db)

	_, err := LoadCuckooOracle(db, 1024, enc2)
	if err == nil {
		t.Fatal("expected decryption failure with wrong key")
	}
}

// TestCuckooFilter_BackwardCompat verifies that an unencrypted filter loads
// correctly without an encryptor.
func TestCuckooFilter_BackwardCompat(t *testing.T) {
	t.Helper()
	db := newEncryptedCuckooDB(t)

	oracle := NewCuckooOracle(1024)
	oracle.Insert("plain-mem")
	oracle.Persist(db)

	loaded, err := LoadCuckooOracle(db, 1024, nil)
	if err != nil {
		t.Fatal("load unencrypted:", err)
	}
	if !loaded.Lookup("plain-mem") {
		t.Fatal("expected plain-mem in unencrypted loaded filter")
	}
}
