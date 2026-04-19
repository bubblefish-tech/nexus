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

package destination_test

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bubblefish-tech/nexus/internal/destination"
)

// openTestSQLite opens a temp SQLite database for tier tests.
func openTestSQLite(t *testing.T) *destination.SQLiteDestination {
	t.Helper()
	dir := t.TempDir()
	db, err := destination.OpenSQLite(filepath.Join(dir, "test.db"), slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func makePayload(t *testing.T, id, content string, tier int) destination.TranslatedPayload {
	t.Helper()
	return destination.TranslatedPayload{
		PayloadID:   id,
		RequestID:   "req-" + id,
		Source:      "test-source",
		Subject:     "subject",
		Namespace:   "ns",
		Destination: "dest",
		Content:     content,
		Timestamp:   time.Now().UTC(),
		Tier:        tier,
	}
}

// TestTier_SQLEnforcement verifies that SQL-layer tier filtering prevents
// sources from reading entries above their tier level.
func TestTier_SQLEnforcement(t *testing.T) {
	t.Helper()
	db := openTestSQLite(t)

	// Write entries at different tiers.
	if err := db.Write(makePayload(t, "t0", "public content", 0)); err != nil {
		t.Fatalf("write tier 0: %v", err)
	}
	if err := db.Write(makePayload(t, "t1", "internal content", 1)); err != nil {
		t.Fatalf("write tier 1: %v", err)
	}
	if err := db.Write(makePayload(t, "t2", "confidential content", 2)); err != nil {
		t.Fatalf("write tier 2: %v", err)
	}
	if err := db.Write(makePayload(t, "t3", "secret content", 3)); err != nil {
		t.Fatalf("write tier 3: %v", err)
	}

	tests := []struct {
		name       string
		sourceTier int
		wantCount  int
	}{
		{"tier0_source_sees_tier0_only", 0, 1},
		{"tier1_source_sees_tier0_and_1", 1, 2},
		{"tier2_source_sees_tier0_through_2", 2, 3},
		{"tier3_source_sees_all", 3, 4},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := db.Query(destination.QueryParams{
				Namespace:   "ns",
				Destination: "dest",
				Limit:       100,
				TierFilter:  true,
				SourceTier:  tc.sourceTier,
			})
			if err != nil {
				t.Fatalf("Query: %v", err)
			}
			if len(result.Records) != tc.wantCount {
				t.Errorf("sourceTier=%d: got %d records, want %d", tc.sourceTier, len(result.Records), tc.wantCount)
			}
		})
	}
}

// TestTier_NoFilter verifies that admin queries (TierFilter=false) see all tiers.
func TestTier_NoFilter(t *testing.T) {
	t.Helper()
	db := openTestSQLite(t)

	for i, tier := range []int{0, 1, 2, 3} {
		id := string(rune('a' + i))
		if err := db.Write(makePayload(t, id, "content", tier)); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	result, err := db.Query(destination.QueryParams{
		Namespace:   "ns",
		Destination: "dest",
		Limit:       100,
		TierFilter:  false, // admin — no restriction
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(result.Records) != 4 {
		t.Errorf("admin query got %d records, want 4", len(result.Records))
	}
}

// TestTier_StoredAsWritten verifies that the Tier value is stored and returned exactly.
// Defaulting (0→1) is the handler's responsibility, not the destination layer's.
func TestTier_StoredAsWritten(t *testing.T) {
	t.Helper()
	db := openTestSQLite(t)

	// Write with explicit Tier=2.
	p := makePayload(t, "tier2-stored", "sensitive content", 2)
	if err := db.Write(p); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Source with tier=1 should NOT see it.
	result1, err := db.Query(destination.QueryParams{
		Namespace:   "ns",
		Destination: "dest",
		Limit:       100,
		TierFilter:  true,
		SourceTier:  1,
	})
	if err != nil {
		t.Fatalf("Query tier 1: %v", err)
	}
	if len(result1.Records) != 0 {
		t.Errorf("tier-1 source should not see tier-2 entry, got %d records", len(result1.Records))
	}

	// Source with tier=2 should see it.
	result2, err := db.Query(destination.QueryParams{
		Namespace:   "ns",
		Destination: "dest",
		Limit:       100,
		TierFilter:  true,
		SourceTier:  2,
	})
	if err != nil {
		t.Fatalf("Query tier 2: %v", err)
	}
	if len(result2.Records) != 1 {
		t.Errorf("tier-2 source should see tier-2 entry, got %d records", len(result2.Records))
	}
	if result2.Records[0].Tier != 2 {
		t.Errorf("stored Tier = %d, want 2", result2.Records[0].Tier)
	}
}

// TestTier_Persisted verifies the tier value round-trips correctly.
func TestTier_Persisted(t *testing.T) {
	t.Helper()
	db := openTestSQLite(t)

	p := makePayload(t, "tier2-entry", "sensitive content", 2)
	if err := db.Write(p); err != nil {
		t.Fatalf("write: %v", err)
	}

	result, err := db.Query(destination.QueryParams{
		Namespace:   "ns",
		Destination: "dest",
		Limit:       100,
		TierFilter:  true,
		SourceTier:  3,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(result.Records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(result.Records))
	}
	if result.Records[0].Tier != 2 {
		t.Errorf("Tier = %d, want 2", result.Records[0].Tier)
	}
}
