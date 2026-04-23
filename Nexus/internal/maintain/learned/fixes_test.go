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

package learned_test

import (
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/bubblefish-tech/nexus/internal/maintain/learned"
)

// tempStore returns a Store backed by a temp-dir JSON file.
func tempStore(t *testing.T) *learned.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixes.json")
	s, err := learned.NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s
}

// TestStore_Record_IncreasesSuccesses verifies that OutcomeSuccess increments the counter.
func TestStore_Record_IncreasesSuccesses(t *testing.T) {
	s := tempStore(t)
	s.Record("claude-desktop", "missing_mcp_servers_key", learned.OutcomeSuccess)
	all := s.All()
	if len(all) != 1 {
		t.Fatalf("expected 1 record, got %d", len(all))
	}
	if all[0].Successes != 1 {
		t.Errorf("expected 1 success, got %d", all[0].Successes)
	}
	if all[0].Failures != 0 {
		t.Errorf("expected 0 failures, got %d", all[0].Failures)
	}
}

// TestStore_Record_IncreasesFailures verifies that OutcomeFailure increments the counter.
func TestStore_Record_IncreasesFailures(t *testing.T) {
	s := tempStore(t)
	s.Record("ollama", "port_not_listening", learned.OutcomeFailure)
	all := s.All()
	if all[0].Failures != 1 {
		t.Errorf("expected 1 failure, got %d", all[0].Failures)
	}
	if all[0].LastResult != learned.OutcomeFailure {
		t.Error("LastResult should be OutcomeFailure")
	}
}

// TestStore_Record_Accumulates verifies multiple records for the same pair accumulate.
func TestStore_Record_Accumulates(t *testing.T) {
	s := tempStore(t)
	for i := 0; i < 5; i++ {
		s.Record("cursor", "wrong_nexus_command", learned.OutcomeSuccess)
	}
	s.Record("cursor", "wrong_nexus_command", learned.OutcomeFailure)
	all := s.All()
	if all[0].Successes != 5 || all[0].Failures != 1 {
		t.Errorf("expected 5 successes 1 failure, got %d/%d", all[0].Successes, all[0].Failures)
	}
}

// TestStore_Weight_NoHistory returns neutralWeight (0.5) for unknown pairs.
func TestStore_Weight_NoHistory(t *testing.T) {
	s := tempStore(t)
	w := s.Weight("unknown-tool", "unknown-issue")
	if w != 0.5 {
		t.Errorf("expected neutral weight 0.5, got %f", w)
	}
}

// TestStore_Weight_AllSuccesses returns close to 1.0 for recent all-success history.
func TestStore_Weight_AllSuccesses(t *testing.T) {
	s := tempStore(t)
	for i := 0; i < 10; i++ {
		s.Record("claude-desktop", "missing_mcp_servers_key", learned.OutcomeSuccess)
	}
	w := s.Weight("claude-desktop", "missing_mcp_servers_key")
	// Weight should be close to 1.0 (just recorded, minimal decay)
	if w < 0.9 {
		t.Errorf("all-success recent weight should be near 1.0, got %f", w)
	}
}

// TestStore_Weight_AllFailures returns close to 0.0 for recent all-failure history.
func TestStore_Weight_AllFailures(t *testing.T) {
	s := tempStore(t)
	for i := 0; i < 10; i++ {
		s.Record("ollama", "port_not_listening", learned.OutcomeFailure)
	}
	w := s.Weight("ollama", "port_not_listening")
	if w > 0.1 {
		t.Errorf("all-failure weight should be near 0.0, got %f", w)
	}
}

// TestFixMemory_Weight_Decays verifies the decay formula: at half-life the weight
// is halved relative to a freshly-recorded entry.
func TestFixMemory_Weight_Decays(t *testing.T) {
	s := tempStore(t)
	s.Record("windsurf", "drift_issue", learned.OutcomeSuccess)
	all := s.All()
	fm := all[0]

	now := time.Now().UTC()
	wFresh := fm.Weight(now)

	// Simulate 7 days later (the configured half-life)
	wHalfLife := fm.Weight(now.Add(7 * 24 * time.Hour))

	ratio := wHalfLife / wFresh
	// Should be approximately 0.5 (±5%)
	if math.Abs(ratio-0.5) > 0.05 {
		t.Errorf("expected weight to halve at 7 days, ratio=%.4f", ratio)
	}
}

