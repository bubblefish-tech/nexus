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

package daemon_test

import (
	"encoding/json"
	"fmt"
	"hash/crc32"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/BubbleFish-Nexus/internal/audit"
	"github.com/BubbleFish-Nexus/internal/config"
	"github.com/BubbleFish-Nexus/internal/daemon"
)

// writeTestAuditRecord writes a single JSONL+CRC32 line to a file.
func writeTestAuditRecord(t *testing.T, f *os.File, rec audit.InteractionRecord) {
	t.Helper()
	rec.CRC32 = ""
	jsonBytes, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	checksum := crc32.ChecksumIEEE(jsonBytes)
	line := fmt.Sprintf("%s\t%08x\n", jsonBytes, checksum)
	if _, err := f.WriteString(line); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// setupAuditDaemon creates a test daemon with a real audit log containing one
// record and a rate limiter configured at the given rpm.
func setupAuditDaemon(t *testing.T, rpm int) (*daemon.Daemon, string) {
	t.Helper()

	// Create a real audit log file with one record.
	dir := t.TempDir()
	logFile := filepath.Join(dir, "interactions.jsonl")
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("create audit log: %v", err)
	}
	writeTestAuditRecord(t, f, audit.InteractionRecord{
		RecordID:       "test-rec-001",
		Timestamp:      time.Now().UTC(),
		Source:         "claude",
		OperationType:  "write",
		PolicyDecision: "allowed",
	})
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	src, keys := stdSource("claude", "audit-rl-key")

	cfg := &config.Config{
		Daemon: config.DaemonConfig{
			Port: 0,
			Bind: "127.0.0.1",
			Audit: config.AuditConfig{
				Enabled:              true,
				AdminRateLimitPerMin: rpm,
			},
			RateLimit: config.GlobalRateLimitConfig{
				GlobalRequestsPerMinute: 100000,
			},
		},
		Retrieval:          config.RetrievalConfig{DefaultProfile: "balanced"},
		Sources:            []*config.Source{src},
		Destinations:       []*config.Destination{{Name: "sqlite", Type: "sqlite"}},
		ResolvedSourceKeys: keys,
		ResolvedAdminKey:   []byte("audit-admin-key"),
	}

	d := daemon.NewTestDaemon(t, cfg)
	d.SetAuditReader(audit.NewAuditReader(logFile, audit.WithReaderDualWrite(false)))
	d.SetAuditRateLimiter()

	baseURL, shutdown := liveServer(t, d)
	t.Cleanup(shutdown)

	return d, baseURL
}

// TestAuditStats_RateLimited verifies that /api/audit/stats enforces the
// admin_rate_limit_per_minute. Requests beyond the limit receive 429 with
// a Retry-After header.
func TestAuditStats_RateLimited(t *testing.T) {
	const rpm = 3
	_, baseURL := setupAuditDaemon(t, rpm)
	client := &http.Client{Timeout: 5 * time.Second}

	// Exhaust the rate limit budget.
	for i := 0; i < rpm; i++ {
		status, _ := get(t, client, baseURL+"/api/audit/stats", "audit-admin-key")
		if status != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i, status)
		}
	}

	// The next request must be rate-limited.
	req, err := http.NewRequest(http.MethodGet, baseURL+"/api/audit/stats", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer audit-admin-key")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", resp.StatusCode)
	}
	if resp.Header.Get("Retry-After") == "" {
		t.Fatal("expected Retry-After header on 429 response")
	}

	var errBody struct{ Error string `json:"error"` }
	if err := json.NewDecoder(resp.Body).Decode(&errBody); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if errBody.Error != "rate_limit_exceeded" {
		t.Fatalf("expected error=rate_limit_exceeded, got %q", errBody.Error)
	}
}

// TestAuditExport_RateLimited verifies that /api/audit/export enforces the
// admin_rate_limit_per_minute. Requests beyond the limit receive 429 with
// a Retry-After header.
func TestAuditExport_RateLimited(t *testing.T) {
	const rpm = 3
	_, baseURL := setupAuditDaemon(t, rpm)
	client := &http.Client{Timeout: 5 * time.Second}

	// Exhaust the rate limit budget.
	for i := 0; i < rpm; i++ {
		status, _ := get(t, client, baseURL+"/api/audit/export", "audit-admin-key")
		if status != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i, status)
		}
	}

	// The next request must be rate-limited.
	req, err := http.NewRequest(http.MethodGet, baseURL+"/api/audit/export", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer audit-admin-key")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", resp.StatusCode)
	}
	if resp.Header.Get("Retry-After") == "" {
		t.Fatal("expected Retry-After header on 429 response")
	}

	var errBody struct{ Error string `json:"error"` }
	if err := json.NewDecoder(resp.Body).Decode(&errBody); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if errBody.Error != "rate_limit_exceeded" {
		t.Fatalf("expected error=rate_limit_exceeded, got %q", errBody.Error)
	}
}
