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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

// TestCopyFileWithHash verifies that copyFileWithHash produces a correct
// SHA256 digest and an identical copy.
func TestCopyFileWithHash(t *testing.T) {
	t.Helper()

	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")

	content := []byte("hello, bubblefish backup")
	if err := os.WriteFile(src, content, 0600); err != nil {
		t.Fatalf("write src: %v", err)
	}

	got, err := copyFileWithHash(src, dst)
	if err != nil {
		t.Fatalf("copyFileWithHash: %v", err)
	}

	// Verify digest.
	h := sha256.Sum256(content)
	want := hex.EncodeToString(h[:])
	if got != want {
		t.Errorf("SHA256 mismatch: got %s, want %s", got, want)
	}

	// Verify content.
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(data) != string(content) {
		t.Errorf("content mismatch: got %q, want %q", data, content)
	}
}

// TestSHA256File verifies sha256File returns the correct digest.
func TestSHA256File(t *testing.T) {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "test.bin")
	content := []byte("checksum test data")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := sha256File(path)
	if err != nil {
		t.Fatalf("sha256File: %v", err)
	}

	h := sha256.Sum256(content)
	want := hex.EncodeToString(h[:])
	if got != want {
		t.Errorf("sha256 mismatch: got %s, want %s", got, want)
	}
}

