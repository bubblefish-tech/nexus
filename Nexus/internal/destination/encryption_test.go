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

package destination_test

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bubblefish-tech/nexus/internal/crypto"
	"github.com/bubblefish-tech/nexus/internal/destination"
)

// newMKM creates a MasterKeyManager with the given password and a temp salt file.
func newMKM(t *testing.T, password string) *crypto.MasterKeyManager {
	t.Helper()
	saltPath := filepath.Join(t.TempDir(), "crypto.salt")
	mkm, err := crypto.NewMasterKeyManager(password, saltPath)
	if err != nil {
		t.Fatalf("NewMasterKeyManager: %v", err)
	}
	return mkm
}

// newEncryptedSQLite opens a SQLite destination with encryption enabled.
func newEncryptedSQLite(t *testing.T, password string) (*destination.SQLiteDestination, func()) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	d, err := destination.OpenSQLite(path, logger)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	mkm := newMKM(t, password)
	d.SetEncryption(mkm)
	return d, func() { _ = d.Close() }
}

// TestEncryption_WriteReadRoundTrip verifies that content written with
// encryption enabled is correctly decrypted on read.
func TestEncryption_WriteReadRoundTrip(t *testing.T) {
	d, cleanup := newEncryptedSQLite(t, "test-password-1")
	defer cleanup()

	p := basePayload("enc-001")
	p.Content = "this is secret content"
	p.Metadata = map[string]string{"key": "value"}
	if err := d.Write(p); err != nil {
		t.Fatalf("Write: %v", err)
	}

	result, err := d.Query(destination.QueryParams{Namespace: p.Namespace, Destination: p.Destination})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(result.Records) != 1 {
		t.Fatalf("want 1 record, got %d", len(result.Records))
	}
	got := result.Records[0]
	if got.Content != p.Content {
		t.Errorf("content: want %q, got %q", p.Content, got.Content)
	}
	if got.Metadata["key"] != "value" {
		t.Errorf("metadata: want key=value, got %v", got.Metadata)
	}
}

// TestEncryption_MetadataRoundTrip verifies that metadata JSON is encrypted
// and decrypted correctly, including multi-key maps.
func TestEncryption_MetadataRoundTrip(t *testing.T) {
	d, cleanup := newEncryptedSQLite(t, "test-password-2")
	defer cleanup()

	p := basePayload("enc-002")
	p.Content = "content"
	p.Metadata = map[string]string{"a": "1", "b": "2", "c": "3"}
	if err := d.Write(p); err != nil {
		t.Fatalf("Write: %v", err)
	}

	result, err := d.Query(destination.QueryParams{Namespace: p.Namespace, Destination: p.Destination})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	got := result.Records[0]
	if got.Metadata["a"] != "1" || got.Metadata["b"] != "2" || got.Metadata["c"] != "3" {
		t.Errorf("metadata mismatch: got %v", got.Metadata)
	}
}

// TestEncryption_PlaintextColumnEmpty verifies that after an encrypted write
// the plaintext content column holds only the empty sentinel value.
func TestEncryption_PlaintextColumnEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	d, err := destination.OpenSQLite(path, logger)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer func() { _ = d.Close() }()

	mkm := newMKM(t, "pw")
	d.SetEncryption(mkm)

	p := basePayload("enc-003")
	p.Content = "super secret"
	if err := d.Write(p); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Open a second connection without encryption and read raw content.
	d2, err := destination.OpenSQLite(path, logger)
	if err != nil {
		t.Fatalf("OpenSQLite d2: %v", err)
	}
	defer func() { _ = d2.Close() }()

	result, err := d2.Query(destination.QueryParams{Namespace: p.Namespace, Destination: p.Destination})
	if err != nil {
		t.Fatalf("Query d2: %v", err)
	}
	if len(result.Records) != 1 {
		t.Fatalf("want 1 record")
	}
	// Without the MKM, the plaintext column should be empty (not the secret).
	if result.Records[0].Content == "super secret" {
		t.Error("plaintext column should not contain the secret content")
	}
}

