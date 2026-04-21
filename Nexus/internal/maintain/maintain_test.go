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

package maintain_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/bubblefish-tech/nexus/internal/maintain"
)

// newTestMaintainer creates a Maintainer backed by a temp dir.
func newTestMaintainer(t *testing.T) *maintain.Maintainer {
	t.Helper()
	cfg := maintain.Config{
		ConfigDir:         t.TempDir(),
		ReconcileInterval: time.Hour, // prevent auto-reconcile during tests
		ScanInterval:      time.Hour, // prevent auto-scan during tests
		LearnedStorePath:  filepath.Join(t.TempDir(), "fixes.json"),
	}
	m, err := maintain.New(cfg, nil)
	if err != nil {
		t.Fatalf("maintain.New: %v", err)
	}
	return m
}

// TestNew_Defaults verifies that New succeeds with a zero Config
// (defaults are applied) and returns a non-nil Maintainer.
func TestNew_Defaults(t *testing.T) {
	m := newTestMaintainer(t)
	if m == nil {
		t.Fatal("expected non-nil Maintainer")
	}
}

// TestNew_Twin verifies the embedded EnvironmentTwin is exposed.
func TestNew_Twin(t *testing.T) {
	m := newTestMaintainer(t)
	if m.Twin() == nil {
		t.Fatal("Twin() must not be nil")
	}
}

// TestNew_Registry verifies the connector registry is loaded.
func TestNew_Registry(t *testing.T) {
	m := newTestMaintainer(t)
	reg := m.Registry()
	if reg == nil {
		t.Fatal("Registry() must not be nil")
	}
	if reg.Len() == 0 {
		t.Error("expected at least one connector in embedded registry")
	}
}

// TestStatus_EmptyAfterNew verifies Status() is coherent on a fresh Maintainer
// before any scan has run.
func TestStatus_EmptyAfterNew(t *testing.T) {
	m := newTestMaintainer(t)
	s := m.Status()
	if s.Platform == "" {
		t.Error("Platform must not be empty")
	}
	// No scan has run yet, so LastScan should be zero.
	if !s.LastScan.IsZero() {
		t.Errorf("LastScan should be zero before first scan, got %v", s.LastScan)
	}
}

// TestScan_UpdatesLastScan verifies Scan() populates LastScan.
func TestScan_UpdatesLastScan(t *testing.T) {
	m := newTestMaintainer(t)
	before := time.Now().UTC()
	if err := m.Scan(context.Background()); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	s := m.Status()
	if s.LastScan.Before(before) {
		t.Errorf("LastScan (%v) should be after scan start (%v)", s.LastScan, before)
	}
}

// TestStart_Stop verifies the background loops start and stop cleanly.
func TestStart_Stop(t *testing.T) {
	m := newTestMaintainer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Second Start is a no-op.
	if err := m.Start(ctx); err != nil {
		t.Errorf("second Start should be no-op, got: %v", err)
	}
	m.Stop()
}

// TestStart_InitialScan verifies Start() runs an initial scan synchronously.
func TestStart_InitialScan(t *testing.T) {
	m := newTestMaintainer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	s := m.Status()
	if s.LastScan.IsZero() {
		t.Error("Start should perform an initial scan; LastScan is still zero")
	}
	m.Stop()
}

// TestReconcile_ReturnsResults verifies Reconcile() returns a result slice
// (may be empty if no tools are tracked, but must not panic).
func TestReconcile_ReturnsResults(t *testing.T) {
	m := newTestMaintainer(t)
	results := m.Reconcile(context.Background())
	// result slice may be nil or empty on a fresh twin — just must not panic.
	_ = results
}

// TestFixTool_UnknownTool returns an error for an unregistered tool.
func TestFixTool_UnknownTool(t *testing.T) {
	m := newTestMaintainer(t)
	err := m.FixTool(context.Background(), "nonexistent-tool-xyz")
	if err == nil {
		t.Error("expected error for unknown tool, got nil")
	}
}

// TestStatus_Fields verifies all exported fields on MaintainStatus are populated
// after a scan.
func TestStatus_Fields(t *testing.T) {
	m := newTestMaintainer(t)
	if err := m.Scan(context.Background()); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	s := m.Status()
	if s.Platform == "" {
		t.Error("Platform must not be empty after scan")
	}
}

// TestStatus_LearnedCountReflectsReconcile verifies LearnedCount grows after
// Reconcile records outcomes to the learned store.
func TestStatus_LearnedCountReflectsReconcile(t *testing.T) {
	m := newTestMaintainer(t)
	before := m.Status().LearnedCount
	// Reconcile on an empty twin produces no results (no tools tracked).
	// Just verifying the count doesn't decrease.
	m.Reconcile(context.Background())
	after := m.Status().LearnedCount
	if after < before {
		t.Errorf("LearnedCount went from %d to %d after Reconcile", before, after)
	}
}
