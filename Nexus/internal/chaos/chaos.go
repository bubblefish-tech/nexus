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

// Package chaos implements the `nexus chaos` fault injection tool.
// It sends a workload of writes to a running Nexus daemon, periodically
// injects faults (process kill, network timeout simulation), then measures
// data loss after recovery. The report is machine-readable JSON.
//
// Reference: v0.1.3 Build Plan Section 6.1.
package chaos

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite" // SQLite driver for direct DB verification.
)

// Options configures a chaos run.
type Options struct {
	// URL is the base URL of the running Nexus daemon (e.g. http://127.0.0.1:8000).
	URL string
	// Source is the source name for write requests.
	Source string
	// DBPath is the path to memories.db for direct DB verification (ground truth).
	DBPath string
	// APIKey is the data-plane API key.
	APIKey string
	// AdminKey is the admin token for /ready, /api/status, and /admin/memories.
	AdminKey string
	// Duration is how long to run the chaos test.
	Duration time.Duration
	// Concurrency is the number of concurrent writer goroutines.
	Concurrency int
	// FaultInterval is the approximate time between fault injections.
	FaultInterval time.Duration
	// Seed controls deterministic fault scheduling. 0 uses a random seed.
	Seed int64
	// ReportFile is the output path for the JSON report. Empty means stdout.
	ReportFile string
	// Logger for structured output.
	Logger *slog.Logger
}

// Report is the machine-readable chaos test outcome.
type Report struct {
	StartedAt     time.Time     `json:"started_at"`
	FinishedAt    time.Time     `json:"finished_at"`
	Duration      time.Duration `json:"duration_ns"`
	DurationHuman string        `json:"duration"`
	Seed          int64         `json:"seed"`

	// Workload stats.
	WritesAttempted int64 `json:"writes_attempted"`
	WritesAccepted  int64 `json:"writes_accepted"`
	WritesFailed    int64 `json:"writes_failed"`

	// Fault injection stats.
	FaultsInjected int      `json:"faults_injected"`
	FaultTypes     []string `json:"fault_types"`

	// Path A — DB ground truth.
	DBRecoveredCount int `json:"db_recovered_count"`

	// Path B — admin HTTP read path.
	HTTPRecoveredCount int `json:"http_recovered_count"`

	// Cross-checks.
	AcceptedNotInDB   int `json:"accepted_not_in_db"`   // durability bug
	AcceptedNotInHTTP int `json:"accepted_not_in_http"` // read-path bug
	DBNotInHTTP       int `json:"db_not_in_http"`       // read-path / cursor bug
	HTTPNotInDB       int `json:"http_not_in_db"`       // phantom data

	// Backwards-compat fields.
	RecoveredCount  int     `json:"recovered_count"`
	MissingCount    int     `json:"missing_count"`
	DuplicateCount  int     `json:"duplicate_count"`
	DataLossPercent float64 `json:"data_loss_percent"`

	// Verdict.
	Pass    bool   `json:"pass"`
	Verdict string `json:"verdict"`
}

