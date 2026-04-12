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
	"os"
	"testing"
)

func TestFsyncTest_Passes(t *testing.T) {
	dir := t.TempDir()
	result := FsyncTest(dir)
	if !result.OK {
		t.Fatalf("FsyncTest failed on temp dir: %s", result.Error)
	}
	if result.Duration <= 0 {
		t.Error("expected positive duration")
	}
}

func TestFsyncTest_BadDir(t *testing.T) {
	result := FsyncTest("/nonexistent/path/that/should/not/exist")
	if result.OK {
		t.Error("expected failure for nonexistent directory")
	}
	if result.Error == "" {
		t.Error("expected non-empty error message")
	}
}

func TestFsyncTest_CleansUp(t *testing.T) {
	dir := t.TempDir()
	_ = FsyncTest(dir)

	// Verify no probe files remain.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "." && e.Name() != ".." {
			t.Errorf("probe file not cleaned up: %s", e.Name())
		}
	}
}
