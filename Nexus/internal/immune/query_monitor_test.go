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

package immune_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bubblefish-tech/nexus/internal/immune"
)

// fixedClock returns a clock function whose value can be advanced by the caller.
type fixedClock struct{ t time.Time }

func (c *fixedClock) now() time.Time      { return c.t }
func (c *fixedClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newTestMonitor(cfg immune.MonitorConfig) (*immune.QueryMonitor, *fixedClock) {
	clk := &fixedClock{t: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	m := immune.NewQueryMonitor(cfg).WithClock(clk.now)
	return m, clk
}

// helper used in table-driven tests so t.Helper() is set correctly.
func requireAlert(t *testing.T, alert *immune.MonitorAlert, wantType string) {
	t.Helper()
	if alert == nil {
		t.Fatalf("expected alert %q but got nil", wantType)
	}
	if alert.AlertType != wantType {
		t.Fatalf("expected AlertType=%q, got %q (details: %s)", wantType, alert.AlertType, alert.Details)
	}
}

func requireNoAlert(t *testing.T, alert *immune.MonitorAlert) {
	t.Helper()
	if alert != nil {
		t.Fatalf("expected no alert but got %q: %s", alert.AlertType, alert.Details)
	}
}

// ── Rate limiting ────────────────────────────────────────────────────────────

func TestQueryMonitor_RateLimit_Fires(t *testing.T) {
	t.Helper()
	m, _ := newTestMonitor(immune.MonitorConfig{RateLimitPerMin: 5})

	for i := 0; i < 5; i++ {
		a := m.RecordQuery("agent-a", fmt.Sprintf("query number %d distinct", i))
		requireNoAlert(t, a)
	}
	// 6th query within the same minute triggers RATE_LIMIT.
	a := m.RecordQuery("agent-a", "query number 6 distinct")
	requireAlert(t, a, "RATE_LIMIT")
	if a.AgentID != "agent-a" {
		t.Fatalf("AgentID: want agent-a, got %q", a.AgentID)
	}
}

func TestQueryMonitor_RateLimit_BelowThreshold_NoAlert(t *testing.T) {
	t.Helper()
	m, _ := newTestMonitor(immune.MonitorConfig{RateLimitPerMin: 10})

	for i := 0; i < 10; i++ {
		a := m.RecordQuery("agent-b", fmt.Sprintf("search term unique%d", i))
		requireNoAlert(t, a)
	}
}

func TestQueryMonitor_RateLimit_AlertDetailsContainCount(t *testing.T) {
	t.Helper()
	m, _ := newTestMonitor(immune.MonitorConfig{RateLimitPerMin: 3})

	for i := 0; i < 3; i++ {
		m.RecordQuery("agent-c", fmt.Sprintf("query detail test %d", i))
	}
	a := m.RecordQuery("agent-c", "query detail test 4")
	requireAlert(t, a, "RATE_LIMIT")
	if !strings.Contains(a.Details, "4") {
		t.Fatalf("Details should mention query count, got: %q", a.Details)
	}
}

func TestQueryMonitor_RateLimit_OldQueriesIgnored(t *testing.T) {
	t.Helper()
	m, clk := newTestMonitor(immune.MonitorConfig{
		WindowDuration:  5 * time.Minute,
		RateLimitPerMin: 3,
	})

	// Issue 3 queries, then advance time past the 1-minute boundary.
	for i := 0; i < 3; i++ {
		m.RecordQuery("agent-d", fmt.Sprintf("stale query unique%d", i))
	}
	clk.advance(61 * time.Second)

	// These 3 queries are now in a fresh minute window — no alert.
	for i := 0; i < 3; i++ {
		a := m.RecordQuery("agent-d", fmt.Sprintf("fresh query unique%d", i))
		requireNoAlert(t, a)
	}
}

// ── Membership inference ─────────────────────────────────────────────────────

func TestQueryMonitor_MembershipInference_Fires(t *testing.T) {
	t.Helper()
	m, _ := newTestMonitor(immune.MonitorConfig{
		OverlapThreshold: 3,
		RateLimitPerMin:  1000,
	})

	// 4 queries all containing the significant token "wallet".
	for i := 0; i < 4; i++ {
		m.RecordQuery("agent-e", fmt.Sprintf("wallet balance transaction%d", i))
	}
	// 5th query with "wallet" should trigger: 4 prior queries overlap > threshold(3).
	a := m.RecordQuery("agent-e", "wallet transfer history")
	requireAlert(t, a, "MEMBERSHIP_INFERENCE")
}

func TestQueryMonitor_MembershipInference_AtThreshold_NoAlert(t *testing.T) {
	t.Helper()
	m, _ := newTestMonitor(immune.MonitorConfig{
		OverlapThreshold: 5,
		RateLimitPerMin:  1000,
	})

	// 5 queries with shared token "passport".
	for i := 0; i < 5; i++ {
		m.RecordQuery("agent-f", fmt.Sprintf("passport renewal document%d", i))
	}
	// Exactly 5 overlap — equals threshold, must NOT fire (rule is ">").
	a := m.RecordQuery("agent-f", "passport application process")
	requireNoAlert(t, a)
}

func TestQueryMonitor_MembershipInference_AlertDetailsContainCount(t *testing.T) {
	t.Helper()
	m, _ := newTestMonitor(immune.MonitorConfig{
		OverlapThreshold: 2,
		RateLimitPerMin:  1000,
	})

	for i := 0; i < 3; i++ {
		m.RecordQuery("agent-g", fmt.Sprintf("invoice payment record%d", i))
	}
	a := m.RecordQuery("agent-g", "invoice outstanding balance")
	requireAlert(t, a, "MEMBERSHIP_INFERENCE")
	if !strings.Contains(a.Details, "3") {
		t.Fatalf("Details should mention overlap count, got: %q", a.Details)
	}
}

func TestQueryMonitor_MembershipInference_EmptyQuery_NoAlert(t *testing.T) {
	t.Helper()
	m, _ := newTestMonitor(immune.MonitorConfig{
		OverlapThreshold: 0,
		RateLimitPerMin:  1000,
	})

	// Even with a threshold of 0, an empty query has no tokens and must not fire.
	for i := 0; i < 5; i++ {
		m.RecordQuery("agent-h", "")
	}
	a := m.RecordQuery("agent-h", "")
	requireNoAlert(t, a)
}

// ── Post-delete probing ──────────────────────────────────────────────────────

func TestQueryMonitor_PostDeleteProbe_Fires(t *testing.T) {
	t.Helper()
	m, _ := newTestMonitor(immune.DefaultMonitorConfig())

	m.NotifyDelete("agent-i", "alice-project-2026")
	a := m.RecordQuery("agent-i", "search for alice-project-2026 details")
	requireAlert(t, a, "POST_DELETE_PROBE")
	if a.AgentID != "agent-i" {
		t.Fatalf("AgentID: want agent-i, got %q", a.AgentID)
	}
}

func TestQueryMonitor_PostDeleteProbe_CaseInsensitive(t *testing.T) {
	t.Helper()
	m, _ := newTestMonitor(immune.DefaultMonitorConfig())

	m.NotifyDelete("agent-j", "SecretDocument")
	a := m.RecordQuery("agent-j", "looking for secretdocument backup")
	requireAlert(t, a, "POST_DELETE_PROBE")
}

func TestQueryMonitor_PostDeleteProbe_NoDelete_NoAlert(t *testing.T) {
	t.Helper()
	m, _ := newTestMonitor(immune.DefaultMonitorConfig())

	a := m.RecordQuery("agent-k", "find alice-project-2026")
	requireNoAlert(t, a)
}

func TestQueryMonitor_PostDeleteProbe_RefExpires(t *testing.T) {
	t.Helper()
	m, clk := newTestMonitor(immune.MonitorConfig{
		WindowDuration:  2 * time.Minute,
		RateLimitPerMin: 1000,
	})

	m.NotifyDelete("agent-l", "expired-secret-ref")
	clk.advance(3 * time.Minute) // past window

	a := m.RecordQuery("agent-l", "search expired-secret-ref data")
	requireNoAlert(t, a)
}

// ── Window eviction ──────────────────────────────────────────────────────────

func TestQueryMonitor_WindowEviction_OldQueriesRemovedFromOverlapCheck(t *testing.T) {
	t.Helper()
	m, clk := newTestMonitor(immune.MonitorConfig{
		WindowDuration:   30 * time.Second,
		RateLimitPerMin:  1000,
		OverlapThreshold: 2,
	})

	// 3 queries with token "treasury" — these will age out.
	for i := 0; i < 3; i++ {
		m.RecordQuery("agent-m", fmt.Sprintf("treasury funds record%d", i))
	}
	// Advance past window; old queries evicted.
	clk.advance(31 * time.Second)

	// New queries with same token should not see the old overlap.
	for i := 0; i < 3; i++ {
		a := m.RecordQuery("agent-m", fmt.Sprintf("treasury funds fresh%d", i))
		requireNoAlert(t, a)
	}
}

// ── Agent isolation ──────────────────────────────────────────────────────────

func TestQueryMonitor_TwoAgents_Independent(t *testing.T) {
	t.Helper()
	m, _ := newTestMonitor(immune.MonitorConfig{
		RateLimitPerMin:  3,
		OverlapThreshold: 2,
	})

	// Fill agent-n to the threshold.
	for i := 0; i < 3; i++ {
		m.RecordQuery("agent-n", fmt.Sprintf("query overlap shared%d", i))
	}
	// agent-o should be unaffected.
	a := m.RecordQuery("agent-o", "query overlap shared fresh")
	requireNoAlert(t, a)
}

// ── Default config ───────────────────────────────────────────────────────────

func TestQueryMonitor_DefaultConfig_Fields(t *testing.T) {
	t.Helper()
	cfg := immune.DefaultMonitorConfig()
	if cfg.WindowDuration != 5*time.Minute {
		t.Errorf("WindowDuration: want 5m, got %v", cfg.WindowDuration)
	}
	if cfg.RateLimitPerMin != 100 {
		t.Errorf("RateLimitPerMin: want 100, got %d", cfg.RateLimitPerMin)
	}
	if cfg.OverlapThreshold != 10 {
		t.Errorf("OverlapThreshold: want 10, got %d", cfg.OverlapThreshold)
	}
}

func TestQueryMonitor_ZeroConfig_UsesDefaults(t *testing.T) {
	t.Helper()
	// A zero MonitorConfig should fall back to defaults without panicking.
	m := immune.NewQueryMonitor(immune.MonitorConfig{})
	a := m.RecordQuery("agent-p", "normal query about project data")
	requireNoAlert(t, a)
}
