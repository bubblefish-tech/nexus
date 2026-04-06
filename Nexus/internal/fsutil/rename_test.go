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

package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRobustRename_Success(t *testing.T) {
	t.Helper()

	dir := t.TempDir()
	src := filepath.Join(dir, "source.txt")
	dst := filepath.Join(dir, "dest.txt")

	if err := os.WriteFile(src, []byte("hello"), 0600); err != nil {
		t.Fatalf("write source: %v", err)
	}

	if err := RobustRename(src, dst); err != nil {
		t.Fatalf("RobustRename: %v", err)
	}

	// Source should no longer exist.
	if _, err := os.Stat(src); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("source still exists after rename")
	}

	// Destination should have the content.
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("dest content = %q, want %q", data, "hello")
	}
}

func TestRobustRename_OverwriteExisting(t *testing.T) {
	t.Helper()

	dir := t.TempDir()
	src := filepath.Join(dir, "new.txt")
	dst := filepath.Join(dir, "existing.txt")

	if err := os.WriteFile(dst, []byte("old"), 0600); err != nil {
		t.Fatalf("write existing: %v", err)
	}
	if err := os.WriteFile(src, []byte("new"), 0600); err != nil {
		t.Fatalf("write source: %v", err)
	}

	if err := RobustRename(src, dst); err != nil {
		t.Fatalf("RobustRename: %v", err)
	}

	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(data) != "new" {
		t.Errorf("dest content = %q, want %q", data, "new")
	}
}

func TestRobustRename_SourceNotFound(t *testing.T) {
	t.Helper()

	dir := t.TempDir()
	src := filepath.Join(dir, "nonexistent.txt")
	dst := filepath.Join(dir, "dest.txt")

	err := RobustRename(src, dst)
	if err == nil {
		t.Fatal("expected error for nonexistent source")
	}

	// Should NOT retry on file-not-found (not a transient lock error).
	// The error should propagate immediately.
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected os.ErrNotExist, got: %v", err)
	}
}

func TestIsRetryableRenameErr_NilError(t *testing.T) {
	t.Helper()

	// Should not panic on nil-like errors.
	if isRetryableRenameErr(errors.New("generic error")) {
		t.Error("generic error should not be retryable")
	}
}

func TestIsRetryableRenameErr_NonLinkError(t *testing.T) {
	t.Helper()

	if isRetryableRenameErr(os.ErrNotExist) {
		t.Error("ErrNotExist should not be retryable")
	}
}
