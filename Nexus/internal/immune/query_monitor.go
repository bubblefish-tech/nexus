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

package immune

import (
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"
)

// MonitorConfig holds tunable parameters for the QueryMonitor.
type MonitorConfig struct {
	// WindowDuration is the sliding window for anomaly assessment. Default: 5m.
	WindowDuration time.Duration
	// RateLimitPerMin is the per-minute query rate that triggers RATE_LIMIT.
	// Default: 100.
	RateLimitPerMin int
	// OverlapThreshold is the number of prior queries whose tokens overlap with
	// the current query before MEMBERSHIP_INFERENCE fires. Default: 10.
	OverlapThreshold int
}

// DefaultMonitorConfig returns the recommended production defaults.
func DefaultMonitorConfig() MonitorConfig {
	return MonitorConfig{
		WindowDuration:   5 * time.Minute,
		RateLimitPerMin:  100,
		OverlapThreshold: 10,
	}
}

// MonitorAlert is returned by RecordQuery when an anomaly is detected.
type MonitorAlert struct {
	AgentID   string
	AlertType string // "RATE_LIMIT", "MEMBERSHIP_INFERENCE", "POST_DELETE_PROBE"
	Details   string
}

// QueryMonitor tracks per-agent query patterns in a sliding window and emits
// alerts on excess query rate, membership inference attempts, and queries
// targeting recently deleted content.
type QueryMonitor struct {
	mu     sync.Mutex
	cfg    MonitorConfig
	agents map[string]*agentState
	now    func() time.Time
}

type agentState struct {
	queries     []queryRecord
	deletedRefs []deletedRef
}

type queryRecord struct {
	ts     time.Time
	tokens map[string]struct{}
}

type deletedRef struct {
	ts  time.Time
	ref string // lowercased, trimmed
}

// NewQueryMonitor creates a QueryMonitor. Zero-value fields in cfg fall back
// to DefaultMonitorConfig values.
func NewQueryMonitor(cfg MonitorConfig) *QueryMonitor {
	def := DefaultMonitorConfig()
	if cfg.WindowDuration == 0 {
		cfg.WindowDuration = def.WindowDuration
	}
	if cfg.RateLimitPerMin == 0 {
		cfg.RateLimitPerMin = def.RateLimitPerMin
	}
	if cfg.OverlapThreshold == 0 {
		cfg.OverlapThreshold = def.OverlapThreshold
	}
	return &QueryMonitor{
		cfg:    cfg,
		agents: make(map[string]*agentState),
		now:    time.Now,
	}
}

// WithClock replaces the internal time source. Used in tests to drive time
// forward deterministically.
func (m *QueryMonitor) WithClock(fn func() time.Time) *QueryMonitor {
	m.now = fn
	return m
}

// RecordQuery records a query from agentID and returns a MonitorAlert if an
// anomaly is detected, or nil when the query is normal. Checks are evaluated
// in severity order: RATE_LIMIT → POST_DELETE_PROBE → MEMBERSHIP_INFERENCE.
func (m *QueryMonitor) RecordQuery(agentID, query string) *MonitorAlert {
	return m.recordQuery(agentID, query, m.now())
}

// NotifyDelete registers that contentRef was deleted for agentID. Subsequent
// queries whose text contains contentRef (case-insensitive) will be flagged as
// POST_DELETE_PROBE for the duration of the window.
func (m *QueryMonitor) NotifyDelete(agentID, contentRef string) {
	m.notifyDelete(agentID, contentRef, m.now())
}

