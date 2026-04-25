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
	"io"
	"os"
	"path/filepath"
)

const cleanShutdownMarker = "nexus.clean-shutdown"

// snapshotLastGood copies the destination DB to .lastgood for trivial recovery.
// Only snapshots if a clean-shutdown marker from the previous run exists.
// Returns nil if there is nothing to snapshot (no DB or no clean marker).
func snapshotLastGood(configDir, dbPath string) error {
	markerPath := filepath.Join(configDir, cleanShutdownMarker)
	if _, err := os.Stat(markerPath); os.IsNotExist(err) {
		return nil // previous run crashed — skip snapshot
	}

	src, err := os.Open(dbPath)
	if err != nil {
		return nil // no DB yet = nothing to snapshot
	}
	defer src.Close()

	dst, err := os.Create(dbPath + ".lastgood")
	if err != nil {
		return err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return err
	}

	// Remove the marker — next run must see a fresh one from Stop().
	_ = os.Remove(markerPath)
	return nil
}

// writeCleanShutdownMarker writes the clean-shutdown marker file.
// Called at the end of daemon.Stop() to indicate a graceful exit.
func writeCleanShutdownMarker(configDir string) error {
	markerPath := filepath.Join(configDir, cleanShutdownMarker)
	return os.WriteFile(markerPath, []byte("ok"), 0600)
}
