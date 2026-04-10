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

// Package doctor implements the "bubblefish doctor" health check subsystem.
// It tests whether the daemon, WAL, destinations, and disk are healthy without
// modifying any persistent state.
//
// Reference: Tech Spec Section 13.1, Phase 0D Behavioral Contract items 11–13.
package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// DefaultMinDiskFreeBytes is the minimum acceptable free disk space on the WAL
// partition. Matches the WAL watchdog default of 100 MiB.
const DefaultMinDiskFreeBytes uint64 = 100 * 1024 * 1024

// Pinger is the subset of destination.DestinationWriter required for doctor
// checks. It matches the interface exactly so destinations can be passed
// directly without adaptation.
//
// INVARIANT: Close() MUST be called after Ping() to prevent connection leaks.
// Reference: Phase 0D Behavioral Contract item 12 (CR-7).
type Pinger interface {
	Ping() error
	Close() error
}

// Named associates a Pinger with a human-readable name for reporting.
type Named struct {
	Name string
	Dest Pinger
}

// DestinationResult is the health outcome for a single destination.
type DestinationResult struct {
	// Name is the destination's configured name.
	Name string `json:"name"`

	// Reachable is true if Ping() succeeded.
	Reachable bool `json:"reachable"`

	// Error is the Ping() error message, or "" on success.
	Error string `json:"error,omitempty"`
}

// Result is the aggregated doctor health check outcome.
type Result struct {
	// DaemonRunning is always true when Check is called from within the daemon.
	DaemonRunning bool `json:"daemon_running"`

	// WALWritable is true if a probe file could be written and deleted in walDir.
	WALWritable bool `json:"wal_writable"`

	// DiskFreeBytes is the available disk space on the WAL partition in bytes.
	DiskFreeBytes uint64 `json:"disk_free_bytes"`

	// DiskSpaceOK is true if DiskFreeBytes >= 100 MiB.
	DiskSpaceOK bool `json:"disk_space_ok"`

	// Destinations holds the reachability result for each destination.
	Destinations []DestinationResult `json:"destinations"`

	// AllHealthy is true when all individual checks pass.
	AllHealthy bool `json:"all_healthy"`
}

// Check runs all doctor health checks and returns a Result.
// minDiskBytes is the minimum free disk threshold; pass 0 to use the default
// (100 MiB).
//
// For each destination: Ping() is called, then Close() is called regardless of
// Ping outcome. This prevents connection leaks (CR-7).
//
// Reference: Tech Spec Section 13.1, Phase 0D Behavioral Contract items 11–13.
func Check(walDir string, dests []Named, minDiskBytes uint64) Result {
	if minDiskBytes == 0 {
		minDiskBytes = DefaultMinDiskFreeBytes
	}

	r := Result{
		DaemonRunning: true, // If we are executing, the daemon is running.
		Destinations:  make([]DestinationResult, 0, len(dests)),
	}

	// WAL directory: write a probe file then delete it.
	r.WALWritable = checkWALWritable(walDir)

	// Disk space on the WAL partition.
	free, err := diskFreeBytes(walDir)
	if err == nil {
		r.DiskFreeBytes = free
		r.DiskSpaceOK = free >= minDiskBytes
	}

	// Destination reachability. Always Close() after Ping() — CR-7.
	for _, d := range dests {
		dr := DestinationResult{Name: d.Name}
		pingErr := d.Dest.Ping()
		if pingErr != nil {
			dr.Reachable = false
			dr.Error = pingErr.Error()
		} else {
			dr.Reachable = true
		}
		// Close unconditionally to release the connection. Ignore close error —
		// doctor is read-only; a close failure does not affect the check result.
		_ = d.Dest.Close()
		r.Destinations = append(r.Destinations, dr)
	}

	// AllHealthy requires every individual check to pass.
	r.AllHealthy = r.WALWritable && r.DiskSpaceOK
	for _, dr := range r.Destinations {
		if !dr.Reachable {
			r.AllHealthy = false
		}
	}

	return r
}

// checkWALWritable creates and immediately removes a probe file in walDir.
// Returns true only if both the write and the removal succeed.
//
// Reference: Phase 0D Behavioral Contract item 13.
func checkWALWritable(walDir string) bool {
	// Use UnixNano in the name to avoid collisions with concurrent probes.
	probeFile := filepath.Join(walDir, fmt.Sprintf(".probe-%d", time.Now().UnixNano()))
	f, err := os.OpenFile(probeFile, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return false
	}
	_ = f.Close()
	return os.Remove(probeFile) == nil
}
