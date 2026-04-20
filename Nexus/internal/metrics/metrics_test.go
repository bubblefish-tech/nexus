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

package metrics_test

import (
	"strings"
	"testing"

	dto "github.com/prometheus/client_model/go"

	"github.com/bubblefish-tech/nexus/internal/metrics"
)

// TestMultipleRegistriesNoPanic verifies that creating multiple Metrics
// instances does not panic. This is the core invariant: private registries
// never collide with prometheus.DefaultRegisterer.
//
// Reference: Phase 0D Behavioral Contract item 1.
func TestMultipleRegistriesNoPanic(t *testing.T) {
	// Creating many instances must never panic.
	instances := make([]*metrics.Metrics, 5)
	for i := range instances {
		instances[i] = metrics.New()
		if instances[i] == nil {
			t.Fatalf("New() returned nil at index %d", i)
		}
	}

	// All private registries are independent — gathering from one does not
	// affect others.
	for i, m := range instances {
		mfs, err := m.Registry().Gather()
		if err != nil {
			t.Errorf("instance %d: Gather() error: %v", i, err)
		}
		if len(mfs) == 0 {
			t.Errorf("instance %d: Gather() returned no metric families", i)
		}
	}
}

// TestMetricsNonZeroAfterExercise verifies that the throughput, latency, and
// queue metrics are non-zero after exercising the write and read paths.
//
// Reference: Phase 0D Behavioral Contract item 3, Verification Gate.
func TestMetricsNonZeroAfterExercise(t *testing.T) {
	m := metrics.New()

	// Simulate 10 writes.
	for i := 0; i < 10; i++ {
		m.ThroughputPerSource.WithLabelValues("test-source").Inc()
		m.PayloadProcessingLatency.WithLabelValues("test-source").Observe(0.001)
		m.QueueProcessingRate.Inc()
	}

	// Simulate 5 reads.
	for i := 0; i < 5; i++ {
		m.ReadLatency.WithLabelValues("test-source", "/query").Observe(0.0005)
	}

	// Queue depth set to non-zero.
	m.QueueDepth.Set(3)

	// WAL metrics.
	m.WALPendingEntries.Set(10)
	m.WALHealthy.Set(1)
	m.WALDiskBytesFree.Set(1e9)
	m.WALAppendLatency.Observe(0.0001)
	m.ReplayEntriesTotal.Add(10)
	m.ReplayDurationSeconds.Set(0.05)

	// Gather and assert.
	mfs, err := m.Registry().Gather()
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	wantNonZero := map[string]bool{
		"nexus_throughput_per_source_total":          false,
		"nexus_payload_processing_latency_seconds":   false,
		"nexus_read_latency_seconds":                 false,
		"nexus_queue_processing_rate_total":          false,
		"nexus_queue_depth":                          false,
		"nexus_wal_pending_entries":                  false,
		"nexus_wal_healthy":                          false,
		"nexus_wal_disk_bytes_free":                  false,
		"nexus_wal_append_latency_seconds":           false,
		"nexus_replay_entries_total":                 false,
		"nexus_replay_duration_seconds":              false,
	}

	for _, mf := range mfs {
		name := mf.GetName()
		if _, want := wantNonZero[name]; !want {
			continue
		}
		if isNonZero(t, mf) {
			wantNonZero[name] = true
		}
	}

	for name, found := range wantNonZero {
		if !found {
			t.Errorf("metric %q was expected to be non-zero but was zero or missing", name)
		}
	}
}

// TestRegistryServesPrometheusFormat verifies that the private registry
// produces valid Prometheus text format output.
//
// Reference: Phase 0D Verification Gate.
func TestRegistryServesPrometheusFormat(t *testing.T) {
	m := metrics.New()
	m.ThroughputPerSource.WithLabelValues("s1").Inc()

	mfs, err := m.Registry().Gather()
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	// Must include the nexus_ namespace.
	found := false
	for _, mf := range mfs {
		if strings.HasPrefix(mf.GetName(), "nexus_") {
			found = true
			break
		}
	}
	if !found {
		t.Error("no nexus_ metrics found in registry output")
	}
}

// TestSecurityMetricsRegistered verifies that the four security-related metrics
// are registered and produce non-zero values when exercised.
//
// Reference: Tech Spec Section 11.3, Phase R-18 Behavioral Contract.
func TestSecurityMetricsRegistered(t *testing.T) {
	m := metrics.New()

	// Exercise all four security metrics.
	m.AuthFailuresTotal.WithLabelValues("unknown").Inc()
	m.PolicyDenialsTotal.WithLabelValues("claude", "write not allowed").Inc()
	m.RateLimitHitsTotal.WithLabelValues("claude").Inc()
	m.AdminCallsTotal.WithLabelValues("/api/status").Inc()

	mfs, err := m.Registry().Gather()
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	wantNonZero := map[string]bool{
		"nexus_auth_failures_total":   false,
		"nexus_policy_denials_total":  false,
		"nexus_rate_limit_hits_total": false,
		"nexus_admin_calls_total":     false,
	}

	for _, mf := range mfs {
		name := mf.GetName()
		if _, want := wantNonZero[name]; !want {
			continue
		}
		if isNonZero(t, mf) {
			wantNonZero[name] = true
		}
	}

	for name, found := range wantNonZero {
		if !found {
			t.Errorf("metric %q was expected to be non-zero but was zero or missing", name)
		}
	}
}

// isNonZero returns true if any metric sample in the family has a non-zero value.
func isNonZero(t *testing.T, mf *dto.MetricFamily) bool {
	t.Helper()
	for _, m := range mf.GetMetric() {
		switch {
		case m.GetCounter() != nil && m.GetCounter().GetValue() != 0:
			return true
		case m.GetGauge() != nil && m.GetGauge().GetValue() != 0:
			return true
		case m.GetHistogram() != nil && m.GetHistogram().GetSampleCount() != 0:
			return true
		}
	}
	return false
}
