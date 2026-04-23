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

package daemon

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestAcquireLock_Success(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	fl, err := AcquireLock(dir)
	if err != nil {
		t.Fatalf("AcquireLock failed: %v", err)
	}
	defer fl.Unlock()

	if fl == nil {
		t.Fatal("expected non-nil flock")
	}
}

func TestAcquireLock_SecondAttemptFails(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	fl1, err := AcquireLock(dir)
	if err != nil {
		t.Fatalf("first AcquireLock failed: %v", err)
	}
	defer fl1.Unlock()

	_, err2 := AcquireLock(dir)
	if err2 == nil {
		t.Fatal("expected error on second lock attempt")
	}
	if !strings.Contains(err2.Error(), "already running") {
		t.Errorf("expected 'already running' in error, got: %v", err2)
	}
}

func TestAcquireLock_ReleaseAndReacquire(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	fl1, err := AcquireLock(dir)
	if err != nil {
		t.Fatalf("first AcquireLock failed: %v", err)
	}
	if err := fl1.Unlock(); err != nil {
		t.Fatalf("Unlock failed: %v", err)
	}

	fl2, err := AcquireLock(dir)
	if err != nil {
		t.Fatalf("second AcquireLock after release failed: %v", err)
	}
	defer fl2.Unlock()
}

func TestAcquireLock_CorrectPath(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	fl, err := AcquireLock(dir)
	if err != nil {
		t.Fatalf("AcquireLock failed: %v", err)
	}
	defer fl.Unlock()

	expected := filepath.Join(dir, "nexus.lock")
	if fl.Path() != expected {
		t.Errorf("lock path = %q, want %q", fl.Path(), expected)
	}
}