// Run executes the chaos test. It is the main entry point.
func Run(opts Options) (*Report, error) {
	if opts.URL == "" {
		return nil, fmt.Errorf("chaos: --url is required")
	}
	if opts.APIKey == "" {
		return nil, fmt.Errorf("chaos: --api-key is required")
	}
	if opts.DBPath == "" {
		return nil, fmt.Errorf("chaos: --db is required (path to memories.db)")
	}
	if opts.AdminKey == "" {
		return nil, fmt.Errorf("chaos: --admin-key is required for /admin/memories verification")
	}
	if opts.Duration <= 0 {
		opts.Duration = 60 * time.Second
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = 5
	}
	if opts.FaultInterval <= 0 {
		opts.FaultInterval = 10 * time.Second
	}
	if opts.Seed == 0 {
		opts.Seed = time.Now().UnixNano()
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	rng := rand.New(rand.NewSource(opts.Seed))
	client := &http.Client{Timeout: 5 * time.Second}

	report := &Report{
		StartedAt: time.Now().UTC(),
		Seed:      opts.Seed,
	}

	// Track all accepted payload IDs for verification.
	var (
		acceptedMu  sync.Mutex
		acceptedIDs []string
	)

	var writesAttempted, writesAccepted, writesFailed atomic.Int64

	ctx, cancel := context.WithTimeout(context.Background(), opts.Duration)
	defer cancel()

	// Start writer goroutines.
	var wg sync.WaitGroup
	for w := 0; w < opts.Concurrency; w++ {
		wg.Add(1)
		// Each goroutine gets its own rng to avoid data races on math/rand.Rand.
		workerRng := rand.New(rand.NewSource(rng.Int63()))
		go func(workerID int, localRng *rand.Rand) {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}

				writesAttempted.Add(1)
				payloadID, err := sendWrite(client, opts.URL, opts.Source, opts.APIKey, workerID, writesAttempted.Load())
				if err != nil {
					writesFailed.Add(1)
					continue
				}
				writesAccepted.Add(1)
				acceptedMu.Lock()
				acceptedIDs = append(acceptedIDs, payloadID)
				acceptedMu.Unlock()

				// Small random delay between writes (1-10ms).
				time.Sleep(time.Duration(1+localRng.Intn(10)) * time.Millisecond)
			}
		}(w, workerRng)
	}

	// Start fault injector goroutine.
	var faultTypes []string
	var faultsInjected int
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(opts.FaultInterval + time.Duration(rng.Int63n(int64(opts.FaultInterval/2)))):
			}

			fault := injectFault(client, opts, rng)
			faultsInjected++
			faultTypes = append(faultTypes, fault)
			opts.Logger.Info("chaos: fault injected",
				"component", "chaos",
				"fault", fault,
				"total_faults", faultsInjected,
			)
		}
	}()

	// Wait for duration to expire.
	wg.Wait()

	report.WritesAttempted = writesAttempted.Load()
	report.WritesAccepted = writesAccepted.Load()
	report.WritesFailed = writesFailed.Load()
	report.FaultsInjected = faultsInjected
	report.FaultTypes = faultTypes

	opts.Logger.Info("chaos: workload complete — verifying recovery",
		"component", "chaos",
		"writes_accepted", report.WritesAccepted,
		"faults_injected", report.FaultsInjected,
	)

	// Wait for daemon to be ready (it may have been killed).
	if err := waitForReady(client, opts.URL, opts.AdminKey, 30*time.Second); err != nil {
		opts.Logger.Warn("chaos: daemon not ready after workload — recovery check may be incomplete",
			"component", "chaos",
			"error", err,
		)
	}

	// Wait for WAL + queue to drain before reading the DB.
	if err := waitForDrain(client, opts.URL, opts.AdminKey, 30*time.Second, opts.Logger); err != nil {
		opts.Logger.Warn("chaos: queue did not fully drain — some accepted writes may not yet be in SQLite",
			"component", "chaos",
			"error", err,
		)
	}

	// Path A: ground truth from SQLite directly.
	opts.Logger.Info("chaos: verifying against DB (ground truth)", "component", "chaos", "db", opts.DBPath)
	dbSet, err := verifyAgainstDB(opts.DBPath)
	if err != nil {
		return nil, fmt.Errorf("chaos: DB verification failed: %w", err)
	}

	// Path B: HTTP admin list endpoint.
	opts.Logger.Info("chaos: verifying against admin API", "component", "chaos", "url", opts.URL)
	httpSet, duplicates, err := verifyAgainstAdminList(client, opts.URL, opts.AdminKey)
	if err != nil {
		return nil, fmt.Errorf("chaos: admin API verification failed: %w", err)
	}

	// Build accepted set.
	acceptedSet := make(map[string]bool, len(acceptedIDs))
	for _, id := range acceptedIDs {
		acceptedSet[id] = true
	}

	// Set differences.
	acceptedNotInDB := 0
	for id := range acceptedSet {
		if !dbSet[id] {
			acceptedNotInDB++
		}
	}
	acceptedNotInHTTP := 0
	for id := range acceptedSet {
		if !httpSet[id] {
			acceptedNotInHTTP++
		}
	}
	dbNotInHTTP := 0
	for id := range dbSet {
		if !httpSet[id] {
			dbNotInHTTP++
		}
	}
	httpNotInDB := 0
	for id := range httpSet {
		if !dbSet[id] {
			httpNotInDB++
		}
	}

	report.DBRecoveredCount = len(dbSet)
	report.HTTPRecoveredCount = len(httpSet)
	report.AcceptedNotInDB = acceptedNotInDB
	report.AcceptedNotInHTTP = acceptedNotInHTTP
	report.DBNotInHTTP = dbNotInHTTP
	report.HTTPNotInDB = httpNotInDB
	report.DuplicateCount = duplicates

	// Backwards-compat fields.
	report.RecoveredCount = len(dbSet)
	report.MissingCount = acceptedNotInDB
	if report.WritesAccepted > 0 {
		report.DataLossPercent = float64(acceptedNotInDB) / float64(report.WritesAccepted) * 100.0
	}

	report.FinishedAt = time.Now().UTC()
	report.Duration = report.FinishedAt.Sub(report.StartedAt)
	report.DurationHuman = report.Duration.Round(time.Millisecond).String()

	pass := report.AcceptedNotInDB == 0 &&
		report.AcceptedNotInHTTP == 0 &&
		report.DBNotInHTTP == 0 &&
		report.HTTPNotInDB == 0 &&
		report.DuplicateCount == 0
	report.Pass = pass

	if pass {
		report.Verdict = fmt.Sprintf("PASS — %d writes accepted, %d in DB, %d via admin API, all sets agree, %d faults injected",
			report.WritesAccepted, report.DBRecoveredCount, report.HTTPRecoveredCount, report.FaultsInjected)
	} else {
		var parts []string
		if report.AcceptedNotInDB > 0 {
			parts = append(parts, fmt.Sprintf("DURABILITY BUG: %d accepted writes missing from DB", report.AcceptedNotInDB))
		}
		if report.AcceptedNotInHTTP > 0 {
			parts = append(parts, fmt.Sprintf("READ-PATH BUG: %d accepted writes missing from admin API", report.AcceptedNotInHTTP))
		}
		if report.DBNotInHTTP > 0 {
			parts = append(parts, fmt.Sprintf("READ-PATH BUG: %d DB rows not returned by admin API", report.DBNotInHTTP))
		}
		if report.HTTPNotInDB > 0 {
			parts = append(parts, fmt.Sprintf("PHANTOM DATA: %d admin API rows not in DB", report.HTTPNotInDB))
		}
		if report.DuplicateCount > 0 {
			parts = append(parts, fmt.Sprintf("CURSOR INSTABILITY: %d duplicates in admin API pagination", report.DuplicateCount))
		}
		report.Verdict = "FAIL — " + strings.Join(parts, "; ")
	}

	return report, nil
}

