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

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckCloudSync_CleanDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	r := checkCloudSync(dir)
	// On machines with OneDrive installed, the temp dir may be under a cloud-synced parent.
	if r.Status != "OK" && r.Status != "WARN" {
		t.Errorf("expected OK or WARN, got %s: %s", r.Status, r.Message)
	}
}

func TestCheckCloudSync_DropboxMarker(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	// Create a .dropbox marker in the parent.
	os.Mkdir(filepath.Join(dir, ".dropbox"), 0700)
	subdir := filepath.Join(dir, "nexus")
	os.Mkdir(subdir, 0700)

	r := checkCloudSync(subdir)
	if r.Status != "WARN" {
		t.Errorf("expected WARN for Dropbox marker, got %s: %s", r.Status, r.Message)
	}
}

func TestCheckDiskSpace_OK(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	r := checkDiskSpace(dir)
	// Most test machines have >1 GiB free.
	if r.Status == "CRITICAL" {
		t.Skipf("skipping: test machine has very low disk space: %s", r.Message)
	}
}

func TestCheckPorts_FreePort(t *testing.T) {
	t.Helper()
	// Port 0 should always be "available" in the sense that nothing binds to it.
	// Use a high random port that's likely free.
	results := checkPorts(59999)
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}
	if results[0].Status != "OK" {
		t.Errorf("expected OK for likely-free port, got %s", results[0].Status)
	}
}

func TestCheckPermissions_OK(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	keysDir := filepath.Join(dir, "keys")
	os.MkdirAll(keysDir, 0700)

	r := checkPermissions(dir)
	if r.Status == "CRITICAL" {
		t.Errorf("expected non-CRITICAL for 0700 keys dir, got: %s", r.Message)
	}
}

func TestCheckPermissions_MissingKeysDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	r := checkPermissions(dir)
	if r.Status == "CRITICAL" {
		t.Errorf("expected non-CRITICAL for missing keys dir, got: %s", r.Message)
	}
}

func TestRunAllChecks_ReturnsResults(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "keys"), 0700)
	results := RunAllChecks(dir)
	if len(results) == 0 {
		t.Error("expected non-empty results")
	}
}

func TestCriticals_FiltersCorrectly(t *testing.T) {
	t.Helper()
	results := CheckResults{
		{Name: "a", Status: "OK"},
		{Name: "b", Status: "CRITICAL"},
		{Name: "c", Status: "WARN"},
		{Name: "d", Status: "CRITICAL"},
	}
	criticals := results.Criticals()
	if len(criticals) != 2 {
		t.Errorf("expected 2 criticals, got %d", len(criticals))
	}
}

func TestWarnings_FiltersCorrectly(t *testing.T) {
	t.Helper()
	results := CheckResults{
		{Name: "a", Status: "OK"},
		{Name: "b", Status: "WARN"},
	}
	warnings := results.Warnings()
	if len(warnings) != 1 {
		t.Errorf("expected 1 warning, got %d", len(warnings))
	}
}

func TestCheckFilesystem_ReturnsSomething(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	r := checkFilesystem(dir)
	if r.Name != "filesystem" {
		t.Errorf("expected check name 'filesystem', got %q", r.Name)
	}
}

func TestAutoRun_NoCriticalOnTempDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "keys"), 0700)
	results := RunAllChecks(dir)
	criticals := results.Criticals()
	if len(criticals) > 0 {
		t.Errorf("expected no criticals on temp dir, got %d: %v", len(criticals), criticals)
	}
}
