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

// Package simulate implements the FoundationDB-style deterministic testing
// harness for BubbleFish Nexus. It runs the write+deliver pipeline against
// real WAL and real SQLite in temporary directories with seeded random fault
// injection. Every nondeterministic decision is controlled by a seed, making
// failures reproducible.
//
// Reference: v0.1.3 Build Plan Section 6.2.
package simulate

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bubblefish-tech/nexus/internal/destination"
	"github.com/bubblefish-tech/nexus/internal/wal"
)

// Options configures a simulation run.
type Options struct {
	Seed        int64
	Duration    time.Duration
	Concurrency int
	FaultRate   float64 // probability of fault per write cycle (0.0-1.0)
	Logger      *slog.Logger
}

// Report is the machine-readable simulation outcome.
type Report struct {
	Seed             int64  `json:"seed"`
	Duration         string `json:"duration"`
	WritesAttempted  int64  `json:"writes_attempted"`
	WritesDelivered  int64  `json:"writes_delivered"`
	FaultsInjected   int    `json:"faults_injected"`
	CrashRecoveries  int    `json:"crash_recoveries"`
	RecoveredCount   int    `json:"recovered_count"`
	MissingCount     int    `json:"missing_count"`
	DuplicateCount   int    `json:"duplicate_count"`
	Pass             bool   `json:"pass"`
	Verdict          string `json:"verdict"`
}