// TestSHA256FileMissing verifies sha256File returns an error for missing files.
func TestSHA256FileMissing(t *testing.T) {
	t.Helper()

	_, err := sha256File(filepath.Join(t.TempDir(), "nonexistent"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

// TestCopyFile verifies that copyFile creates an identical copy with 0600 perms.
func TestCopyFile(t *testing.T) {
	t.Helper()

	dir := t.TempDir()
	src := filepath.Join(dir, "src.dat")
	dst := filepath.Join(dir, "dst.dat")

	content := []byte("copy file test")
	if err := os.WriteFile(src, content, 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile: %v", err)
	}

	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(data) != string(content) {
		t.Errorf("content mismatch: got %q, want %q", data, content)
	}
}

// TestBackupFileAndManifest verifies backupFile copies a file and records
// it in the manifest with correct SHA256 and category.
func TestBackupFileAndManifest(t *testing.T) {
	t.Helper()

	baseDir := t.TempDir()
	destDir := t.TempDir()

	// Create a source file.
	srcFile := filepath.Join(baseDir, "daemon.toml")
	content := []byte("[daemon]\nport = 8080\n")
	if err := os.WriteFile(srcFile, content, 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	var manifest Manifest
	if err := backupFile(destDir, baseDir, srcFile, "config", &manifest, discardLogger()); err != nil {
		t.Fatalf("backupFile: %v", err)
	}

	if len(manifest.Files) != 1 {
		t.Fatalf("expected 1 manifest entry, got %d", len(manifest.Files))
	}

	mf := manifest.Files[0]
	if mf.RelPath != "daemon.toml" {
		t.Errorf("relPath: got %q, want %q", mf.RelPath, "daemon.toml")
	}
	if mf.Category != "config" {
		t.Errorf("category: got %q, want %q", mf.Category, "config")
	}

	h := sha256.Sum256(content)
	want := hex.EncodeToString(h[:])
	if mf.SHA256 != want {
		t.Errorf("sha256: got %s, want %s", mf.SHA256, want)
	}

	// Verify copied file exists in destDir.
	copied, err := os.ReadFile(filepath.Join(destDir, "daemon.toml"))
	if err != nil {
		t.Fatalf("read copied: %v", err)
	}
	if string(copied) != string(content) {
		t.Errorf("copied content mismatch")
	}
}

// TestBackupFileMissing verifies backupFile silently skips missing files.
func TestBackupFileMissing(t *testing.T) {
	t.Helper()

	var manifest Manifest
	err := backupFile(t.TempDir(), t.TempDir(), "/nonexistent/file.toml", "config", &manifest, discardLogger())
	if err != nil {
		t.Fatalf("expected nil for missing file, got: %v", err)
	}
	if len(manifest.Files) != 0 {
		t.Errorf("expected 0 files in manifest, got %d", len(manifest.Files))
	}
}

// TestBackupDir verifies backupDir copies all files from a directory.
func TestBackupDir(t *testing.T) {
	t.Helper()

	baseDir := t.TempDir()
	destDir := t.TempDir()

	// Create source subdirectory with two files.
	srcDir := filepath.Join(baseDir, "sources")
	if err := os.MkdirAll(srcDir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, name := range []string{"default.toml", "openwebui.toml"} {
		if err := os.WriteFile(filepath.Join(srcDir, name), []byte("name = \""+name+"\"\n"), 0600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	var manifest Manifest
	if err := backupDir(destDir, baseDir, srcDir, "config", &manifest, discardLogger()); err != nil {
		t.Fatalf("backupDir: %v", err)
	}

	if len(manifest.Files) != 2 {
		t.Fatalf("expected 2 manifest entries, got %d", len(manifest.Files))
	}

	for _, mf := range manifest.Files {
		if mf.Category != "config" {
			t.Errorf("category: got %q, want %q", mf.Category, "config")
		}
		copied := filepath.Join(destDir, mf.RelPath)
		if _, err := os.Stat(copied); err != nil {
			t.Errorf("copied file missing: %s", copied)
		}
	}
}

// TestBackupDirMissing verifies backupDir silently skips missing directories.
func TestBackupDirMissing(t *testing.T) {
	t.Helper()

	var manifest Manifest
	err := backupDir(t.TempDir(), t.TempDir(), "/nonexistent/dir", "wal", &manifest, discardLogger())
	if err != nil {
		t.Fatalf("expected nil for missing dir, got: %v", err)
	}
}

// TestRestoreVerifiesChecksums verifies that restore detects corrupted files.
func TestRestoreVerifiesChecksums(t *testing.T) {
	t.Helper()

	backupDir := t.TempDir()
	targetDir := t.TempDir()

	// Create a file in the backup.
	content := []byte("original content")
	if err := os.WriteFile(filepath.Join(backupDir, "daemon.toml"), content, 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Write manifest with correct checksum.
	h := sha256.Sum256(content)
	manifest := Manifest{
		Version:   "0.1.0",
		ConfigDir: targetDir,
		Files: []ManifestFile{
			{
				RelPath:  "daemon.toml",
				SHA256:   hex.EncodeToString(h[:]),
				Size:     int64(len(content)),
				Category: "config",
			},
		},
	}

	// First: restore should succeed with correct checksum.
	writeManifest(t, backupDir, manifest)

	if err := Restore(RestoreOptions{From: backupDir, Logger: discardLogger()}); err != nil {
		t.Fatalf("restore with correct checksum failed: %v", err)
	}

	// Verify the file was restored.
	restored, err := os.ReadFile(filepath.Join(targetDir, "daemon.toml"))
	if err != nil {
		t.Fatalf("read restored: %v", err)
	}
	if string(restored) != string(content) {
		t.Errorf("restored content mismatch")
	}

	// Second: corrupt the backup file and try again.
	if err := os.WriteFile(filepath.Join(backupDir, "daemon.toml"), []byte("CORRUPTED"), 0600); err != nil {
		t.Fatalf("corrupt: %v", err)
	}

	err = Restore(RestoreOptions{From: backupDir, Force: true, Logger: discardLogger()})
	if err == nil {
		t.Fatal("expected error for corrupted file")
	}
	if got := err.Error(); !contains(got, "checksum mismatch") {
		t.Errorf("expected checksum mismatch error, got: %v", err)
	}
}

// TestRestoreRefusesOverwrite verifies restore fails without --force when
// target files already exist.
func TestRestoreRefusesOverwrite(t *testing.T) {
	t.Helper()

	backupDir := t.TempDir()
	targetDir := t.TempDir()

	content := []byte("config data")
	if err := os.WriteFile(filepath.Join(backupDir, "daemon.toml"), content, 0600); err != nil {
		t.Fatalf("write backup: %v", err)
	}

	h := sha256.Sum256(content)
	manifest := Manifest{
		Version:   "0.1.0",
		ConfigDir: targetDir,
		Files: []ManifestFile{
			{
				RelPath:  "daemon.toml",
				SHA256:   hex.EncodeToString(h[:]),
				Size:     int64(len(content)),
				Category: "config",
			},
		},
	}
	writeManifest(t, backupDir, manifest)

	// Create existing file at target.
	if err := os.WriteFile(filepath.Join(targetDir, "daemon.toml"), []byte("existing"), 0600); err != nil {
		t.Fatalf("write existing: %v", err)
	}

	// Without --force: should fail.
	err := Restore(RestoreOptions{From: backupDir, Force: false, Logger: discardLogger()})
	if err == nil {
		t.Fatal("expected error without --force")
	}
	if got := err.Error(); !contains(got, "file exists") {
		t.Errorf("expected 'file exists' error, got: %v", err)
	}

	// With --force: should succeed.
	err = Restore(RestoreOptions{From: backupDir, Force: true, Logger: discardLogger()})
	if err != nil {
		t.Fatalf("restore with --force failed: %v", err)
	}

	restored, _ := os.ReadFile(filepath.Join(targetDir, "daemon.toml"))
	if string(restored) != string(content) {
		t.Errorf("restored content mismatch after --force")
	}
}

// TestRestoreMissingManifest verifies restore fails when manifest.json is missing.
func TestRestoreMissingManifest(t *testing.T) {
	t.Helper()

	err := Restore(RestoreOptions{From: t.TempDir(), Logger: discardLogger()})
	if err == nil {
		t.Fatal("expected error for missing manifest")
	}
}

// TestExpandPath verifies ~ expansion and clean paths.
func TestExpandPath(t *testing.T) {
	t.Helper()

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "absolute", input: "/tmp/test"},
		{name: "tilde", input: "~/.bubblefish/Nexus"},
		{name: "relative", input: "relative/path"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := expandPath(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("expandPath(%q): err=%v, wantErr=%v", tt.input, err, tt.wantErr)
			}
			if err == nil && result == "" {
				t.Errorf("expandPath(%q): empty result", tt.input)
			}
		})
	}
}

// TestRoundTrip verifies a full backup + restore cycle with multiple files.
func TestRoundTrip(t *testing.T) {
	t.Helper()

	baseDir := t.TempDir()
	backupDest := t.TempDir()
	restoreTarget := t.TempDir()

	// Create a tree of files simulating a config dir.
	dirs := map[string][]struct {
		name    string
		content string
	}{
		".": {
			{"daemon.toml", "[daemon]\nport = 8080\n"},
		},
		"sources": {
			{"default.toml", "[source]\nname = \"default\"\n"},
		},
		"compiled": {
			{"policies.json", `{"rules":[]}`},
		},
		"wal": {
			{"wal-0001.jsonl", `{"payload_id":"p1"}` + "\n"},
			{"wal-0002.jsonl", `{"payload_id":"p2"}` + "\n"},
		},
	}

	for dir, files := range dirs {
		d := filepath.Join(baseDir, dir)
		if err := os.MkdirAll(d, 0700); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
		for _, f := range files {
			if err := os.WriteFile(filepath.Join(d, f.name), []byte(f.content), 0600); err != nil {
				t.Fatalf("write %s/%s: %v", dir, f.name, err)
			}
		}
	}

	// Build a manifest manually (simulating Create).
	var manifest Manifest
	manifest.Version = "0.1.0"
	manifest.ConfigDir = restoreTarget

	for dir, files := range dirs {
		for _, f := range files {
			srcPath := filepath.Join(baseDir, dir, f.name)
			relPath := filepath.Join(dir, f.name)
			if dir == "." {
				relPath = f.name
			}

			destPath := filepath.Join(backupDest, relPath)
			if err := os.MkdirAll(filepath.Dir(destPath), 0700); err != nil {
				t.Fatalf("mkdir: %v", err)
			}

			checksum, err := copyFileWithHash(srcPath, destPath)
			if err != nil {
				t.Fatalf("copy %s: %v", relPath, err)
			}

			fi, _ := os.Stat(srcPath)
			manifest.Files = append(manifest.Files, ManifestFile{
				RelPath:  relPath,
				SHA256:   checksum,
				Size:     fi.Size(),
				Category: "test",
			})
		}
	}

	writeManifest(t, backupDest, manifest)

	// Restore to a clean target.
	if err := Restore(RestoreOptions{From: backupDest, Logger: discardLogger()}); err != nil {
		t.Fatalf("restore: %v", err)
	}

	// Verify all files restored.
	for _, mf := range manifest.Files {
		restored := filepath.Join(restoreTarget, mf.RelPath)
		if _, err := os.Stat(restored); err != nil {
			t.Errorf("missing restored file: %s", restored)
		}

		checksum, err := sha256File(restored)
		if err != nil {
			t.Fatalf("hash %s: %v", restored, err)
		}
		if checksum != mf.SHA256 {
			t.Errorf("checksum mismatch for %s after restore", mf.RelPath)
		}
	}
}

// --- helpers ---

func writeManifest(t *testing.T, dir string, m Manifest) {
	t.Helper()
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// --- Verify tests (V.5) ---

func TestVerifyPassingBackup(t *testing.T) {
	dir := t.TempDir()

	// Create files and manifest with correct checksums.
	writeTestFile(t, dir, "config.toml", "key = true\n")
	writeTestFile(t, dir, "wal/segment.jsonl", `{"id":"1"}`+"\n")

	hash1 := mustSHA256(t, filepath.Join(dir, "config.toml"))
	hash2 := mustSHA256(t, filepath.Join(dir, "wal/segment.jsonl"))
	size1 := mustFileSize(t, filepath.Join(dir, "config.toml"))
	size2 := mustFileSize(t, filepath.Join(dir, "wal/segment.jsonl"))

	m := Manifest{
		Version: "0.1.0",
		Files: []ManifestFile{
			{RelPath: "config.toml", SHA256: hash1, Size: size1, Category: "config"},
			{RelPath: "wal/segment.jsonl", SHA256: hash2, Size: size2, Category: "wal"},
		},
	}
	writeManifest(t, dir, m)

	result, err := Verify(dir, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Pass {
		t.Errorf("expected pass, got failures: %+v", result.Failures)
	}
	if result.TotalFiles != 2 || result.PassedFiles != 2 {
		t.Errorf("expected 2/2 passed, got %d/%d", result.PassedFiles, result.TotalFiles)
	}
}

func TestVerifyMissingFile(t *testing.T) {
	dir := t.TempDir()

	writeTestFile(t, dir, "exists.toml", "data")

	m := Manifest{
		Version: "0.1.0",
		Files: []ManifestFile{
			{RelPath: "exists.toml", SHA256: mustSHA256(t, filepath.Join(dir, "exists.toml")), Size: 4, Category: "config"},
			{RelPath: "gone.toml", SHA256: "deadbeef", Size: 10, Category: "config"},
		},
	}
	writeManifest(t, dir, m)

	result, err := Verify(dir, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	if result.Pass {
		t.Error("expected failure for missing file")
	}
	if result.MissingFiles != 1 {
		t.Errorf("MissingFiles = %d, want 1", result.MissingFiles)
	}
	if len(result.Failures) != 1 || result.Failures[0].Reason != "missing" {
		t.Errorf("expected missing failure, got %+v", result.Failures)
	}
}

func TestVerifyChecksumMismatch(t *testing.T) {
	dir := t.TempDir()

	writeTestFile(t, dir, "data.bin", "original content")

	m := Manifest{
		Version: "0.1.0",
		Files: []ManifestFile{
			{RelPath: "data.bin", SHA256: "0000000000000000000000000000000000000000000000000000000000000000", Size: mustFileSize(t, filepath.Join(dir, "data.bin")), Category: "config"},
		},
	}
	writeManifest(t, dir, m)

	result, err := Verify(dir, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	if result.Pass {
		t.Error("expected failure for checksum mismatch")
	}
	if len(result.Failures) != 1 || result.Failures[0].Reason != "checksum_mismatch" {
		t.Errorf("expected checksum_mismatch failure, got %+v", result.Failures)
	}
}

func TestVerifyMissingManifest(t *testing.T) {
	dir := t.TempDir()
	_, err := Verify(dir, discardLogger())
	if err == nil {
		t.Error("expected error for missing manifest")
	}
}

func writeTestFile(t *testing.T, baseDir, relPath, content string) {
	t.Helper()
	full := filepath.Join(baseDir, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func mustSHA256(t *testing.T, path string) string {
	t.Helper()
	h, err := sha256File(path)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func mustFileSize(t *testing.T, path string) int64 {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Size()
}
