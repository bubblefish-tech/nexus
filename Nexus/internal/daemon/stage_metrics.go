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
	"sync"
	"sync/atomic"
	"time"
)

type stageMetric struct {
	totalMs atomic.Int64
	count   atomic.Int64
	status  atomic.Value // string: "OK", "SKIP", "ERR"
}

func (s *stageMetric) record(d time.Duration) {
	s.totalMs.Add(d.Microseconds())
	s.count.Add(1)
}

func (s *stageMetric) avgMs() float64 {
	c := s.count.Load()
	if c == 0 {
		return 0
	}
	return float64(s.totalMs.Load()) / float64(c) / 1000.0
}

func (s *stageMetric) getStatus() string {
	v := s.status.Load()
	if v == nil {
		return "OK"
	}
	return v.(string)
}

func (s *stageMetric) hits() int64 {
	return s.count.Load()
}

type pipelineMetrics struct {
	cascadeStages map[string]*stageMetric
	writeStages   map[string]*stageMetric

	writesTotal atomic.Int64
	readsTotal  atomic.Int64
	errorsTotal atomic.Int64

	mu            sync.Mutex
	recentWrites  []time.Time
	recentReads   []time.Time
	recentErrors  []time.Time

	immuneScans      atomic.Int64
	quarantineTotal  atomic.Int64
}

func newPipelineMetrics() *pipelineMetrics {
	pm := &pipelineMetrics{
		cascadeStages: make(map[string]*stageMetric),
		writeStages:   make(map[string]*stageMetric),
	}
	for _, name := range []string{"policy", "exact_cache", "semantic_cache", "structured", "semantic", "hybrid_merge"} {
		pm.cascadeStages[name] = &stageMetric{}
		pm.cascadeStages[name].status.Store("OK")
	}
	for _, name := range []string{"auth", "policy", "idempotency", "rate_limit", "immune_scan", "embedding", "wal_append", "queue_send", "dest_write"} {
		pm.writeStages[name] = &stageMetric{}
		pm.writeStages[name].status.Store("OK")
	}
	return pm
}

func (pm *pipelineMetrics) recordWrite() {
	pm.writesTotal.Add(1)
	pm.mu.Lock()
	pm.recentWrites = append(pm.recentWrites, time.Now())
	pm.mu.Unlock()
}

func (pm *pipelineMetrics) recordRead() {
	pm.readsTotal.Add(1)
	pm.mu.Lock()
	pm.recentReads = append(pm.recentReads, time.Now())
	pm.mu.Unlock()
}

func (pm *pipelineMetrics) recordError() {
	pm.errorsTotal.Add(1)
	pm.mu.Lock()
	pm.recentErrors = append(pm.recentErrors, time.Now())
	pm.mu.Unlock()
}

func (pm *pipelineMetrics) countRecent(entries *[]time.Time, window time.Duration) int {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	cutoff := time.Now().Add(-window)
	n := 0
	for i := len(*entries) - 1; i >= 0; i-- {
		if (*entries)[i].After(cutoff) {
			n++
		} else {
			break
		}
	}
	// Trim old entries
	if len(*entries) > 1000 {
		keep := 0
		for i, t := range *entries {
			if t.After(cutoff) {
				keep = i
				break
			}
		}
		*entries = (*entries)[keep:]
	}
	return n
}

func (pm *pipelineMetrics) writes1m() int {
	return pm.countRecent(&pm.recentWrites, time.Minute)
}

func (pm *pipelineMetrics) reads1m() int {
	return pm.countRecent(&pm.recentReads, time.Minute)
}

func (pm *pipelineMetrics) errors1m() int {
	return pm.countRecent(&pm.recentErrors, time.Minute)
}

func (pm *pipelineMetrics) stageSnapshot(stages map[string]*stageMetric) map[string]map[string]interface{} {
	result := make(map[string]map[string]interface{}, len(stages))
	for name, s := range stages {
		result[name] = map[string]interface{}{
			"status": s.getStatus(),
			"avg_ms": s.avgMs(),
			"hits":   s.hits(),
		}
	}
	return result
}
