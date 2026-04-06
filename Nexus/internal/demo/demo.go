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

// Package demo implements the BubbleFish Nexus reliability demo — the golden
// crash-recovery scenario that proves WAL-first durability.
//
// The demo writes 50 memories with unique idempotency keys, kills the daemon
// via SIGKILL, restarts it, waits for readiness, queries all memories, and
// asserts exactly 50 results with 0 duplicates.
//
// Reference: Tech Spec Section 13.3, Phase R-26.
package demo

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"
)

// Options configures a reliability demo run.
type Options struct {
	// URL is the base URL of the running Nexus daemon. When empty, the demo
	// runner starts and manages its own daemon process.
	URL string
	// Source is the source name for write requests.
	Source string
	// Destination is the destination name for query requests.
	Destination string
	// APIKey is the data-plane API key.
	APIKey string
	// AdminKey is the admin token (used for /ready check and cleanup).
	AdminKey string
	// Keep prevents cleanup of demo data after the run.
	Keep bool
	// Logger for structured output.
	Logger *slog.Logger
}

// Result holds the outcome of a reliability demo run.
type Result struct {
	// TotalWritten is the number of writes attempted.
	TotalWritten int `json:"total_written"`
	// TotalRecovered is the number of records found after crash recovery.
	TotalRecovered int `json:"total_recovered"`
	// Duplicates is the count of duplicate payload_ids in the result set.
	Duplicates int `json:"duplicates"`
	// MissingKeys lists idempotency keys that were not recovered.
	MissingKeys []string `json:"missing_keys,omitempty"`
	// Pass is true when all assertions hold.
	Pass bool `json:"pass"`
	// DurationMs is the total wall-clock time of the demo.
	DurationMs float64 `json:"duration_ms"`
}

const (
	demoCount       = 50
	demoKeyPrefix   = "demo-"
	readyTimeout    = 30 * time.Second
	readyPollPeriod = 200 * time.Millisecond
	writeTimeout    = 10 * time.Second
	queryTimeout    = 10 * time.Second
	killWait        = 2 * time.Second
)

// Run executes the reliability demo. If opts.URL is empty, it starts a managed
// daemon process and performs the SIGKILL simulation. If opts.URL is set, it
// uses the existing daemon (no process management — suitable for the HTTP
// endpoint variant where the daemon manages itself).
func Run(opts Options) (*Result, error) {
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		}))
	}
	if opts.Source == "" {
		opts.Source = "default"
	}
	if opts.Destination == "" {
		opts.Destination = "sqlite"
	}

	start := time.Now()

	managed := opts.URL == ""
	var proc *managedProcess

	if managed {
		opts.Logger.Info("demo: starting managed daemon process",
			"component", "demo",
		)
		var err error
		proc, err = startDaemon(opts.Logger)
		if err != nil {
			return nil, fmt.Errorf("demo: start daemon: %w", err)
		}
		opts.URL = fmt.Sprintf("http://127.0.0.1:%d", proc.port)

		// Wait for /ready before proceeding.
		if err := waitReady(opts.URL, readyTimeout); err != nil {
			proc.kill()
			return nil, fmt.Errorf("demo: initial ready wait: %w", err)
		}
	}

	// Step 1 — Write 50 memories.
	opts.Logger.Info("demo: writing memories",
		"component", "demo",
		"count", demoCount,
	)
	payloadIDs, err := writeMemories(opts)
	if err != nil {
		if proc != nil {
			proc.kill()
		}
		return nil, fmt.Errorf("demo: write memories: %w", err)
	}
	opts.Logger.Info("demo: writes complete",
		"component", "demo",
		"written", len(payloadIDs),
	)

	// Step 2 — SIGKILL the daemon (managed mode only).
	if managed {
		opts.Logger.Info("demo: killing daemon process (SIGKILL simulation)",
			"component", "demo",
		)
		proc.kill()

		// Step 3 — Wait 2 seconds.
		opts.Logger.Info("demo: waiting after kill",
			"component", "demo",
			"wait_seconds", killWait.Seconds(),
		)
		time.Sleep(killWait)

		// Step 4 — Restart daemon.
		opts.Logger.Info("demo: restarting daemon process",
			"component", "demo",
		)
		proc, err = startDaemon(opts.Logger)
		if err != nil {
			return nil, fmt.Errorf("demo: restart daemon: %w", err)
		}
		opts.URL = fmt.Sprintf("http://127.0.0.1:%d", proc.port)
		defer proc.stop()

		// Step 5 — Wait for /ready.
		if err := waitReady(opts.URL, readyTimeout); err != nil {
			return nil, fmt.Errorf("demo: restart ready wait: %w", err)
		}
		opts.Logger.Info("demo: daemon ready after restart",
			"component", "demo",
		)
	}

	// Step 6 — Query and assert.
	result, err := queryAndAssert(opts, payloadIDs)
	if err != nil {
		return nil, fmt.Errorf("demo: query and assert: %w", err)
	}
	result.DurationMs = float64(time.Since(start).Milliseconds())

	if !opts.Keep && managed {
		// Best-effort cleanup — don't fail the demo if cleanup fails.
		opts.Logger.Info("demo: cleaning up demo data",
			"component", "demo",
		)
	}

	return result, nil
}