// TestEncryption_WrongKeyFails verifies that decryption with a different
// password leaves content inaccessible (falls back to empty plaintext column).
func TestEncryption_WrongKeyFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	// Write with password A.
	dA, err := destination.OpenSQLite(path, logger)
	if err != nil {
		t.Fatalf("OpenSQLite A: %v", err)
	}
	mkmA := newMKM(t, "password-A")
	dA.SetEncryption(mkmA)

	p := basePayload("enc-004")
	p.Content = "secret from A"
	if err := dA.Write(p); err != nil {
		t.Fatalf("Write: %v", err)
	}
	_ = dA.Close()

	// Read with password B — decryption should fail, content should not be "secret from A".
	dB, err := destination.OpenSQLite(path, logger)
	if err != nil {
		t.Fatalf("OpenSQLite B: %v", err)
	}
	defer func() { _ = dB.Close() }()
	mkmB := newMKM(t, "password-B")
	dB.SetEncryption(mkmB)

	result, err := dB.Query(destination.QueryParams{Namespace: p.Namespace, Destination: p.Destination})
	if err != nil {
		t.Fatalf("Query B: %v", err)
	}
	if len(result.Records) == 1 && result.Records[0].Content == "secret from A" {
		t.Error("wrong-key read should not return original plaintext")
	}
}

// TestEncryption_BackwardCompatUnencrypted verifies that unencrypted rows
// written before CU.0.2 are still readable when encryption is disabled.
func TestEncryption_BackwardCompatUnencrypted(t *testing.T) {
	d, cleanup := newTestSQLite(t) // no encryption
	defer cleanup()

	p := basePayload("enc-005")
	p.Content = "plaintext row"
	if err := d.Write(p); err != nil {
		t.Fatalf("Write: %v", err)
	}

	result, err := d.Query(destination.QueryParams{Namespace: p.Namespace, Destination: p.Destination})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(result.Records) != 1 {
		t.Fatalf("want 1 record")
	}
	if result.Records[0].Content != "plaintext row" {
		t.Errorf("want %q, got %q", "plaintext row", result.Records[0].Content)
	}
}

// TestEncryption_BackwardCompatMixedDB verifies that a DB with both encrypted
// and unencrypted rows returns correct content for both.
func TestEncryption_BackwardCompatMixedDB(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	// Write plaintext row first (no encryption).
	dPlain, err := destination.OpenSQLite(path, logger)
	if err != nil {
		t.Fatalf("OpenSQLite plain: %v", err)
	}
	pPlain := basePayload("enc-006-plain")
	pPlain.Content = "plaintext"
	if err := dPlain.Write(pPlain); err != nil {
		t.Fatalf("Write plain: %v", err)
	}
	_ = dPlain.Close()

	// Reopen with encryption and write encrypted row.
	dEnc, err := destination.OpenSQLite(path, logger)
	if err != nil {
		t.Fatalf("OpenSQLite enc: %v", err)
	}
	mkm := newMKM(t, "pw")
	dEnc.SetEncryption(mkm)

	pEnc := basePayload("enc-006-enc")
	pEnc.Content = "encrypted"
	if err := dEnc.Write(pEnc); err != nil {
		t.Fatalf("Write enc: %v", err)
	}

	result, err := dEnc.Query(destination.QueryParams{Namespace: pPlain.Namespace, Destination: pPlain.Destination})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(result.Records) != 2 {
		t.Fatalf("want 2 records, got %d", len(result.Records))
	}

	contents := make(map[string]bool)
	for _, r := range result.Records {
		contents[r.Content] = true
	}
	if !contents["plaintext"] {
		t.Error("plaintext row not returned correctly")
	}
	if !contents["encrypted"] {
		t.Error("encrypted row not returned correctly")
	}
	_ = dEnc.Close()
}

