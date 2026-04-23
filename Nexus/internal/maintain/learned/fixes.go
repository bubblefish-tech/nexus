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

// Package learned implements adaptive fix learning — the immune-memory pattern.
// Every time the convergence loop applies a fix recipe, the outcome (success or
// failure) is recorded here. Over time the store builds a weighted model of which
// fixes reliably work for which tools, so BestIssue can prefer proven recipes over
// untried ones. Weights decay exponentially (half-life 7 days) so stale knowledge
// does not dominate over fresh observations.
//
// Persistence: the store writes JSON to disk after every Record() call, so the
// learned model survives daemon restarts. Raw success/failure counts and last-seen
// timestamps are persisted; the decay factor is recomputed from those at query time.
package learned

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const (
	// halfLifeDays is the time after which an unseen fix loses half its weight.
	halfLifeDays = 7.0

	// neutralWeight is returned for fixes with no observation history.
	// 0.5 places unknown fixes between known-bad (→0) and known-good (→1).
	neutralWeight = 0.5

	// decayK is the per-day exponential decay constant: k = ln(2) / halfLife.
	decayK = math.Ln2 / halfLifeDays
)

// FixOutcome is the result of applying one fix recipe.
type FixOutcome int

const (
	OutcomeSuccess FixOutcome = iota
	OutcomeFailure
)

// FixMemory is the learned record for one (tool, issue) pair.
type FixMemory struct {
	ToolName   string     `json:"tool"`
	IssueID    string     `json:"issue"`
	Successes  int        `json:"successes"`
	Failures   int        `json:"failures"`
	LastSeen   time.Time  `json:"last_seen"`
	LastResult FixOutcome `json:"last_result"`
}

// Weight returns the decay-adjusted success rate at time now.
//
//	weight = (successes / total) × exp(−k × daysSinceLastSeen)
//
// Returns neutralWeight when no observations have been recorded.
func (fm *FixMemory) Weight(now time.Time) float64 {
	total := fm.Successes + fm.Failures
	if total == 0 {
		return neutralWeight
	}
	rate := float64(fm.Successes) / float64(total)
	days := now.Sub(fm.LastSeen).Hours() / 24
	if days < 0 {
		days = 0
	}
	return rate * math.Exp(-decayK*days)
}

// Store persists learned fix outcomes in a JSON file and provides weight-based
// issue selection. All methods are safe for concurrent use.
type Store struct {
	mu      sync.RWMutex
	records map[string]*FixMemory // key: "tool:issue"
	path    string
}

// NewStore opens (or creates) the store at path. Existing data is loaded if the
// file exists; a missing file is treated as an empty store (not an error).
func NewStore(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	s := &Store{
		records: make(map[string]*FixMemory),
		path:    path,
	}
	_ = s.load() // missing file is fine — start fresh
	return s, nil
}

// Record saves the outcome of applying issueID's fix recipe to toolName.
// The store is persisted to disk after every call.
func (s *Store) Record(toolName, issueID string, outcome FixOutcome) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := storeKey(toolName, issueID)
	fm, ok := s.records[k]
	if !ok {
		fm = &FixMemory{ToolName: toolName, IssueID: issueID}
		s.records[k] = fm
	}
	fm.LastSeen = time.Now().UTC()
	fm.LastResult = outcome
	switch outcome {
	case OutcomeSuccess:
		fm.Successes++
	case OutcomeFailure:
		fm.Failures++
	}
	_ = s.persist() // best-effort; caller can call Save() to check the error
}

// Weight returns the decay-adjusted success rate for (toolName, issueID).
// Returns neutralWeight (0.5) when no history exists for that pair.
func (s *Store) Weight(toolName, issueID string) float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	fm, ok := s.records[storeKey(toolName, issueID)]
	if !ok {
		return neutralWeight
	}
	return fm.Weight(time.Now().UTC())
}

// BestIssue returns the candidate issue ID with the highest learned weight for
// toolName. When no memory exists for any candidate, the first candidate is
// returned (preserving the connector registry's ordering as tie-breaker).
// Returns "" when candidates is empty.
func (s *Store) BestIssue(toolName string, candidates []string) string {
	if len(candidates) == 0 {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now().UTC()
	best := candidates[0]
	bestW := -1.0
	for _, issue := range candidates {
		w := neutralWeight
		if fm, ok := s.records[storeKey(toolName, issue)]; ok {
			w = fm.Weight(now)
		}
		if w > bestW {
			bestW = w
			best = issue
		}
	}
	return best
}

// All returns a stable-sorted snapshot of all learned records (tool then issue).
func (s *Store) All() []*FixMemory {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*FixMemory, 0, len(s.records))
	for _, fm := range s.records {
		cp := *fm
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ToolName != out[j].ToolName {
			return out[i].ToolName < out[j].ToolName
		}
		return out[i].IssueID < out[j].IssueID
	})
	return out
}

// Save persists the current state to disk and returns any write error.
func (s *Store) Save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.persist()
}

// Len returns the number of (tool, issue) pairs in the store.
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.records)
}

// persist writes the store to disk. Must be called with at least a read lock held.
func (s *Store) persist() error {
	data, err := json.MarshalIndent(s.records, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0600)
}

// load reads the JSON file into s.records. Caller is responsible for locking.
func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var records map[string]*FixMemory
	if err := json.Unmarshal(data, &records); err != nil {
		return err
	}
	s.mu.Lock()
	s.records = records
	s.mu.Unlock()
	return nil
}

// storeKey returns the canonical map key for a (tool, issue) pair.
func storeKey(tool, issue string) string {
	return tool + ":" + issue
}