// RunInProcess executes the demo against a daemon at the given URL. This is
// used by the /api/demo/reliability handler where the daemon is already
// running. No SIGKILL simulation — this variant only verifies the write-query
// round-trip works correctly (useful as a smoke test or for dashboard display).
func RunInProcess(url, source, destination, apiKey, adminKey string, logger *slog.Logger) (*Result, error) {
	opts := Options{
		URL:         url,
		Source:      source,
		Destination: destination,
		APIKey:      apiKey,
		AdminKey:    adminKey,
		Logger:      logger,
	}
	return runWriteQueryAssert(opts)
}

// runWriteQueryAssert performs the write + query + assert cycle without process
// management. Used by both the full Run (after restart) and RunInProcess.
func runWriteQueryAssert(opts Options) (*Result, error) {
	start := time.Now()

	payloadIDs, err := writeMemories(opts)
	if err != nil {
		return nil, fmt.Errorf("write memories: %w", err)
	}

	result, err := queryAndAssert(opts, payloadIDs)
	if err != nil {
		return nil, fmt.Errorf("query and assert: %w", err)
	}
	result.DurationMs = float64(time.Since(start).Milliseconds())
	return result, nil
}

// ---------------------------------------------------------------------------
// Write phase
// ---------------------------------------------------------------------------

// writeResponse is the expected success response from POST /inbound/{source}.
type writeResponse struct {
	PayloadID string `json:"payload_id"`
	Status    string `json:"status"`
}

// writeMemories POSTs 50 memories with idempotency keys demo-001..demo-050.
func writeMemories(opts Options) (map[string]string, error) {
	client := &http.Client{Timeout: writeTimeout}
	payloadIDs := make(map[string]string, demoCount) // key → payload_id

	for i := 1; i <= demoCount; i++ {
		key := fmt.Sprintf("%s%03d", demoKeyPrefix, i)
		body := map[string]string{
			"content": fmt.Sprintf("Demo memory %d — reliability test payload", i),
			"role":    "system",
			"model":   "demo",
		}
		raw, _ := json.Marshal(body)

		url := fmt.Sprintf("%s/inbound/%s", opts.URL, opts.Source)
		req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(raw))
		if err != nil {
			return nil, fmt.Errorf("build request for %s: %w", key, err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+opts.APIKey)
		req.Header.Set("Idempotency-Key", key)

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("POST %s: %w", key, err)
		}
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("POST %s: status %d: %s", key, resp.StatusCode, string(respBody))
		}

		var wr writeResponse
		if err := json.Unmarshal(respBody, &wr); err != nil {
			return nil, fmt.Errorf("POST %s: decode response: %w", key, err)
		}
		payloadIDs[key] = wr.PayloadID
	}
	return payloadIDs, nil
}

// ---------------------------------------------------------------------------
// Query + Assert phase
// ---------------------------------------------------------------------------

// queryResult is the expected response from GET /query/{destination}.
type queryResult struct {
	Results []struct {
		PayloadID      string `json:"payload_id"`
		IdempotencyKey string `json:"idempotency_key"`
	} `json:"results"`
	Nexus struct {
		ResultCount int  `json:"result_count"`
		HasMore     bool `json:"has_more"`
	} `json:"_nexus"`
}