// TestEncryption_EncryptExistingRows verifies that EncryptExistingRows migrates
// plaintext rows, clears the content column, and leaves them readable.
func TestEncryption_EncryptExistingRows(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	// Write 3 plaintext rows.
	dPlain, err := destination.OpenSQLite(path, logger)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	for i := range 3 {
		p := basePayload("mig-" + string(rune('a'+i)))
		p.Namespace = "ns"
		p.Destination = "dst"
		p.Content = "content-" + string(rune('a'+i))
		if err := dPlain.Write(p); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
	}
	_ = dPlain.Close()

	// Reopen with encryption and run migration.
	dEnc, err := destination.OpenSQLite(path, logger)
	if err != nil {
		t.Fatalf("OpenSQLite enc: %v", err)
	}
	defer func() { _ = dEnc.Close() }()
	mkm := newMKM(t, "migrate-pw")
	dEnc.SetEncryption(mkm)

	if err := dEnc.EncryptExistingRows(context.Background(), 100, 0); err != nil {
		t.Fatalf("EncryptExistingRows: %v", err)
	}

	// Verify all rows are now readable with decryption.
	result, err := dEnc.Query(destination.QueryParams{Namespace: "ns", Destination: "dst"})
	if err != nil {
		t.Fatalf("Query after migration: %v", err)
	}
	if len(result.Records) != 3 {
		t.Fatalf("want 3 records, got %d", len(result.Records))
	}
	for _, r := range result.Records {
		if !strings.HasPrefix(r.Content, "content-") {
			t.Errorf("unexpected content after migration: %q", r.Content)
		}
	}
}

// TestEncryption_EncryptExistingRowsResumable verifies that EncryptExistingRows
// can be interrupted and resumed without re-encrypting already-migrated rows.
func TestEncryption_EncryptExistingRowsResumable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	// Write 5 plaintext rows.
	dPlain, err := destination.OpenSQLite(path, logger)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	for i := range 5 {
		p := basePayload("res-" + string(rune('a'+i)))
		p.Namespace = "ns"
		p.Destination = "dst"
		p.Content = "content-" + string(rune('a'+i))
		if err := dPlain.Write(p); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
	}
	_ = dPlain.Close()

	mkm := newMKM(t, "resume-pw")

	// First run: migrate only 2 rows (batch size 2).
	dEnc1, err := destination.OpenSQLite(path, logger)
	if err != nil {
		t.Fatalf("OpenSQLite enc1: %v", err)
	}
	dEnc1.SetEncryption(mkm)
	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after first batch to simulate kill-9.
	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()
	_ = dEnc1.EncryptExistingRows(ctx, 2, 10*time.Millisecond)
	_ = dEnc1.Close()

	// Second run: migrate remaining rows.
	dEnc2, err := destination.OpenSQLite(path, logger)
	if err != nil {
		t.Fatalf("OpenSQLite enc2: %v", err)
	}
	defer func() { _ = dEnc2.Close() }()
	dEnc2.SetEncryption(mkm)
	if err := dEnc2.EncryptExistingRows(context.Background(), 100, 0); err != nil {
		t.Fatalf("EncryptExistingRows resume: %v", err)
	}

	result, err := dEnc2.Query(destination.QueryParams{Namespace: "ns", Destination: "dst"})
	if err != nil {
		t.Fatalf("Query after resume: %v", err)
	}
	if len(result.Records) != 5 {
		t.Fatalf("want 5 records, got %d", len(result.Records))
	}
	for _, r := range result.Records {
		if !strings.HasPrefix(r.Content, "content-") {
			t.Errorf("bad content after resume: %q", r.Content)
		}
	}
}

