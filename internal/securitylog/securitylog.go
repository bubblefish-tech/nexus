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

// Package securitylog provides an append-only, mutex-protected JSON Lines
// writer for structured security events. Events are written to both a
// dedicated log file and kept in a bounded in-memory ring buffer for the
// /api/security/events and /api/security/summary admin endpoints.
//
// Reference: Tech Spec Section 11.2.
package securitylog

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Event represents a single structured security event.
// Reference: Tech Spec Section 11.2.
type Event struct {
	EventType string                 `json:"event_type"`
	Source    string                 `json:"source,omitempty"`
	Subject  string                 `json:"subject,omitempty"`
	IP       string                 `json:"ip,omitempty"`
	Endpoint string                 `json:"endpoint,omitempty"`
	Timestamp time.Time             `json:"timestamp"`
	Details  map[string]interface{} `json:"details,omitempty"`
}

// Summary holds aggregated counts for /api/security/summary.
type Summary struct {
	AuthFailures               int            `json:"auth_failures"`
	PolicyDenials              int            `json:"policy_denials"`
	RateLimitHits              int            `json:"rate_limit_hits"`
	WALTamperDetected          int            `json:"wal_tamper_detected"`
	ConfigSignatureInvalid     int            `json:"config_signature_invalid"`
	AdminAccess                int            `json:"admin_access"`
	RetrievalFirewallFiltered  int            `json:"retrieval_firewall_filtered"`
	RetrievalFirewallDenied    int            `json:"retrieval_firewall_denied"`
	BySource                   map[string]int `json:"by_source"`
}

// Logger is an append-only, mutex-protected security event logger.
// It writes JSON Lines to a file and retains the last maxRing events
// in memory for the admin API.
type Logger struct {
	mu      sync.Mutex
	file    *os.File
	enc     *json.Encoder
	ring    []Event
	maxRing int
	logger  *slog.Logger
}

// New creates a Logger that writes to logFile. The parent directory is
// created with mode 0700 if needed. The file is opened append-only with
// mode 0600. Returns an error if the file cannot be opened.
func New(logFile string, logger *slog.Logger) (*Logger, error) {
	dir := filepath.Dir(logFile)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, err
	}
	return &Logger{
		file:    f,
		enc:     json.NewEncoder(f),
		ring:    make([]Event, 0, 1024),
		maxRing: 1024,
		logger:  logger,
	}, nil
}

// Emit writes a security event to the file and ring buffer. It is safe
// for concurrent use. Errors writing to the file are logged but do not
// propagate — the ring buffer always receives the event.
func (l *Logger) Emit(e Event) {
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// Append to file — errors logged, never propagated.
	if err := l.enc.Encode(e); err != nil {
		l.logger.Error("securitylog: write failed",
			"component", "securitylog",
			"error", err,
		)
	}

	// Append to ring buffer, evicting oldest if full.
	if len(l.ring) >= l.maxRing {
		copy(l.ring, l.ring[1:])
		l.ring[len(l.ring)-1] = e
	} else {
		l.ring = append(l.ring, e)
	}
}

// Recent returns the last n events from the ring buffer. If n <= 0 or
// exceeds the buffer size, all buffered events are returned. The returned
// slice is a copy and safe to use without holding the lock.
func (l *Logger) Recent(n int) []Event {
	l.mu.Lock()
	defer l.mu.Unlock()

	total := len(l.ring)
	if n <= 0 || n > total {
		n = total
	}
	out := make([]Event, n)
	copy(out, l.ring[total-n:])
	return out
}

// Summarize returns aggregated counts across all buffered events.
func (l *Logger) Summarize() Summary {
	l.mu.Lock()
	defer l.mu.Unlock()

	s := Summary{BySource: make(map[string]int)}
	for _, e := range l.ring {
		switch e.EventType {
		case "auth_failure":
			s.AuthFailures++
		case "policy_denied":
			s.PolicyDenials++
		case "rate_limit_hit":
			s.RateLimitHits++
		case "wal_tamper_detected":
			s.WALTamperDetected++
		case "config_signature_invalid":
			s.ConfigSignatureInvalid++
		case "admin_access":
			s.AdminAccess++
		case "retrieval_firewall_filtered":
			s.RetrievalFirewallFiltered++
		case "retrieval_firewall_denied":
			s.RetrievalFirewallDenied++
		}
		if e.Source != "" {
			s.BySource[e.Source]++
		}
	}
	return s
}

// SourceMetrics holds per-source security metric breakdown.
type SourceMetrics struct {
	AuthFailures  int `json:"auth_failures"`
	PolicyDenials int `json:"policy_denials"`
	RateLimitHits int `json:"rate_limit_hits"`
}

// SummarizeDetailed returns aggregated counts with per-source per-event-type
// breakdown for the dashboard contract /api/security/summary shape.
func (l *Logger) SummarizeDetailed() (Summary, map[string]SourceMetrics) {
	l.mu.Lock()
	defer l.mu.Unlock()

	s := Summary{BySource: make(map[string]int)}
	bySource := make(map[string]SourceMetrics)

	for _, e := range l.ring {
		switch e.EventType {
		case "auth_failure":
			s.AuthFailures++
			if e.Source != "" {
				m := bySource[e.Source]
				m.AuthFailures++
				bySource[e.Source] = m
			}
		case "policy_denied":
			s.PolicyDenials++
			if e.Source != "" {
				m := bySource[e.Source]
				m.PolicyDenials++
				bySource[e.Source] = m
			}
		case "rate_limit_hit":
			s.RateLimitHits++
			if e.Source != "" {
				m := bySource[e.Source]
				m.RateLimitHits++
				bySource[e.Source] = m
			}
		case "wal_tamper_detected":
			s.WALTamperDetected++
		case "config_signature_invalid":
			s.ConfigSignatureInvalid++
		case "admin_access":
			s.AdminAccess++
		case "retrieval_firewall_filtered":
			s.RetrievalFirewallFiltered++
		case "retrieval_firewall_denied":
			s.RetrievalFirewallDenied++
		}
		if e.Source != "" {
			s.BySource[e.Source]++
		}
	}
	return s, bySource
}

// Close flushes and closes the underlying file. Safe to call multiple times.
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		err := l.file.Close()
		l.file = nil
		return err
	}
	return nil
}