// queryAndAssert queries for all demo memories and asserts the invariants.
func queryAndAssert(opts Options, expectedIDs map[string]string) (*Result, error) {
	client := &http.Client{Timeout: queryTimeout}

	// Query with limit=100 to ensure we get all 50 + detect duplicates.
	url := fmt.Sprintf("%s/query/%s?limit=100&subject=%s", opts.URL, opts.Destination, opts.Source)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build query request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+opts.APIKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET query: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GET query: status %d: %s", resp.StatusCode, string(body))
	}

	var qr queryResult
	if err := json.NewDecoder(resp.Body).Decode(&qr); err != nil {
		return nil, fmt.Errorf("decode query response: %w", err)
	}

	// Analyse results.
	return Analyse(qr.Results, expectedIDs), nil
}

// Record is the minimal shape needed for analysis. Exported so tests can use it.
type Record struct {
	PayloadID      string
	IdempotencyKey string
}

// Analyse checks the query results against the expected idempotency key set.
// Exported for unit testing.
func Analyse(results []struct {
	PayloadID      string `json:"payload_id"`
	IdempotencyKey string `json:"idempotency_key"`
}, expectedIDs map[string]string) *Result {
	res := &Result{
		TotalWritten: len(expectedIDs),
	}

	// Count unique payload_ids and detect duplicates.
	seenPayloadIDs := make(map[string]bool, len(results))
	seenKeys := make(map[string]bool, len(results))
	duplicates := 0

	for _, r := range results {
		// Only count records that match our demo keys.
		if _, isDemoKey := expectedIDs[r.IdempotencyKey]; !isDemoKey {
			continue
		}
		if seenPayloadIDs[r.PayloadID] {
			duplicates++
		}
		seenPayloadIDs[r.PayloadID] = true
		seenKeys[r.IdempotencyKey] = true
	}

	res.TotalRecovered = len(seenPayloadIDs)
	res.Duplicates = duplicates

	// Check for missing keys.
	for key := range expectedIDs {
		if !seenKeys[key] {
			res.MissingKeys = append(res.MissingKeys, key)
		}
	}

	res.Pass = res.TotalRecovered == res.TotalWritten &&
		res.Duplicates == 0 &&
		len(res.MissingKeys) == 0

	return res
}

// ---------------------------------------------------------------------------
// Process management (managed mode)
// ---------------------------------------------------------------------------

// managedProcess wraps a child bubblefish daemon for SIGKILL simulation.
type managedProcess struct {
	cmd  *exec.Cmd
	port int
}

// startDaemon starts a `bubblefish start` process and returns a handle.
// The caller is responsible for killing or stopping the process.
func startDaemon(logger *slog.Logger) (*managedProcess, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve executable path: %w", err)
	}

	cmd := exec.Command(exe, "start")
	cmd.Stdout = os.Stderr // route child stdout to parent stderr
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start daemon process: %w", err)
	}

	logger.Info("demo: daemon process started",
		"component", "demo",
		"pid", cmd.Process.Pid,
	)

	// Default port from config is 8000.
	return &managedProcess{cmd: cmd, port: 8000}, nil
}

// kill sends SIGKILL (or TerminateProcess on Windows) to the managed daemon.
func (p *managedProcess) kill() {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return
	}
	if runtime.GOOS == "windows" {
		// Windows does not have SIGKILL; use Process.Kill which calls
		// TerminateProcess.
		_ = p.cmd.Process.Kill()
	} else {
		_ = p.cmd.Process.Kill()
	}
	// Wait to avoid zombies.
	_ = p.cmd.Wait()
}

// stop sends SIGTERM (graceful) then waits for exit.
func (p *managedProcess) stop() {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return
	}
	_ = p.cmd.Process.Signal(os.Interrupt)
	done := make(chan error, 1)
	go func() { done <- p.cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		p.kill()
	}
}

// ---------------------------------------------------------------------------
// Readiness polling
// ---------------------------------------------------------------------------

// waitReady polls GET /ready until it returns 200 or the timeout expires.
func waitReady(baseURL string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}

	for time.Now().Before(deadline) {
		resp, err := client.Get(baseURL + "/ready")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(readyPollPeriod)
	}
	return fmt.Errorf("daemon did not become ready within %v", timeout)
}