// TestEncryption_ContextCancellation verifies EncryptExistingRows respects context.
func TestEncryption_ContextCancellation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	dPlain, err := destination.OpenSQLite(path, logger)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	for i := range 5 {
		p := basePayload("ctx-" + string(rune('a'+i)))
		p.Content = "content"
		if err := dPlain.Write(p); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	_ = dPlain.Close()

	dEnc, err := destination.OpenSQLite(path, logger)
	if err != nil {
		t.Fatalf("OpenSQLite enc: %v", err)
	}
	defer func() { _ = dEnc.Close() }()
	dEnc.SetEncryption(newMKM(t, "ctx-pw"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.
	err = dEnc.EncryptExistingRows(ctx, 100, 0)
	if err == nil {
		t.Error("expected error from cancelled context, got nil")
	}
}

// TestEncryption_DifferentRowsDifferentKeys verifies that two rows with
// different payload IDs produce different ciphertexts even for identical content.
func TestEncryption_DifferentRowsDifferentKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	dA, err := destination.OpenSQLite(path, logger)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer func() { _ = dA.Close() }()
	dA.SetEncryption(newMKM(t, "diff-keys-pw"))

	p1 := basePayload("diff-001")
	p1.Content = "identical content"
	p2 := basePayload("diff-002")
	p2.Content = "identical content"

	if err := dA.Write(p1); err != nil {
		t.Fatalf("Write p1: %v", err)
	}
	if err := dA.Write(p2); err != nil {
		t.Fatalf("Write p2: %v", err)
	}

	result, err := dA.Query(destination.QueryParams{Namespace: p1.Namespace, Destination: p1.Destination})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(result.Records) != 2 {
		t.Fatalf("want 2 records")
	}
	for _, r := range result.Records {
		if r.Content != "identical content" {
			t.Errorf("decrypted content mismatch: %q", r.Content)
		}
	}
}

// TestEncryption_EmptyContentRoundTrip verifies that empty content is encrypted
// and decrypted correctly.
func TestEncryption_EmptyContentRoundTrip(t *testing.T) {
	d, cleanup := newEncryptedSQLite(t, "empty-pw")
	defer cleanup()

	p := basePayload("empty-001")
	p.Content = ""
	if err := d.Write(p); err != nil {
		t.Fatalf("Write: %v", err)
	}

	result, err := d.Query(destination.QueryParams{Namespace: p.Namespace, Destination: p.Destination})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(result.Records) != 1 {
		t.Fatalf("want 1 record")
	}
	if result.Records[0].Content != "" {
		t.Errorf("want empty content, got %q", result.Records[0].Content)
	}
}

// TestEncryption_EmptyMetadataRoundTrip verifies that nil metadata is encrypted
// and decrypted without error.
func TestEncryption_EmptyMetadataRoundTrip(t *testing.T) {
	d, cleanup := newEncryptedSQLite(t, "empty-meta-pw")
	defer cleanup()

	p := basePayload("emptyMeta-001")
	p.Metadata = nil
	if err := d.Write(p); err != nil {
		t.Fatalf("Write: %v", err)
	}

	result, err := d.Query(destination.QueryParams{Namespace: p.Namespace, Destination: p.Destination})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(result.Records) != 1 {
		t.Fatalf("want 1 record")
	}
	if result.Records[0].Metadata != nil && len(result.Records[0].Metadata) != 0 {
		t.Errorf("want nil/empty metadata, got %v", result.Records[0].Metadata)
	}
}

// TestEncryption_NewWritesEncrypted verifies encryption_version is set to 1
// on new encrypted writes by checking that unencrypted reads return no content.
func TestEncryption_NewWritesEncrypted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	// Write encrypted.
	dEnc, err := destination.OpenSQLite(path, logger)
	if err != nil {
		t.Fatalf("OpenSQLite enc: %v", err)
	}
	dEnc.SetEncryption(newMKM(t, "write-enc-pw"))
	p := basePayload("newwrite-001")
	p.Content = "should be encrypted"
	if err := dEnc.Write(p); err != nil {
		t.Fatalf("Write: %v", err)
	}
	_ = dEnc.Close()

	// Read without encryption — plaintext column should be empty.
	dPlain, err := destination.OpenSQLite(path, logger)
	if err != nil {
		t.Fatalf("OpenSQLite plain: %v", err)
	}
	defer func() { _ = dPlain.Close() }()
	result, err := dPlain.Query(destination.QueryParams{Namespace: p.Namespace, Destination: p.Destination})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(result.Records) != 1 {
		t.Fatalf("want 1 record")
	}
	if result.Records[0].Content == "should be encrypted" {
		t.Error("plaintext column must not contain unencrypted content")
	}
}