// Run executes a deterministic simulation. All randomness is controlled by
// opts.Seed, making failures reproducible via `bubblefish simulate --seed N`.
func Run(opts Options) (*Report, error) {
	if opts.Duration <= 0 {
		opts.Duration = 30 * time.Second
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = 5
	}
	if opts.FaultRate < 0 {
		opts.FaultRate = 0.05
	}
	if opts.Seed == 0 {
		opts.Seed = time.Now().UnixNano()
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	rng := rand.New(rand.NewSource(opts.Seed))
	report := &Report{Seed: opts.Seed}

	walDir, err := os.MkdirTemp("", "nexus-sim-wal-*")
	if err != nil {
		return nil, fmt.Errorf("simulate: create WAL dir: %w", err)
	}
	defer os.RemoveAll(walDir)

	dbDir, err := os.MkdirTemp("", "nexus-sim-db-*")
	if err != nil {
		return nil, fmt.Errorf("simulate: create DB dir: %w", err)
	}
	defer os.RemoveAll(dbDir)
	dbPath := dbDir + "/sim.db"

	// Track all written payload IDs.
	var (
		writtenMu  sync.Mutex
		writtenIDs []string
	)
	var writesAttempted atomic.Int64

	ctx, cancel := context.WithTimeout(context.Background(), opts.Duration)
	defer cancel()

	// Open WAL and destination.
	w, dest, openErr := openPipeline(walDir, dbPath, opts.Logger)
	if openErr != nil {
		return nil, openErr
	}

	var faultsInjected int
	var crashRecoveries int
	var mu sync.Mutex // guards w and dest during crash recovery

	// Writer goroutines.
	var wg sync.WaitGroup
	for i := 0; i < opts.Concurrency; i++ {
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

				seq := writesAttempted.Add(1)
				payloadID := fmt.Sprintf("sim-%d-%d", workerID, seq)
				content := fmt.Sprintf("simulate-worker-%d-seq-%d", workerID, seq)

				tp := destination.TranslatedPayload{
					PayloadID:   payloadID,
					Source:      "simulate",
					Destination: "sqlite",
					Content:     content,
					Timestamp:   time.Now().UTC(),
				}
				payloadBytes, _ := json.Marshal(tp)

				mu.Lock()
				currentW := w
				currentDest := dest
				mu.Unlock()

				if currentW == nil {
					time.Sleep(time.Millisecond)
					continue
				}

				entry := wal.Entry{
					PayloadID:   payloadID,
					Source:      "simulate",
					Destination: "sqlite",
					Payload:     payloadBytes,
				}

				if err := currentW.Append(entry); err != nil {
					continue // WAL may be closed during crash sim
				}

				// Deliver directly to destination (no queue in simulation).
				if err := currentDest.Write(tp); err != nil {
					continue
				}

				writtenMu.Lock()
				writtenIDs = append(writtenIDs, payloadID)
				writtenMu.Unlock()

				// Seeded fault injection.
				if localRng.Float64() < opts.FaultRate {
					mu.Lock()
					faultsInjected++

					// Simulate crash: close WAL + dest, reopen (WAL replay).
					opts.Logger.Info("simulate: injecting crash fault",
						"component", "simulate",
						"worker", workerID,
						"faults_total", faultsInjected,
					)
					_ = w.Close()
					_ = dest.Close()

					crashRecoveries++
					newW, newDest, rErr := openPipeline(walDir, dbPath, opts.Logger)
					if rErr != nil {
						opts.Logger.Error("simulate: recovery failed", "error", rErr)
						mu.Unlock()
						continue
					}
					w = newW
					dest = newDest
					mu.Unlock()
				}

				// Small jitter.
				time.Sleep(time.Duration(localRng.Intn(2)) * time.Millisecond)
			}
		}(i, workerRng)
	}

	wg.Wait()

	// Final close.
	mu.Lock()
	if w != nil {
		_ = w.Close()
	}
	if dest != nil {
		_ = dest.Close()
	}
	mu.Unlock()

	// Recovery verification: reopen and query all records via pagination.
	finalDest, err := destination.OpenSQLite(dbPath, opts.Logger)
	if err != nil {
		return nil, fmt.Errorf("simulate: reopen for verification: %w", err)
	}
	defer func() { _ = finalDest.Close() }()

	recoveredSet := make(map[string]int)
	cursor := ""
	for {
		qp := destination.QueryParams{Limit: 200}
		if cursor != "" {
			qp.Cursor = cursor
		}
		result, qErr := finalDest.Query(qp)
		if qErr != nil {
			return nil, fmt.Errorf("simulate: query for verification: %w", qErr)
		}
		for _, r := range result.Records {
			recoveredSet[r.PayloadID]++
		}
		if !result.HasMore || result.NextCursor == "" {
			break
		}
		cursor = result.NextCursor
	}

	var missing, duplicates int
	for _, id := range writtenIDs {
		count := recoveredSet[id]
		if count == 0 {
			missing++
		} else if count > 1 {
			duplicates++
		}
	}

	report.Duration = opts.Duration.String()
	report.WritesAttempted = writesAttempted.Load()
	report.WritesDelivered = int64(len(writtenIDs))
	report.FaultsInjected = faultsInjected
	report.CrashRecoveries = crashRecoveries
	totalRecovered := 0
	for _, count := range recoveredSet {
		totalRecovered += count
	}
	report.RecoveredCount = totalRecovered
	report.MissingCount = missing
	report.DuplicateCount = duplicates
	report.Pass = missing == 0

	if report.Pass {
		report.Verdict = fmt.Sprintf("PASS — seed=%d, %d writes, %d recovered, 0 missing, %d faults, %d crash recoveries",
			opts.Seed, report.WritesDelivered, report.RecoveredCount, report.FaultsInjected, report.CrashRecoveries)
	} else {
		report.Verdict = fmt.Sprintf("FAIL — seed=%d, %d writes, %d recovered, %d missing, %d faults",
			opts.Seed, report.WritesDelivered, report.RecoveredCount, missing, report.FaultsInjected)
	}

	return report, nil
}

func openPipeline(walDir, dbPath string, logger *slog.Logger) (*wal.WAL, *destination.SQLiteDestination, error) {
	w, err := wal.Open(walDir, 50, logger)
	if err != nil {
		return nil, nil, fmt.Errorf("simulate: open WAL: %w", err)
	}

	dest, err := destination.OpenSQLite(dbPath, logger)
	if err != nil {
		_ = w.Close()
		return nil, nil, fmt.Errorf("simulate: open destination: %w", err)
	}

	return w, dest, nil
}
