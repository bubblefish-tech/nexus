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

package daemon

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/BubbleFish-Nexus/internal/config"
	"github.com/BubbleFish-Nexus/internal/securitylog"
)

func newTestDaemonWithSecurityLog(t *testing.T) *Daemon {
	t.Helper()
	logFile := filepath.Join(t.TempDir(), "security.jsonl")
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	sl, err := securitylog.New(logFile, logger)
	if err != nil {
		t.Fatalf("securitylog.New: %v", err)
	}
	t.Cleanup(func() { sl.Close() })

	d := &Daemon{
		cfg: &config.Config{
			Daemon: config.DaemonConfig{},
		},
		logger:      logger,
		securityLog: sl,
		stopped:     make(chan struct{}),
	}
	return d
}

func TestEmitSecurityEvent(t *testing.T) {
	t.Helper()
	d := newTestDaemonWithSecurityLog(t)

	d.emitSecurityEvent(securitylog.Event{
		EventType: "auth_failure",
		Source:    "unknown",
		IP:        "10.0.0.1",
		Endpoint:  "/inbound/claude",
	})

	events := d.securityLog.Recent(0)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].EventType != "auth_failure" {
		t.Errorf("event type = %q, want auth_failure", events[0].EventType)
	}
	if events[0].IP != "10.0.0.1" {
		t.Errorf("event IP = %q, want 10.0.0.1", events[0].IP)
	}
}

func TestEmitSecurityEventNilLogger(t *testing.T) {
	t.Helper()
	// When securityLog is nil, emitSecurityEvent should be a no-op (no panic).
	d := &Daemon{
		cfg: &config.Config{},
		logger: slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelWarn,
		})),
		stopped: make(chan struct{}),
	}
	// Should not panic.
	d.emitSecurityEvent(securitylog.Event{EventType: "auth_failure"})
}

func TestHandleSecurityEvents(t *testing.T) {
	t.Helper()
	d := newTestDaemonWithSecurityLog(t)

	// Emit some events.
	d.securityLog.Emit(securitylog.Event{EventType: "auth_failure", Source: "unknown", IP: "1.2.3.4"})
	d.securityLog.Emit(securitylog.Event{EventType: "policy_denied", Source: "claude"})
	d.securityLog.Emit(securitylog.Event{EventType: "rate_limit_hit", Source: "claude"})

	req := httptest.NewRequest(http.MethodGet, "/api/security/events?limit=10", nil)
	w := httptest.NewRecorder()
	d.handleSecurityEvents(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp struct {
		Events []securitylog.Event `json:"events"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// 3 original events + 1 admin_access from the handler itself.
	if len(resp.Events) < 3 {
		t.Fatalf("expected at least 3 events, got %d", len(resp.Events))
	}

	// First 3 events should be our emitted events.
	if resp.Events[0].EventType != "auth_failure" {
		t.Errorf("events[0] type = %q, want auth_failure", resp.Events[0].EventType)
	}
}

func TestHandleSecuritySummary(t *testing.T) {
	t.Helper()
	d := newTestDaemonWithSecurityLog(t)

	d.securityLog.Emit(securitylog.Event{EventType: "auth_failure", Source: "unknown"})
	d.securityLog.Emit(securitylog.Event{EventType: "auth_failure", Source: "unknown"})
	d.securityLog.Emit(securitylog.Event{EventType: "policy_denied", Source: "claude"})

	req := httptest.NewRequest(http.MethodGet, "/api/security/summary", nil)
	w := httptest.NewRecorder()
	d.handleSecuritySummary(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var summary securitylog.Summary
	if err := json.Unmarshal(w.Body.Bytes(), &summary); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if summary.AuthFailures != 2 {
		t.Errorf("AuthFailures = %d, want 2", summary.AuthFailures)
	}
	if summary.PolicyDenials != 1 {
		t.Errorf("PolicyDenials = %d, want 1", summary.PolicyDenials)
	}
}

func TestHandleSecurityEventsDisabled(t *testing.T) {
	t.Helper()
	// When security events are disabled, handlers should return empty results.
	d := &Daemon{
		cfg: &config.Config{},
		logger: slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelWarn,
		})),
		stopped: make(chan struct{}),
	}

	req := httptest.NewRequest(http.MethodGet, "/api/security/events", nil)
	w := httptest.NewRecorder()
	d.handleSecurityEvents(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp struct {
		Events  []securitylog.Event `json:"events"`
		Message string              `json:"message"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Events) != 0 {
		t.Errorf("expected 0 events, got %d", len(resp.Events))
	}
	if resp.Message != "security events not enabled" {
		t.Errorf("message = %q, want 'security events not enabled'", resp.Message)
	}
}

func TestHandleSecurityEventsLimitClamping(t *testing.T) {
	t.Helper()
	d := newTestDaemonWithSecurityLog(t)

	// Emit 5 events.
	for i := 0; i < 5; i++ {
		d.securityLog.Emit(securitylog.Event{EventType: "auth_failure"})
	}

	// Request with limit=2 should return only the last 2 + the admin_access event.
	req := httptest.NewRequest(http.MethodGet, "/api/security/events?limit=2", nil)
	w := httptest.NewRecorder()
	d.handleSecurityEvents(w, req)

	var resp struct {
		Events []securitylog.Event `json:"events"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Events) != 2 {
		t.Errorf("expected 2 events, got %d", len(resp.Events))
	}
}