// TestStore_BestIssue_PrefersSuccessful verifies that BestIssue returns the
// candidate with the highest learned weight.
func TestStore_BestIssue_PrefersSuccessful(t *testing.T) {
	s := tempStore(t)
	// "fix_a" has mixed history; "fix_b" always succeeds.
	for i := 0; i < 5; i++ {
		s.Record("cursor", "fix_a", learned.OutcomeSuccess)
		s.Record("cursor", "fix_a", learned.OutcomeFailure) // 50% rate
	}
	for i := 0; i < 10; i++ {
		s.Record("cursor", "fix_b", learned.OutcomeSuccess) // 100% rate
	}
	best := s.BestIssue("cursor", []string{"fix_a", "fix_b"})
	if best != "fix_b" {
		t.Errorf("expected fix_b (100%% success rate), got %q", best)
	}
}

// TestStore_BestIssue_NoHistory returns the first candidate unchanged.
func TestStore_BestIssue_NoHistory(t *testing.T) {
	s := tempStore(t)
	best := s.BestIssue("unknown-tool", []string{"issue_a", "issue_b"})
	if best != "issue_a" {
		t.Errorf("expected first candidate when no history, got %q", best)
	}
}

// TestStore_BestIssue_Empty returns empty string for empty candidates.
func TestStore_BestIssue_Empty(t *testing.T) {
	s := tempStore(t)
	if got := s.BestIssue("tool", nil); got != "" {
		t.Errorf("expected empty string for nil candidates, got %q", got)
	}
}

// TestStore_SaveLoad_RoundTrip verifies that Save+Load preserves all records exactly.
func TestStore_SaveLoad_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixes.json")
	s, _ := learned.NewStore(path)

	s.Record("claude-desktop", "missing_mcp_servers_key", learned.OutcomeSuccess)
	s.Record("ollama", "port_not_listening", learned.OutcomeFailure)
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	s2, err := learned.NewStore(path)
	if err != nil {
		t.Fatalf("NewStore (reload): %v", err)
	}
	if s2.Len() != 2 {
		t.Errorf("expected 2 records after reload, got %d", s2.Len())
	}
	all := s2.All()
	if all[0].ToolName != "claude-desktop" || all[0].Successes != 1 {
		t.Errorf("claude-desktop record not preserved: %+v", all[0])
	}
	if all[1].ToolName != "ollama" || all[1].Failures != 1 {
		t.Errorf("ollama record not preserved: %+v", all[1])
	}
}

// TestStore_PersistsAfterRecord verifies the JSON file is updated on every Record.
func TestStore_PersistsAfterRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixes.json")
	s, _ := learned.NewStore(path)

	s.Record("cursor", "issue_x", learned.OutcomeSuccess)
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected JSON file after Record: %v", err)
	}
}

// TestStore_All_Sorted verifies All() returns records in stable tool+issue order.
func TestStore_All_Sorted(t *testing.T) {
	s := tempStore(t)
	s.Record("zed", "issue_z", learned.OutcomeSuccess)
	s.Record("aider", "issue_a", learned.OutcomeSuccess)
	s.Record("aider", "issue_b", learned.OutcomeSuccess)

	all := s.All()
	if len(all) != 3 {
		t.Fatalf("expected 3, got %d", len(all))
	}
	// aider < zed; within aider: issue_a < issue_b
	if all[0].ToolName != "aider" || all[0].IssueID != "issue_a" {
		t.Errorf("sort wrong at [0]: %+v", all[0])
	}
	if all[1].ToolName != "aider" || all[1].IssueID != "issue_b" {
		t.Errorf("sort wrong at [1]: %+v", all[1])
	}
	if all[2].ToolName != "zed" {
		t.Errorf("sort wrong at [2]: %+v", all[2])
	}
}

// TestStore_ConcurrentAccess exercises the store under concurrent reads and writes.
// Run with -race to catch data races.
func TestStore_ConcurrentAccess(t *testing.T) {
	s := tempStore(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			s.Record("claude-desktop", "missing_key", learned.OutcomeSuccess)
		}()
		go func() {
			defer wg.Done()
			_ = s.Weight("claude-desktop", "missing_key")
		}()
		go func() {
			defer wg.Done()
			_ = s.BestIssue("claude-desktop", []string{"missing_key", "wrong_cmd"})
		}()
	}
	wg.Wait()
}

// TestStore_Len verifies Len() reflects the correct number of unique pairs.
func TestStore_Len(t *testing.T) {
	s := tempStore(t)
	if s.Len() != 0 {
		t.Errorf("empty store should have Len=0, got %d", s.Len())
	}
	s.Record("tool-a", "issue-1", learned.OutcomeSuccess)
	s.Record("tool-a", "issue-2", learned.OutcomeSuccess) // different pair
	s.Record("tool-a", "issue-1", learned.OutcomeSuccess) // same pair again
	if s.Len() != 2 {
		t.Errorf("expected Len=2, got %d", s.Len())
	}
}
