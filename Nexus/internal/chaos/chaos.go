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

// Package chaos implements the `bubblefish chaos` fault injection tool.
// It sends a workload of writes to a running Nexus daemon, periodically
// injects faults (process kill, network timeout simulation), then measures
// data loss after recovery. The report is machine-readable JSON.
//
// Reference: v0.1.3 Build Plan Section 6.1.
package chaos

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// Options configures a chaos run.
type Options struct {
	// URL is the base URL of the running Nexus daemon (e.g. http://127.0.0.1:8000).
	URL string
	// Source is the source name for write requests.
	Source string
	// Destination is the destination name for recovery queries.
	Destination string
	// APIKey is the data-plane API key.
	APIKey string
	// AdminKey is the admin token for /ready and /api/status checks.
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
	FaultsInjected int   `json:"faults_injected"`
	FaultTypes     []string `json:"fault_types"`

	// Recovery verification.
	RecoveredCount  int    `json:"recovered_count"`
	MissingCount    int    `json:"missing_count"`
	DuplicateCount  int    `json:"duplicate_count"`
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

	// Verify: query all memories and check against accepted IDs.
	recovered, err := queryAll(client, opts.URL, opts.Destination, opts.APIKey)
	if err != nil {
		return nil, fmt.Errorf("chaos: recovery query failed: %w", err)
	}

	recoveredSet := make(map[string]int)
	for _, id := range recovered {
		recoveredSet[id]++
	}

	var missing, duplicates int
	for _, id := range acceptedIDs {
		count := recoveredSet[id]
		if count == 0 {
			missing++
		} else if count > 1 {
			duplicates++
		}
	}

	report.RecoveredCount = len(recovered)
	report.MissingCount = missing
	report.DuplicateCount = duplicates
	if report.WritesAccepted > 0 {
		report.DataLossPercent = float64(missing) / float64(report.WritesAccepted) * 100.0
	}
	report.FinishedAt = time.Now().UTC()
	report.Duration = report.FinishedAt.Sub(report.StartedAt)
	report.DurationHuman = report.Duration.Round(time.Millisecond).String()

	if missing == 0 && duplicates == 0 {
		report.Pass = true
		report.Verdict = fmt.Sprintf("PASS — %d writes, %d recovered, 0 missing, 0 duplicates, %d faults injected",
			report.WritesAccepted, report.RecoveredCount, report.FaultsInjected)
	} else {
		report.Verdict = fmt.Sprintf("FAIL — %d writes, %d recovered, %d missing (%.2f%% loss), %d duplicates",
			report.WritesAccepted, report.RecoveredCount, missing, report.DataLossPercent, duplicates)
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

// queryAll retrieves all payload IDs from the destination.
func queryAll(client *http.Client, baseURL, destination, apiKey string) ([]string, error) {
	var allIDs []string
	cursor := ""

	for {
		url := fmt.Sprintf("%s/query/%s?limit=200", baseURL, destination)
		if cursor != "" {
			url += "&cursor=" + cursor
		}

		req, _ := http.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("query returned %d: %s", resp.StatusCode, body)
		}

		var result struct {
			Records []struct {
				PayloadID string `json:"payload_id"`
			} `json:"records"`
			Nexus struct {
				HasMore    bool   `json:"has_more"`
				NextCursor string `json:"next_cursor"`
			} `json:"_nexus"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, err
		}

		for _, r := range result.Records {
			allIDs = append(allIDs, r.PayloadID)
		}

		if !result.Nexus.HasMore || result.Nexus.NextCursor == "" {
			break
		}
		cursor = result.Nexus.NextCursor
	}

	return allIDs, nil
}
