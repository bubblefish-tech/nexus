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
	"math"
	"time"
)

// consistencyChecker is a background goroutine that periodically samples
// DELIVERED WAL entries and verifies they exist in the destination. The
// resulting score (found / sampled) is exposed via Prometheus and /api/status.
//
// This goroutine is read-only. It NEVER modifies WAL or destination data.
// Reference: Tech Spec Section 11.5.
func (d *Daemon) consistencyChecker() {
	cfg := d.getConfig()
	interval := cfg.Consistency.IntervalSeconds
	if interval <= 0 {
		interval = 300
	}
	sampleSize := cfg.Consistency.SampleSize
	if sampleSize <= 0 {
		sampleSize = 100
	}

	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-d.stopped:
			return
		case <-ticker.C:
			d.runConsistencyCheck(sampleSize)
		}
	}
}

// runConsistencyCheck performs a single consistency check iteration: sample
// DELIVERED entries from the WAL, query the destination for each, compute
// the score, update metrics, and log appropriately.
//
// Read-only. NEVER modifies WAL or destination data.
// Reference: Tech Spec Section 11.5.
func (d *Daemon) runConsistencyCheck(sampleSize int) {
	if d.wal == nil || d.dest == nil {
		return
	}

	entries, err := d.wal.SampleDelivered(sampleSize)
	if err != nil {
		d.logger.Error("consistency: failed to sample delivered entries",
			"component", "consistency",
			"error", err,
		)
		return
	}

	sampled := len(entries)
	if sampled == 0 {
		// No delivered entries to check — score is trivially 1.0.
		d.metrics.ConsistencyScore.Set(1.0)
		d.consistencyScore.Store(math.Float64bits(1.0))
		d.logger.Info("consistency: no delivered entries to sample",
			"component", "consistency",
		)
		return
	}

	found := 0
	for _, entry := range entries {
		existsChecker, ok := d.dest.(interface{ Exists(string) (bool, error) })
		if !ok {
			d.logger.Debug("consistency: destination does not support Exists; skipping",
				"component", "consistency")
			return
		}
		exists, err := existsChecker.Exists(entry.PayloadID)
		if err != nil {
			d.logger.Warn("consistency: destination exists check failed",
				"component", "consistency",
				"payload_id", entry.PayloadID,
				"error", err,
			)
			continue
		}
		if exists {
			found++
		}
	}

	score := float64(found) / float64(sampled)
	d.metrics.ConsistencyScore.Set(score)
	d.consistencyScore.Store(math.Float64bits(score))

	switch {
	case score < 0.80:
		d.logger.Error("consistency: score critically low",
			"component", "consistency",
			"score", score,
			"found", found,
			"sampled", sampled,
		)
	case score < 0.95:
		d.logger.Warn("consistency: score below threshold",
			"component", "consistency",
			"score", score,
			"found", found,
			"sampled", sampled,
		)
	default:
		d.logger.Info("consistency: check complete",
			"component", "consistency",
			"score", score,
			"found", found,
			"sampled", sampled,
		)
	}
}
