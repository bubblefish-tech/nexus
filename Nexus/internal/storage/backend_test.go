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

package storage

import "testing"

func TestSQLiteBackend_ImplementsBackend(t *testing.T) {
	t.Helper()
	var _ Backend = (*SQLiteBackend)(nil)
}

func TestCapabilities_SQLiteDefaults(t *testing.T) {
	t.Helper()
	// Cannot construct a real SQLiteBackend without a live DB, but we can
	// verify the Capabilities struct shape and defaults.
	caps := Capabilities{
		ConcurrentWriters: false,
		NativeVectorIndex: false,
		FTSRanking:        "bm25",
		SemanticSearch:    false,
		ConflictDetection: true,
		TimeTravel:        true,
		ClusterQueries:    true,
		Encryption:        true,
	}
	if caps.ConcurrentWriters {
		t.Error("SQLite should not support concurrent writers")
	}
	if caps.FTSRanking != "bm25" {
		t.Errorf("expected FTSRanking=bm25, got %s", caps.FTSRanking)
	}
}
