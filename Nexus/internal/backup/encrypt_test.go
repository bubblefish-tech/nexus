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

package backup

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bubblefish-tech/nexus/internal/crypto"
)

// newTestMKM creates a MasterKeyManager with a fixed password and temp salt.
func newTestMKM(t *testing.T, password string) *crypto.MasterKeyManager {
	t.Helper()
	saltPath := filepath.Join(t.TempDir(), "crypto.salt")
	mgr, err := crypto.NewMasterKeyManager(password, saltPath)
	if err != nil {
		t.Fatalf("NewMasterKeyManager: %v", err)
	}
	return mgr
}

func discardLogger2() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// buildSourceDir creates a small directory tree for testing.
func buildSourceDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"daemon.toml":          "[daemon]\nport = 8080\n",
		"sources/default.toml": "name = \"default\"\n",
		"compiled/policy.json": `{"rules":[]}`,
		"wal/seg-0001.jsonl":   `{"id":"p1"}` + "\n",
	}
	for rel, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0600); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return dir
}

// TestEncryptedRoundTrip verifies export + import produces identical files.
func TestEncryptedRoundTrip(t *testing.T) {
	t.Helper()
	mkm := newTestMKM(t, "round-trip-password")
	srcDir := buildSourceDir(t)
	outFile := filepath.Join(t.TempDir(), "backup.bfbk")
	destDir := t.TempDir()
	logger := discardLogger2()

	if err := ExportEncrypted(mkm, ExportEncryptedOptions{
		SourceDir:  srcDir,
		OutputPath: outFile,
		Logger:     logger,
	}); err != nil {
		t.Fatalf("ExportEncrypted: %v", err)
	}

	if err := ImportEncrypted(mkm, ImportEncryptedOptions{
		InputPath: outFile,
		DestDir:   destDir,
		Logger:    logger,
	}); err != nil {
		t.Fatalf("ImportEncrypted: %v", err)
	}

	// Verify all source files exist in destDir with identical content.
	for _, rel := range []string{
		"daemon.toml",
		"sources/default.toml",
		"compiled/policy.json",
		"wal/seg-0001.jsonl",
	} {
		srcContent, err := os.ReadFile(filepath.Join(srcDir, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read src %s: %v", rel, err)
		}
		dstContent, err := os.ReadFile(filepath.Join(destDir, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read dst %s: %v", rel, err)
		}
		if string(srcContent) != string(dstContent) {
			t.Errorf("content mismatch for %s", rel)
		}
	}
}

// TestEncryptedFileHasMagic verifies the output file starts with "BFBK".
func TestEncryptedFileHasMagic(t *testing.T) {
	t.Helper()
	mkm := newTestMKM(t, "magic-test-pw")
	srcDir := buildSourceDir(t)
	outFile := filepath.Join(t.TempDir(), "backup.bfbk")

	if err := ExportEncrypted(mkm, ExportEncryptedOptions{
		SourceDir:  srcDir,
		OutputPath: outFile,
		Logger:     discardLogger2(),
	}); err != nil {
		t.Fatalf("ExportEncrypted: %v", err)
	}

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if len(data) < 8 {
		t.Fatalf("output too short: %d bytes", len(data))
	}
	if string(data[:4]) != "BFBK" {
		t.Errorf("magic: got %q, want %q", string(data[:4]), "BFBK")
	}
}

// TestEncryptedWrongKeyFails verifies decryption fails with a different password.
func TestEncryptedWrongKeyFails(t *testing.T) {
	t.Helper()
	mkmOK := newTestMKM(t, "correct-password")
	mkmBad := newTestMKM(t, "wrong-password")
	srcDir := buildSourceDir(t)
	outFile := filepath.Join(t.TempDir(), "backup.bfbk")

	if err := ExportEncrypted(mkmOK, ExportEncryptedOptions{
		SourceDir:  srcDir,
		OutputPath: outFile,
		Logger:     discardLogger2(),
	}); err != nil {
		t.Fatalf("ExportEncrypted: %v", err)
	}

	err := ImportEncrypted(mkmBad, ImportEncryptedOptions{
		InputPath: outFile,
		DestDir:   t.TempDir(),
		Logger:    discardLogger2(),
	})
	if err == nil {
		t.Fatal("expected error with wrong key, got nil")
	}
}

// TestEncryptedDisabledMKM verifies export and import fail when no password set.
func TestEncryptedDisabledMKM(t *testing.T) {
	t.Helper()
	t.Setenv(crypto.EnvPassword, "")
	saltPath := filepath.Join(t.TempDir(), "crypto.salt")
	mgr, err := crypto.NewMasterKeyManager("", saltPath)
	if err != nil {
		t.Fatalf("NewMasterKeyManager: %v", err)
	}

	srcDir := buildSourceDir(t)
	outFile := filepath.Join(t.TempDir(), "backup.bfbk")

	exportErr := ExportEncrypted(mgr, ExportEncryptedOptions{
		SourceDir:  srcDir,
		OutputPath: outFile,
		Logger:     discardLogger2(),
	})
	if exportErr == nil {
		t.Error("ExportEncrypted: expected error for disabled MKM, got nil")
	}
	if !strings.Contains(exportErr.Error(), "encryption not configured") {
		t.Errorf("ExportEncrypted: unexpected error: %v", exportErr)
	}

	// Write a dummy file so ImportEncrypted has something to open.
	dummyFile := filepath.Join(t.TempDir(), "dummy.bfbk")
	if err := os.WriteFile(dummyFile, []byte("BFBK\x00\x00\x00\x01"), 0600); err != nil {
		t.Fatalf("write dummy: %v", err)
	}
	importErr := ImportEncrypted(mgr, ImportEncryptedOptions{
		InputPath: dummyFile,
		DestDir:   t.TempDir(),
		Logger:    discardLogger2(),
	})
	if importErr == nil {
		t.Error("ImportEncrypted: expected error for disabled MKM, got nil")
	}
}

// TestEncryptedBadMagic verifies ImportEncrypted rejects files with wrong magic.
func TestEncryptedBadMagic(t *testing.T) {
	t.Helper()
	mkm := newTestMKM(t, "bad-magic-pw")
	badFile := filepath.Join(t.TempDir(), "bad.bfbk")
	if err := os.WriteFile(badFile, []byte("XXXX\x00\x00\x00\x01somedata"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	err := ImportEncrypted(mkm, ImportEncryptedOptions{
		InputPath: badFile,
		DestDir:   t.TempDir(),
		Logger:    discardLogger2(),
	})
	if err == nil {
		t.Fatal("expected error for bad magic, got nil")
	}
	if !strings.Contains(err.Error(), "invalid magic") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestEncryptedBadVersion verifies ImportEncrypted rejects unsupported versions.
func TestEncryptedBadVersion(t *testing.T) {
	t.Helper()
	mkm := newTestMKM(t, "bad-version-pw")
	badFile := filepath.Join(t.TempDir(), "bad.bfbk")
	// "BFBK" + version=99
	if err := os.WriteFile(badFile, []byte("BFBK\x00\x00\x00\x63somedata"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	err := ImportEncrypted(mkm, ImportEncryptedOptions{
		InputPath: badFile,
		DestDir:   t.TempDir(),
		Logger:    discardLogger2(),
	})
	if err == nil {
		t.Fatal("expected error for bad version, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported version") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestEncryptedTruncatedFile verifies ImportEncrypted rejects truncated files.
func TestEncryptedTruncatedFile(t *testing.T) {
	t.Helper()
	mkm := newTestMKM(t, "truncated-pw")
	truncFile := filepath.Join(t.TempDir(), "trunc.bfbk")
	// Only 6 bytes — too short for the 8-byte header.
	if err := os.WriteFile(truncFile, []byte("BFBK\x00\x00"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	err := ImportEncrypted(mkm, ImportEncryptedOptions{
		InputPath: truncFile,
		DestDir:   t.TempDir(),
		Logger:    discardLogger2(),
	})
	if err == nil {
		t.Fatal("expected error for truncated file, got nil")
	}
}

// TestEncryptedNoOverwrite verifies ImportEncrypted refuses to overwrite
// existing files without Force.
func TestEncryptedNoOverwrite(t *testing.T) {
	t.Helper()
	mkm := newTestMKM(t, "no-overwrite-pw")
	srcDir := buildSourceDir(t)
	outFile := filepath.Join(t.TempDir(), "backup.bfbk")
	destDir := t.TempDir()
	logger := discardLogger2()

	if err := ExportEncrypted(mkm, ExportEncryptedOptions{
		SourceDir:  srcDir,
		OutputPath: outFile,
		Logger:     logger,
	}); err != nil {
		t.Fatalf("ExportEncrypted: %v", err)
	}

	// Pre-create a file that would be overwritten.
	existing := filepath.Join(destDir, "daemon.toml")
	if err := os.WriteFile(existing, []byte("existing"), 0600); err != nil {
		t.Fatalf("write existing: %v", err)
	}

	err := ImportEncrypted(mkm, ImportEncryptedOptions{
		InputPath: outFile,
		DestDir:   destDir,
		Force:     false,
		Logger:    logger,
	})
	if err == nil {
		t.Fatal("expected error without --force, got nil")
	}
	if !strings.Contains(err.Error(), "file exists") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestEncryptedForceOverwrite verifies ImportEncrypted overwrites with Force=true.
func TestEncryptedForceOverwrite(t *testing.T) {
	t.Helper()
	mkm := newTestMKM(t, "force-overwrite-pw")
	srcDir := buildSourceDir(t)
	outFile := filepath.Join(t.TempDir(), "backup.bfbk")
	destDir := t.TempDir()
	logger := discardLogger2()

	if err := ExportEncrypted(mkm, ExportEncryptedOptions{
		SourceDir:  srcDir,
		OutputPath: outFile,
		Logger:     logger,
	}); err != nil {
		t.Fatalf("ExportEncrypted: %v", err)
	}

	// Pre-create a file that will be overwritten.
	existing := filepath.Join(destDir, "daemon.toml")
	if err := os.WriteFile(existing, []byte("old-content"), 0600); err != nil {
		t.Fatalf("write existing: %v", err)
	}

	if err := ImportEncrypted(mkm, ImportEncryptedOptions{
		InputPath: outFile,
		DestDir:   destDir,
		Force:     true,
		Logger:    logger,
	}); err != nil {
		t.Fatalf("ImportEncrypted with force: %v", err)
	}

	// Verify the file was overwritten.
	content, err := os.ReadFile(existing)
	if err != nil {
		t.Fatalf("read restored: %v", err)
	}
	srcContent, _ := os.ReadFile(filepath.Join(srcDir, "daemon.toml"))
	if string(content) != string(srcContent) {
		t.Errorf("content not restored: got %q, want %q", content, srcContent)
	}
}

// TestEncryptedFilePermissions verifies the output file is created with 0600.
func TestEncryptedFilePermissions(t *testing.T) {
	if os.Getenv("OS") == "Windows_NT" {
		t.Skip("Windows does not enforce Unix permission bits")
	}
	t.Helper()
	mkm := newTestMKM(t, "perms-pw")
	srcDir := buildSourceDir(t)
	outFile := filepath.Join(t.TempDir(), "backup.bfbk")

	if err := ExportEncrypted(mkm, ExportEncryptedOptions{
		SourceDir:  srcDir,
		OutputPath: outFile,
		Logger:     discardLogger2(),
	}); err != nil {
		t.Fatalf("ExportEncrypted: %v", err)
	}

	info, err := os.Stat(outFile)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0600 {
		t.Errorf("output file mode: got %o, want 0600", mode)
	}
}

// TestEncryptedEmptySourceDir verifies an empty directory exports and imports cleanly.
func TestEncryptedEmptySourceDir(t *testing.T) {
	t.Helper()
	mkm := newTestMKM(t, "empty-dir-pw")
	srcDir := t.TempDir()
	outFile := filepath.Join(t.TempDir(), "backup.bfbk")
	logger := discardLogger2()

	if err := ExportEncrypted(mkm, ExportEncryptedOptions{
		SourceDir:  srcDir,
		OutputPath: outFile,
		Logger:     logger,
	}); err != nil {
		t.Fatalf("ExportEncrypted empty dir: %v", err)
	}

	if err := ImportEncrypted(mkm, ImportEncryptedOptions{
		InputPath: outFile,
		DestDir:   t.TempDir(),
		Logger:    logger,
	}); err != nil {
		t.Fatalf("ImportEncrypted empty dir: %v", err)
	}
}

// TestEncryptedCorruptedCiphertext verifies authentication failure on bit-flip.
func TestEncryptedCorruptedCiphertext(t *testing.T) {
	t.Helper()
	mkm := newTestMKM(t, "corrupt-pw")
	srcDir := buildSourceDir(t)
	outFile := filepath.Join(t.TempDir(), "backup.bfbk")

	if err := ExportEncrypted(mkm, ExportEncryptedOptions{
		SourceDir:  srcDir,
		OutputPath: outFile,
		Logger:     discardLogger2(),
	}); err != nil {
		t.Fatalf("ExportEncrypted: %v", err)
	}

	// Flip a byte in the middle of the ciphertext.
	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	mid := headerSize + len(data[headerSize:])/2
	data[mid] ^= 0xFF
	if err := os.WriteFile(outFile, data, 0600); err != nil {
		t.Fatalf("write corrupted: %v", err)
	}

	err = ImportEncrypted(mkm, ImportEncryptedOptions{
		InputPath: outFile,
		DestDir:   t.TempDir(),
		Logger:    discardLogger2(),
	})
	if err == nil {
		t.Fatal("expected error for corrupted ciphertext, got nil")
	}
}
