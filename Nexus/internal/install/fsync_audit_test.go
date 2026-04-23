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
	"os"
	"path/filepath"
	"testing"
)

func TestFsyncAudit_PassesOnTempDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	// Should complete without panic or error.
	FsyncAudit(dir)

	// Verify probe file is cleaned up.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() == ".fsync-audit-probe" {
			t.Error("fsync audit probe file was not cleaned up")
		}
	}
}

func TestFsyncAudit_WarnsOnReadOnlyDir(t *testing.T) {
	t.Helper()
	// Use a path that does not exist to simulate inability to write.
	nonExistent := filepath.Join(t.TempDir(), "does-not-exist", "subdir")
	// Should not panic — just log a warning.
	FsyncAudit(nonExistent)
}