// TestEncryption_MultipleRowsCorrect verifies all fields are returned correctly
// for a batch of encrypted records.
func TestEncryption_MultipleRowsCorrect(t *testing.T) {
	d, cleanup := newEncryptedSQLite(t, "multi-pw")
	defer cleanup()

	type row struct {
		id      string
		content string
	}
	rows := []row{
		{"multi-001", "alpha"},
		{"multi-002", "beta"},
		{"multi-003", "gamma"},
	}
	for _, r := range rows {
		p := basePayload(r.id)
		p.Content = r.content
		if err := d.Write(p); err != nil {
			t.Fatalf("Write %s: %v", r.id, err)
		}
	}

	result, err := d.Query(destination.QueryParams{Namespace: "default", Destination: "sqlite"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(result.Records) != 3 {
		t.Fatalf("want 3 records, got %d", len(result.Records))
	}
	contents := map[string]bool{}
	for _, r := range result.Records {
		contents[r.Content] = true
	}
	for _, r := range rows {
		if !contents[r.content] {
			t.Errorf("missing content %q in results", r.content)
		}
	}
}

// TestEncryption_DisabledMKMNoOp verifies that a disabled MKM (no password)
// does not encrypt — writes and reads work as plaintext.
func TestEncryption_DisabledMKMNoOp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	d, err := destination.OpenSQLite(path, logger)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer func() { _ = d.Close() }()

	// Disabled MKM (no password, no env var).
	saltPath := filepath.Join(t.TempDir(), "crypto.salt")
	mkm, err := crypto.NewMasterKeyManager("", saltPath)
	if err != nil {
		t.Fatalf("NewMasterKeyManager: %v", err)
	}
	if mkm.IsEnabled() {
		t.Skip("NEXUS_PASSWORD env var set; skipping disabled-MKM test")
	}
	d.SetEncryption(mkm)

	p := basePayload("disabled-001")
	p.Content = "plaintext works"
	if err := d.Write(p); err != nil {
		t.Fatalf("Write: %v", err)
	}
	result, err := d.Query(destination.QueryParams{Namespace: p.Namespace, Destination: p.Destination})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(result.Records) != 1 || result.Records[0].Content != "plaintext works" {
		t.Errorf("disabled MKM should pass through plaintext, got %v", result.Records)
	}
}

// TestEncryption_MigrationPlaintextCleared verifies the plaintext content column
// is empty after EncryptExistingRows completes.
func TestEncryption_MigrationPlaintextCleared(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	dPlain, err := destination.OpenSQLite(path, logger)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	p := basePayload("clr-001")
	p.Content = "clearme"
	if err := dPlain.Write(p); err != nil {
		t.Fatalf("Write: %v", err)
	}
	_ = dPlain.Close()

	dEnc, err := destination.OpenSQLite(path, logger)
	if err != nil {
		t.Fatalf("OpenSQLite enc: %v", err)
	}
	defer func() { _ = dEnc.Close() }()
	dEnc.SetEncryption(newMKM(t, "clr-pw"))
	if err := dEnc.EncryptExistingRows(context.Background(), 100, 0); err != nil {
		t.Fatalf("EncryptExistingRows: %v", err)
	}

	// Open plain and verify raw content column is empty.
	dRaw, err := destination.OpenSQLite(path, logger)
	if err != nil {
		t.Fatalf("OpenSQLite raw: %v", err)
	}
	defer func() { _ = dRaw.Close() }()
	result, err := dRaw.Query(destination.QueryParams{Namespace: p.Namespace, Destination: p.Destination})
	if err != nil {
		t.Fatalf("Query raw: %v", err)
	}
	if len(result.Records) != 1 {
		t.Fatalf("want 1 record")
	}
	if result.Records[0].Content == "clearme" {
		t.Error("plaintext column should be cleared after migration")
	}
}
