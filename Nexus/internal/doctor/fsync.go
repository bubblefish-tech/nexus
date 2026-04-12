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

package doctor

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// FsyncTestResult holds the outcome of the fsync verification test.
type FsyncTestResult struct {
	// OK is true if fsync appears to be working correctly.
	OK bool `json:"ok"`
	// Duration is how long the test took.
	Duration time.Duration `json:"duration"`
	// Error is set when the test failed or fsync appears to be a no-op.
	Error string `json:"error,omitempty"`
}

// FsyncTest writes random data, fsyncs, closes, re-opens via a fresh file
// descriptor, reads back, and compares. This detects broken fsync
// implementations (network storage, some consumer SSDs).
//
// dir is the directory where the probe file is created — typically the WAL
// directory so the test exercises the same filesystem.
//
// Reference: v0.1.3 Build Plan Phase 1 Subtask 1.6.
func FsyncTest(dir string) FsyncTestResult {
	start := time.Now()

	// Generate 4KB of random data — large enough to require a real flush.
	payload := make([]byte, 4096)
	if _, err := rand.Read(payload); err != nil {
		return FsyncTestResult{Error: fmt.Sprintf("generate random data: %v", err)}
	}

	probePath := filepath.Join(dir, fmt.Sprintf(".fsync-probe-%d", time.Now().UnixNano()))

	// Step 1: Write + fsync + close.
	f, err := os.OpenFile(probePath, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return FsyncTestResult{Error: fmt.Sprintf("create probe file: %v", err)}
	}

	if _, err := f.Write(payload); err != nil {
		_ = f.Close()
		_ = os.Remove(probePath)
		return FsyncTestResult{Error: fmt.Sprintf("write probe data: %v", err)}
	}

	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(probePath)
		return FsyncTestResult{Error: fmt.Sprintf("fsync probe file: %v", err)}
	}

	if err := f.Close(); err != nil {
		_ = os.Remove(probePath)
		return FsyncTestResult{Error: fmt.Sprintf("close probe file: %v", err)}
	}

	// Step 2: Re-open with a FRESH file descriptor and read back.
	f2, err := os.Open(probePath)
	if err != nil {
		_ = os.Remove(probePath)
		return FsyncTestResult{Error: fmt.Sprintf("reopen probe file: %v", err)}
	}

	readback := make([]byte, len(payload))
	n, err := f2.Read(readback)
	_ = f2.Close()
	_ = os.Remove(probePath)

	if err != nil {
		return FsyncTestResult{Error: fmt.Sprintf("read back probe data: %v", err)}
	}
	if n != len(payload) {
		return FsyncTestResult{Error: fmt.Sprintf("short read: got %d bytes, expected %d", n, len(payload))}
	}

	// Step 3: Compare.
	for i := range payload {
		if payload[i] != readback[i] {
			return FsyncTestResult{
				Error: fmt.Sprintf("fsync appears to be a no-op: byte mismatch at offset %d", i),
			}
		}
	}

	return FsyncTestResult{
		OK:       true,
		Duration: time.Since(start),
	}
}
