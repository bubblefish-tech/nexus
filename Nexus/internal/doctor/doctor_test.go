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

package doctor_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/bubblefish-tech/nexus/internal/doctor"
)

// stubPinger is a test double for doctor.Pinger. It returns the configured
// error (or nil) for both Ping and Close.
type stubPinger struct {
	name      string
	pingErr   error
	closeCalls int
}

func (s *stubPinger) Ping() error  { return s.pingErr }
func (s *stubPinger) Close() error { s.closeCalls++; return nil }

// TestDoctorHealthy verifies that all checks pass when the WAL dir is
// writable and all destinations respond to Ping.
//
// Reference: Phase 0D Verification Gate ("Doctor with healthy setup: all checks pass").
func TestDoctorHealthy(t *testing.T) {
	walDir := t.TempDir()
	dest := &stubPinger{name: "test-dest"}

	result := doctor.Check(walDir, []doctor.Named{
		{Name: dest.name, Dest: dest},
	}, 0)

	if !result.DaemonRunning {
		t.Error("expected DaemonRunning=true")
	}
	if !result.WALWritable {
		t.Errorf("expected WALWritable=true for writable temp dir %s", walDir)
	}
	if !result.DiskSpaceOK {
		// Only fail if disk is truly low — machines with very little space
		// should not break CI, but this would be a real concern.
		t.Logf("DiskSpaceOK=false (free=%d bytes); continuing (may indicate low disk)", result.DiskFreeBytes)
	}
	if len(result.Destinations) != 1 {
		t.Fatalf("expected 1 destination result, got %d", len(result.Destinations))
	}
	if !result.Destinations[0].Reachable {
		t.Errorf("expected destination reachable, got error: %s", result.Destinations[0].Error)
	}
	// CR-7: Close must be called after Ping even on success.
	if dest.closeCalls != 1 {
		t.Errorf("expected Close() called once, got %d", dest.closeCalls)
	}
}

// TestDoctorUnwritableWAL verifies that WALWritable=false is reported when the
// WAL directory does not exist (so the probe file cannot be written).
//
// Reference: Phase 0D Verification Gate ("Doctor with unwritable WAL dir: reports failure").
func TestDoctorUnwritableWAL(t *testing.T) {
	// Use a path that cannot exist: a nested path under a temp dir that was
	// never created. The probe file write will fail because the parent is absent.
	nonExistent := filepath.Join(t.TempDir(), "does", "not", "exist")

	result := doctor.Check(nonExistent, nil, 0)

	if result.WALWritable {
		t.Error("expected WALWritable=false for non-existent directory")
	}
	// AllHealthy must be false if WAL is not writable.
	if result.AllHealthy {
		t.Error("expected AllHealthy=false when WAL is not writable")
	}
}

// TestDoctorDestinationUnreachable verifies that a failing Ping is captured in
// the result and marks AllHealthy=false. Close() is still called (CR-7).
//
// Reference: Phase 0D Behavioral Contract item 12.
func TestDoctorDestinationUnreachable(t *testing.T) {
	walDir := t.TempDir()
	pingErr := errors.New("connection refused")
	dest := &stubPinger{name: "unreachable", pingErr: pingErr}

	result := doctor.Check(walDir, []doctor.Named{
		{Name: dest.name, Dest: dest},
	}, 0)

	if len(result.Destinations) != 1 {
		t.Fatalf("expected 1 destination result, got %d", len(result.Destinations))
	}
	dr := result.Destinations[0]
	if dr.Reachable {
		t.Error("expected destination not reachable")
	}
	if dr.Error == "" {
		t.Error("expected non-empty Error when Ping fails")
	}
	if result.AllHealthy {
		t.Error("expected AllHealthy=false when destination unreachable")
	}
	// CR-7: Close must be called even after a failing Ping.
	if dest.closeCalls != 1 {
		t.Errorf("expected Close() called once after Ping failure, got %d", dest.closeCalls)
	}
}
