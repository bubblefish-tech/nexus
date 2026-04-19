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

// Package sentinel implements continuous drift detection for BubbleFish Nexus.
// It samples a percentage of delivered WAL entries and verifies they exist in
// the destination, catching WAL-to-destination drift in real time.
//
// Zero effect on the hot path — sentinel runs in its own goroutine with its
// own tick interval.
//
// Reference: v0.1.3 Build Plan Section 6.3.
package sentinel

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bubblefish-tech/nexus/internal/wal"
)

// WALReader is the WAL interface needed by Sentinel (subset of wal.WAL).
type WALReader interface {
	SampleDelivered(count int) ([]wal.Entry, error)
}

// DestChecker is the destination interface needed by Sentinel.
type DestChecker interface {
	Exists(payloadID string) (bool, error)
}

// Config configures a Sentinel instance.
type Config struct {
	WAL        WALReader
	Dest       DestChecker
	Logger     *slog.Logger
	Interval   time.Duration // default 60s
	SampleSize int           // entries to sample per check (default 10)
}

// Sentinel performs continuous drift detection by sampling delivered WAL
// entries and verifying they exist in the destination.
type Sentinel struct {
	wal        WALReader
	dest       DestChecker
	logger     *slog.Logger
	interval   time.Duration
	sampleSize int

	checksTotal   atomic.Int64
	anomaliesTotal atomic.Int64

	stopOnce sync.Once
	stopped  chan struct{}
	wg       sync.WaitGroup
}

// New creates a Sentinel but does not start it. Call Start() next.
func New(cfg Config) *Sentinel {
	if cfg.Interval <= 0 {
		cfg.Interval = 60 * time.Second
	}
	if cfg.SampleSize <= 0 {
		cfg.SampleSize = 10
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Sentinel{
		wal:        cfg.WAL,
		dest:       cfg.Dest,
		logger:     cfg.Logger,
		interval:   cfg.Interval,
		sampleSize: cfg.SampleSize,
		stopped:    make(chan struct{}),
	}
}

// Start launches the sentinel background goroutine.
func (s *Sentinel) Start() {
	s.wg.Add(1)
	go s.loop()
}

// Stop signals the sentinel to stop and waits for it to exit.
// Safe to call multiple times.
func (s *Sentinel) Stop() {
	s.stopOnce.Do(func() {
		close(s.stopped)
	})
	s.wg.Wait()
}

// ChecksTotal returns the total number of entries checked.
func (s *Sentinel) ChecksTotal() int64 {
	return s.checksTotal.Load()
}

// AnomaliesTotal returns the total number of anomalies detected.
func (s *Sentinel) AnomaliesTotal() int64 {
	return s.anomaliesTotal.Load()
}

func (s *Sentinel) loop() {
	defer s.wg.Done()

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopped:
			return
		case <-ticker.C:
			s.check()
		}
	}
}

func (s *Sentinel) check() {
	entries, err := s.wal.SampleDelivered(s.sampleSize)
	if err != nil {
		s.logger.Warn("sentinel: sample delivered entries failed",
			"component", "sentinel",
			"error", err,
		)
		return
	}

	for _, entry := range entries {
		s.checksTotal.Add(1)

		exists, err := s.dest.Exists(entry.PayloadID)
		if err != nil {
			s.logger.Warn("sentinel: existence check failed",
				"component", "sentinel",
				"payload_id", entry.PayloadID,
				"error", err,
			)
			continue
		}

		if !exists {
			s.anomaliesTotal.Add(1)
			s.logger.Warn("sentinel: ANOMALY — delivered entry missing from destination",
				"component", "sentinel",
				"payload_id", entry.PayloadID,
				"source", entry.Source,
				"destination", entry.Destination,
			)
		}
	}
}
