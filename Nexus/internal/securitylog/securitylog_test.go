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

package securitylog

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

func TestEmitAndRecent(t *testing.T) {
	t.Helper()
	logFile := filepath.Join(t.TempDir(), "logs", "security.jsonl")
	l, err := New(logFile, testLogger(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer l.Close()

	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	l.Emit(Event{
		EventType: "auth_failure",
		Source:    "unknown",
		IP:        "10.0.0.1",
		Endpoint:  "/inbound/claude",
		Timestamp: ts,
		Details:   map[string]interface{}{"token_class": "unknown"},
	})
	l.Emit(Event{
		EventType: "policy_denied",
		Source:    "claude",
		Subject:   "global",
		Endpoint:  "/inbound/claude",
		Timestamp: ts.Add(time.Second),
		Details:   map[string]interface{}{"reason": "write not allowed"},
	})

	// Recent(0) returns all.
	events := l.Recent(0)
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].EventType != "auth_failure" {
		t.Errorf("event[0] type = %q, want auth_failure", events[0].EventType)
	}
	if events[1].EventType != "policy_denied" {
		t.Errorf("event[1] type = %q, want policy_denied", events[1].EventType)
	}

	// Recent(1) returns last event only.
	events = l.Recent(1)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].EventType != "policy_denied" {
		t.Errorf("event[0] type = %q, want policy_denied", events[0].EventType)
	}
}

func TestFileContainsJSONLines(t *testing.T) {
	t.Helper()
	logFile := filepath.Join(t.TempDir(), "security.jsonl")
	l, err := New(logFile, testLogger(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	l.Emit(Event{EventType: "auth_failure", IP: "1.2.3.4"})
	l.Emit(Event{EventType: "rate_limit_hit", Source: "claude"})
	l.Close()

	f, err := os.Open(logFile)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineCount := 0
	for scanner.Scan() {
		lineCount++
		var ev Event
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			t.Fatalf("line %d: invalid JSON: %v", lineCount, err)
		}
		if ev.EventType == "" {
			t.Fatalf("line %d: empty event_type", lineCount)
		}
		if ev.Timestamp.IsZero() {
			t.Fatalf("line %d: missing timestamp", lineCount)
		}
	}
	if lineCount != 2 {
		t.Fatalf("expected 2 lines, got %d", lineCount)
	}
}

func TestSummarize(t *testing.T) {
	t.Helper()
	logFile := filepath.Join(t.TempDir(), "security.jsonl")
	l, err := New(logFile, testLogger(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer l.Close()

	events := []Event{
		{EventType: "auth_failure", Source: "unknown"},
		{EventType: "auth_failure", Source: "unknown"},
		{EventType: "policy_denied", Source: "claude"},
		{EventType: "rate_limit_hit", Source: "claude"},
		{EventType: "wal_tamper_detected"},
		{EventType: "config_signature_invalid"},
		{EventType: "admin_access", IP: "127.0.0.1"},
	}
	for _, e := range events {
		l.Emit(e)
	}

	s := l.Summarize()
	if s.AuthFailures != 2 {
		t.Errorf("AuthFailures = %d, want 2", s.AuthFailures)
	}
	if s.PolicyDenials != 1 {
		t.Errorf("PolicyDenials = %d, want 1", s.PolicyDenials)
	}
	if s.RateLimitHits != 1 {
		t.Errorf("RateLimitHits = %d, want 1", s.RateLimitHits)
	}
	if s.WALTamperDetected != 1 {
		t.Errorf("WALTamperDetected = %d, want 1", s.WALTamperDetected)
	}
	if s.ConfigSignatureInvalid != 1 {
		t.Errorf("ConfigSignatureInvalid = %d, want 1", s.ConfigSignatureInvalid)
	}
	if s.AdminAccess != 1 {
		t.Errorf("AdminAccess = %d, want 1", s.AdminAccess)
	}
	if s.BySource["claude"] != 2 {
		t.Errorf("BySource[claude] = %d, want 2", s.BySource["claude"])
	}
	if s.BySource["unknown"] != 2 {
		t.Errorf("BySource[unknown] = %d, want 2", s.BySource["unknown"])
	}
}

func TestRingBufferEviction(t *testing.T) {
	t.Helper()
	logFile := filepath.Join(t.TempDir(), "security.jsonl")
	l, err := New(logFile, testLogger(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer l.Close()

	// Override maxRing to test eviction.
	l.maxRing = 3

	for i := 0; i < 5; i++ {
		l.Emit(Event{EventType: "auth_failure", Source: "test", Details: map[string]interface{}{"i": i}})
	}

	events := l.Recent(0)
	if len(events) != 3 {
		t.Fatalf("expected 3 events in ring, got %d", len(events))
	}
	// Oldest events (i=0, i=1) should be evicted; ring has i=2, i=3, i=4.
	for idx, want := range []int{2, 3, 4} {
		got, _ := events[idx].Details["i"].(int)
		if got != want {
			t.Errorf("events[%d].Details[i] = %v, want %v", idx, got, want)
		}
	}
}

func TestConcurrentEmit(t *testing.T) {
	t.Helper()
	logFile := filepath.Join(t.TempDir(), "security.jsonl")
	l, err := New(logFile, testLogger(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer l.Close()

	const goroutines = 10
	const eventsPerGoroutine = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < eventsPerGoroutine; i++ {
				l.Emit(Event{EventType: "auth_failure", Source: "concurrent"})
			}
		}()
	}
	wg.Wait()

	total := goroutines * eventsPerGoroutine
	events := l.Recent(0)
	if len(events) != total {
		t.Errorf("expected %d events, got %d", total, len(events))
	}
}

func TestFilePermissions(t *testing.T) {
	t.Helper()
	logFile := filepath.Join(t.TempDir(), "secure", "events.jsonl")
	l, err := New(logFile, testLogger(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer l.Close()

	// Check file exists and is writable.
	info, err := os.Stat(logFile)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.IsDir() {
		t.Fatal("expected file, got directory")
	}
}

func TestCloseIdempotent(t *testing.T) {
	t.Helper()
	logFile := filepath.Join(t.TempDir(), "security.jsonl")
	l, err := New(logFile, testLogger(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func TestTimestampAutoSet(t *testing.T) {
	t.Helper()
	logFile := filepath.Join(t.TempDir(), "security.jsonl")
	l, err := New(logFile, testLogger(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer l.Close()

	before := time.Now().UTC()
	l.Emit(Event{EventType: "auth_failure"})
	after := time.Now().UTC()

	events := l.Recent(1)
	if len(events) != 1 {
		t.Fatal("expected 1 event")
	}
	ts := events[0].Timestamp
	if ts.Before(before) || ts.After(after) {
		t.Errorf("auto-set timestamp %v not between %v and %v", ts, before, after)
	}
}
