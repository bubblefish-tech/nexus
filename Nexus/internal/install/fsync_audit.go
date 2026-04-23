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

package install

import (
	"crypto/rand"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// FsyncAudit writes a test file, fsyncs it, reads it back, and verifies
// contents match. If the filesystem does not honor fsync, a warning is
// logged but install continues.
func FsyncAudit(dir string) {
	probePath := filepath.Join(dir, ".fsync-audit-probe")

	payload := make([]byte, 1024)
	if _, err := rand.Read(payload); err != nil {
		slog.Warn(fmt.Sprintf("WARNING: fsync audit could not generate test data: %v", err))
		return
	}

	f, err := os.OpenFile(probePath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		slog.Warn(fmt.Sprintf("WARNING: fsync audit could not create test file: %v", err))
		return
	}

	if _, err := f.Write(payload); err != nil {
		_ = f.Close()
		_ = os.Remove(probePath)
		slog.Warn(fmt.Sprintf("WARNING: fsync audit write failed: %v", err))
		return
	}

	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(probePath)
		slog.Warn(fmt.Sprintf("WARNING: fsync audit sync failed: %v", err))
		return
	}
	_ = f.Close()

	readback, err := os.ReadFile(probePath)
	_ = os.Remove(probePath)
	if err != nil {
		slog.Warn(fmt.Sprintf("WARNING: fsync audit readback failed: %v", err))
		return
	}

	if len(readback) != len(payload) {
		slog.Warn(fmt.Sprintf("WARNING: filesystem at %s may not honor fsync. Data durability is at risk.", dir))
		return
	}
	for i := range payload {
		if payload[i] != readback[i] {
			slog.Warn(fmt.Sprintf("WARNING: filesystem at %s may not honor fsync. Data durability is at risk.", dir))
			return
		}
	}
}