// sendWrite sends a single write to the daemon and returns the payload_id.
func sendWrite(client *http.Client, baseURL, source, apiKey string, workerID int, seqNum int64) (string, error) {
	body := map[string]interface{}{
		"content":    fmt.Sprintf("chaos-worker-%d-seq-%d-%d", workerID, seqNum, time.Now().UnixNano()),
		"collection": "chaos-test",
	}
	data, _ := json.Marshal(body)

	url := fmt.Sprintf("%s/inbound/%s", baseURL, source)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("write returned %d: %s", resp.StatusCode, respBody)
	}

	var result struct {
		PayloadID string `json:"payload_id"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", err
	}
	return result.PayloadID, nil
}

// injectFault picks a random fault type and executes it.
func injectFault(client *http.Client, opts Options, rng *rand.Rand) string {
	faults := []string{"network_timeout", "connection_reset", "slow_write"}
	fault := faults[rng.Intn(len(faults))]

	switch fault {
	case "network_timeout":
		// Send a write with a very short timeout to simulate network drop.
		shortClient := &http.Client{Timeout: 1 * time.Millisecond}
		body := []byte(`{"content":"chaos-fault-network-timeout","collection":"chaos-test"}`)
		url := fmt.Sprintf("%s/inbound/%s", opts.URL, opts.Source)
		req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+opts.APIKey)
		_, _ = shortClient.Do(req) // expected to fail

	case "connection_reset":
		// Open a connection and close it immediately mid-request.
		body := bytes.Repeat([]byte("x"), 1024*1024) // 1MB body
		url := fmt.Sprintf("%s/inbound/%s", opts.URL, opts.Source)
		req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+opts.APIKey)
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
		defer cancel()
		req = req.WithContext(ctx)
		_, _ = client.Do(req) // expected to fail

	case "slow_write":
		// Burst of concurrent writes to stress the group commit path.
		var wg sync.WaitGroup
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				body := []byte(fmt.Sprintf(`{"content":"chaos-burst-%d","collection":"chaos-test"}`, i))
				url := fmt.Sprintf("%s/inbound/%s", opts.URL, opts.Source)
				req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer "+opts.APIKey)
				_, _ = client.Do(req)
			}(i)
		}
		wg.Wait()
	}

	return fault
}

// waitForReady polls /ready until the daemon responds 200 or timeout elapses.
func waitForReady(client *http.Client, baseURL, adminKey string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest(http.MethodGet, baseURL+"/ready", nil)
		if adminKey != "" {
			req.Header.Set("Authorization", "Bearer "+adminKey)
		}
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("daemon not ready after %s", timeout)
}

// waitForDrain polls /api/status until queue_depth and wal.pending_entries both
// reach 0, meaning all accepted writes have been flushed to SQLite.
func waitForDrain(client *http.Client, baseURL, adminKey string, timeout time.Duration, logger *slog.Logger) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest(http.MethodGet, baseURL+"/api/status", nil)
		if adminKey != "" {
			req.Header.Set("Authorization", "Bearer "+adminKey)
		}
		resp, err := client.Do(req)
		if err != nil {
			time.Sleep(500 * time.Millisecond)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			time.Sleep(500 * time.Millisecond)
			continue
		}

		var status struct {
			QueueDepth int64 `json:"queue_depth"`
			WAL        struct {
				PendingEntries int64 `json:"pending_entries"`
			} `json:"wal"`
		}
		if err := json.Unmarshal(body, &status); err != nil {
			time.Sleep(500 * time.Millisecond)
			continue
		}

		if status.QueueDepth == 0 && status.WAL.PendingEntries == 0 {
			logger.Info("chaos: queue drained",
				"component", "chaos",
				"queue_depth", status.QueueDepth,
				"wal_pending", status.WAL.PendingEntries,
			)
			return nil
		}

		logger.Info("chaos: waiting for queue drain",
			"component", "chaos",
			"queue_depth", status.QueueDepth,
			"wal_pending", status.WAL.PendingEntries,
		)
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("queue did not drain within %s", timeout)
}

// verifyAgainstDB opens memories.db read-only and returns the set of all payload_ids.
// This is ground truth for the durability claim.
func verifyAgainstDB(dbPath string) (map[string]bool, error) {
	db, err := sql.Open("sqlite", dbPath+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("verifyAgainstDB: open: %w", err)
	}
	defer db.Close()

	rows, err := db.Query("SELECT payload_id FROM memories")
	if err != nil {
		return nil, fmt.Errorf("verifyAgainstDB: query: %w", err)
	}
	defer rows.Close()

	set := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("verifyAgainstDB: scan: %w", err)
		}
		set[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("verifyAgainstDB: rows: %w", err)
	}
	return set, nil
}

// verifyAgainstAdminList paginates through GET /admin/memories and returns the
// set of payload_ids plus the count of duplicates encountered.
func verifyAgainstAdminList(client *http.Client, baseURL, adminKey string) (map[string]bool, int, error) {
	set := make(map[string]bool)
	duplicates := 0
	cursor := ""

	for {
		url := fmt.Sprintf("%s/admin/memories?limit=500", baseURL)
		if cursor != "" {
			url += "&cursor=" + cursor
		}
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return nil, 0, fmt.Errorf("verifyAgainstAdminList: new request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+adminKey)

		resp, err := client.Do(req)
		if err != nil {
			return nil, 0, fmt.Errorf("verifyAgainstAdminList: do: %w", err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, 0, fmt.Errorf("verifyAgainstAdminList: status %d: %s", resp.StatusCode, body)
		}

		var result struct {
			Memories []struct {
				PayloadID string `json:"payload_id"`
			} `json:"memories"`
			Admin struct {
				HasMore    bool   `json:"has_more"`
				NextCursor string `json:"next_cursor"`
			} `json:"_admin"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, 0, fmt.Errorf("verifyAgainstAdminList: decode: %w (body=%s)", err, string(body))
		}

		for _, m := range result.Memories {
			if set[m.PayloadID] {
				duplicates++
			}
			set[m.PayloadID] = true
		}

		if !result.Admin.HasMore || result.Admin.NextCursor == "" {
			break
		}
		cursor = result.Admin.NextCursor
	}
	return set, duplicates, nil
}