func (m *QueryMonitor) recordQuery(agentID, query string, now time.Time) *MonitorAlert {
	m.mu.Lock()
	defer m.mu.Unlock()

	st := m.stateFor(agentID)
	m.evict(st, now)

	tokens := extractQueryTokens(query)

	// 1. Rate limit: count queries issued within the last minute.
	cutoff1m := now.Add(-time.Minute)
	count1m := 0
	for _, qr := range st.queries {
		if qr.ts.After(cutoff1m) {
			count1m++
		}
	}
	if count1m >= m.cfg.RateLimitPerMin {
		st.queries = append(st.queries, queryRecord{ts: now, tokens: tokens})
		return &MonitorAlert{
			AgentID:   agentID,
			AlertType: "RATE_LIMIT",
			Details: fmt.Sprintf(
				"agent issued %d queries in the last minute (limit %d)",
				count1m+1, m.cfg.RateLimitPerMin,
			),
		}
	}

	// 2. Post-delete probing: query text contains a recently deleted content ref.
	queryLower := strings.ToLower(query)
	for _, dr := range st.deletedRefs {
		if strings.Contains(queryLower, dr.ref) {
			st.queries = append(st.queries, queryRecord{ts: now, tokens: tokens})
			return &MonitorAlert{
				AgentID:   agentID,
				AlertType: "POST_DELETE_PROBE",
				Details:   fmt.Sprintf("query matches recently deleted content ref %q", dr.ref),
			}
		}
	}

	// 3. Membership inference: count prior queries whose tokens overlap with
	// the current query's significant token set.
	if len(tokens) > 0 {
		overlapCount := 0
		for _, qr := range st.queries {
			if hasTokenOverlap(tokens, qr.tokens) {
				overlapCount++
			}
		}
		if overlapCount > m.cfg.OverlapThreshold {
			st.queries = append(st.queries, queryRecord{ts: now, tokens: tokens})
			return &MonitorAlert{
				AgentID:   agentID,
				AlertType: "MEMBERSHIP_INFERENCE",
				Details: fmt.Sprintf(
					"query overlaps with %d prior queries in window (threshold %d)",
					overlapCount, m.cfg.OverlapThreshold,
				),
			}
		}
	}

	st.queries = append(st.queries, queryRecord{ts: now, tokens: tokens})
	return nil
}

func (m *QueryMonitor) notifyDelete(agentID, contentRef string, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ref := strings.ToLower(strings.TrimSpace(contentRef))
	if ref == "" {
		return
	}
	st := m.stateFor(agentID)
	m.evict(st, now)
	st.deletedRefs = append(st.deletedRefs, deletedRef{ts: now, ref: ref})
}

func (m *QueryMonitor) stateFor(agentID string) *agentState {
	st, ok := m.agents[agentID]
	if !ok {
		st = &agentState{}
		m.agents[agentID] = st
	}
	return st
}

// evict removes records older than WindowDuration from st.
func (m *QueryMonitor) evict(st *agentState, now time.Time) {
	cutoff := now.Add(-m.cfg.WindowDuration)

	qi := 0
	for _, qr := range st.queries {
		if qr.ts.After(cutoff) {
			st.queries[qi] = qr
			qi++
		}
	}
	st.queries = st.queries[:qi]

	di := 0
	for _, dr := range st.deletedRefs {
		if dr.ts.After(cutoff) {
			st.deletedRefs[di] = dr
			di++
		}
	}
	st.deletedRefs = st.deletedRefs[:di]
}

// hasTokenOverlap returns true if a and b share at least one token.
func hasTokenOverlap(a, b map[string]struct{}) bool {
	for tok := range a {
		if _, ok := b[tok]; ok {
			return true
		}
	}
	return false
}

// queryStopwords is a lookup set of common English words excluded from token
// extraction. These carry no discriminating signal for overlap detection.
var queryStopwords = map[string]bool{
	"that": true, "this": true, "with": true, "from": true, "have": true,
	"been": true, "will": true, "they": true, "were": true, "what": true,
	"when": true, "your": true, "also": true, "does": true, "just": true,
	"more": true, "some": true, "time": true, "very": true, "which": true,
	"said": true, "each": true, "into": true, "than": true, "then": true,
	"them": true, "their": true, "there": true, "about": true, "would": true,
	"could": true, "should": true, "other": true, "after": true, "before": true,
	"these": true, "those": true, "where": true, "while": true, "being": true,
}

// extractQueryTokens returns the significant tokens from query. Only tokens
// with 4 or more characters that are not stopwords are included.
func extractQueryTokens(query string) map[string]struct{} {
	tokens := make(map[string]struct{})
	var buf strings.Builder
	flush := func() {
		w := strings.ToLower(buf.String())
		buf.Reset()
		if len(w) >= 4 && !queryStopwords[w] {
			tokens[w] = struct{}{}
		}
	}
	for _, r := range query {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			buf.WriteRune(r)
		} else if buf.Len() > 0 {
			flush()
		}
	}
	if buf.Len() > 0 {
		flush()
	}
	return tokens
}
