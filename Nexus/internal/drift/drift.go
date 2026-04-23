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

// Package drift implements continuous drift detection for BubbleFish Nexus.
// It samples a percentage of delivered WAL entries and verifies they exist in
// the destination, catching WAL-to-destination drift in real time.
//
// Zero effect on the hot path — drift detection runs in its own goroutine with
// its own tick interval.
//
// Reference: v0.1.3 Build Plan Section 6.3.
package drift

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bubblefish-tech/nexus/internal/wal"
)

// WALReader is the WAL interface needed by Drift (subset of wal.WAL).
type WALReader interface {
	SampleDelivered(count int) ([]wal.Entry, error)
}

// DestChecker is the destination interface needed by Drift.
type DestChecker interface {
	Exists(payloadID string) (bool, error)
}

// Config configures a Drift instance.
type Config struct {
	WAL        WALReader
	Dest       DestChecker
	Logger     *slog.Logger
	Interval   time.Duration // default 60s
	SampleSize int           // entries to sample per check (default 10)
}

// Drift performs continuous drift detection by sampling delivered WAL
// entries and verifying they exist in the destination.
type Drift struct {
	wal        WALReader
	dest       DestChecker
	logger     *slog.Logger
	interval   time.Duration
	sampleSize int

	checksTotal    atomic.Int64
	anomaliesTotal atomic.Int64

	stopOnce sync.Once
	stopped  chan struct{}
	wg       sync.WaitGroup
}

// New creates a Drift detector but does not start it. Call Start() next.
func New(cfg Config) *Drift {
	if cfg.Interval <= 0 {
		cfg.Interval = 60 * time.Second
	}
	if cfg.SampleSize <= 0 {
		cfg.SampleSize = 10
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Drift{
		wal:        cfg.WAL,
		dest:       cfg.Dest,
		logger:     cfg.Logger,
		interval:   cfg.Interval,
		sampleSize: cfg.SampleSize,
		stopped:    make(chan struct{}),
	}
}

// Start launches the drift detection background goroutine.
func (d *Drift) Start() {
	d.wg.Add(1)
	go d.loop()
}

// Stop signals the drift detector to stop and waits for it to exit.
// Safe to call multiple times.
func (d *Drift) Stop() {
	d.stopOnce.Do(func() {
		close(d.stopped)
	})
	d.wg.Wait()
}

// ChecksTotal returns the total number of entries checked.
func (d *Drift) ChecksTotal() int64 {
	return d.checksTotal.Load()
}

// AnomaliesTotal returns the total number of anomalies detected.
func (d *Drift) AnomaliesTotal() int64 {
	return d.anomaliesTotal.Load()
}

func (d *Drift) loop() {
	defer d.wg.Done()

	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()

	for {
		select {
		case <-d.stopped:
			return
		case <-ticker.C:
			d.check()
		}
	}
}

func (d *Drift) check() {
	entries, err := d.wal.SampleDelivered(d.sampleSize)
	if err != nil {
		d.logger.Warn("drift: sample delivered entries failed",
			"component", "drift",
			"error", err,
		)
		return
	}

	for _, entry := range entries {
		d.checksTotal.Add(1)

		exists, err := d.dest.Exists(entry.PayloadID)
		if err != nil {
			d.logger.Warn("drift: existence check failed",
				"component", "drift",
				"payload_id", entry.PayloadID,
				"error", err,
			)
			continue
		}

		if !exists {
			d.anomaliesTotal.Add(1)
			d.logger.Warn("drift: ANOMALY — delivered entry missing from destination",
				"component", "drift",
				"payload_id", entry.PayloadID,
				"source", entry.Source,
				"destination", entry.Destination,
			)
		}
	}
}
