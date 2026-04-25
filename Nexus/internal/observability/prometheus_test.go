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

package observability_test

import (
	"bytes"
	"strings"
	"sync"
	"testing"

	"github.com/bubblefish-tech/nexus/internal/observability"
)

func TestNewRegistry(t *testing.T) {
	r := observability.NewRegistry()
	if r == nil {
		t.Fatal("NewRegistry returned nil")
	}
	if r.UptimeSeconds() < 0 {
		t.Error("uptime should be non-negative")
	}
}

func TestRegisterAndIncCounter(t *testing.T) {
	r := observability.NewRegistry()
	r.RegisterCounter("test_counter_total", "A test counter.")

	r.Inc("test_counter_total")
	r.Inc("test_counter_total")
	r.Inc("test_counter_total")

	if got := r.Get("test_counter_total"); got != 3 {
		t.Errorf("expected 3, got %d", got)
	}
}

func TestIncByCounter(t *testing.T) {
	r := observability.NewRegistry()
	r.RegisterCounter("test_incby_total", "A test counter.")

	r.IncBy("test_incby_total", 10)
	r.IncBy("test_incby_total", 5)

	if got := r.Get("test_incby_total"); got != 15 {
		t.Errorf("expected 15, got %d", got)
	}
}

func TestSetGauge(t *testing.T) {
	r := observability.NewRegistry()
	r.RegisterGauge("test_gauge", "A test gauge.")

	r.Set("test_gauge", 42)
	if got := r.Get("test_gauge"); got != 42 {
		t.Errorf("expected 42, got %d", got)
	}

	r.Set("test_gauge", 0)
	if got := r.Get("test_gauge"); got != 0 {
		t.Errorf("expected 0 after reset, got %d", got)
	}
}

func TestFloatGauge(t *testing.T) {
	r := observability.NewRegistry()
	r.RegisterFloatGauge("test_float_gauge", "A float gauge.")

	r.SetFloat("test_float_gauge", 3.14)
	got := r.GetFloat("test_float_gauge")
	if got < 3.13 || got > 3.15 {
		t.Errorf("expected ~3.14, got %f", got)
	}
}

func TestFloatGaugeZero(t *testing.T) {
	r := observability.NewRegistry()
	r.RegisterFloatGauge("test_float_zero", "A float gauge.")

	// Default value should be 0.
	got := r.GetFloat("test_float_zero")
	if got != 0 {
		t.Errorf("expected 0.0 default, got %f", got)
	}
}

func TestGetNonexistent(t *testing.T) {
	r := observability.NewRegistry()
	if got := r.Get("nonexistent"); got != 0 {
		t.Errorf("expected 0 for nonexistent metric, got %d", got)
	}
	if got := r.GetFloat("nonexistent"); got != 0 {
		t.Errorf("expected 0.0 for nonexistent float metric, got %f", got)
	}
}

func TestIncNonexistent(t *testing.T) {
	r := observability.NewRegistry()
	// Should not panic.
	r.Inc("nonexistent")
	r.IncBy("nonexistent", 5)
	r.Set("nonexistent", 10)
	r.SetFloat("nonexistent", 1.0)
}

func TestDuplicateRegistrationPanics(t *testing.T) {
	r := observability.NewRegistry()
	r.RegisterCounter("dup_metric", "First.")

	defer func() {
		if recover() == nil {
			t.Error("expected panic on duplicate registration")
		}
	}()
	r.RegisterCounter("dup_metric", "Second.")
}

func TestWritePrometheus_Counter(t *testing.T) {
	r := observability.NewRegistry()
	r.RegisterCounter("test_requests_total", "Total requests.")
	r.IncBy("test_requests_total", 42)

	var buf bytes.Buffer
	if err := r.WritePrometheus(&buf); err != nil {
		t.Fatalf("WritePrometheus: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "# HELP test_requests_total Total requests.") {
		t.Error("missing HELP line")
	}
	if !strings.Contains(output, "# TYPE test_requests_total counter") {
		t.Error("missing TYPE line")
	}
	if !strings.Contains(output, "test_requests_total 42") {
		t.Errorf("missing value line, got:\n%s", output)
	}
}

func TestWritePrometheus_Gauge(t *testing.T) {
	r := observability.NewRegistry()
	r.RegisterGauge("test_queue_depth", "Current queue depth.")
	r.Set("test_queue_depth", 7)

	var buf bytes.Buffer
	if err := r.WritePrometheus(&buf); err != nil {
		t.Fatalf("WritePrometheus: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "# TYPE test_queue_depth gauge") {
		t.Error("missing TYPE gauge line")
	}
	if !strings.Contains(output, "test_queue_depth 7") {
		t.Errorf("missing value line, got:\n%s", output)
	}
}

func TestWritePrometheus_FloatGauge(t *testing.T) {
	r := observability.NewRegistry()
	r.RegisterFloatGauge("test_latency_ms", "Latency in ms.")
	r.SetFloat("test_latency_ms", 12.5)

	var buf bytes.Buffer
	if err := r.WritePrometheus(&buf); err != nil {
		t.Fatalf("WritePrometheus: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "test_latency_ms 12.5") {
		t.Errorf("expected float value, got:\n%s", output)
	}
}

func TestWritePrometheus_WithLabels(t *testing.T) {
	r := observability.NewRegistry()
	labels := map[string]string{"method": "GET", "status": "200"}
	r.RegisterCounterWithLabels("http_total", "HTTP requests.", labels)
	r.IncLabeled("http_total", labels)
	r.IncLabeled("http_total", labels)

	var buf bytes.Buffer
	if err := r.WritePrometheus(&buf); err != nil {
		t.Fatalf("WritePrometheus: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, `method="GET"`) {
		t.Errorf("missing method label, got:\n%s", output)
	}
	if !strings.Contains(output, `status="200"`) {
		t.Errorf("missing status label, got:\n%s", output)
	}
	if !strings.Contains(output, "2") {
		t.Errorf("expected value 2, got:\n%s", output)
	}
}

func TestWritePrometheus_Sorted(t *testing.T) {
	r := observability.NewRegistry()
	r.RegisterCounter("z_metric", "Z.")
	r.RegisterCounter("a_metric", "A.")
	r.RegisterCounter("m_metric", "M.")

	var buf bytes.Buffer
	if err := r.WritePrometheus(&buf); err != nil {
		t.Fatalf("WritePrometheus: %v", err)
	}

	output := buf.String()
	aIdx := strings.Index(output, "a_metric")
	mIdx := strings.Index(output, "m_metric")
	zIdx := strings.Index(output, "z_metric")

	if aIdx >= mIdx || mIdx >= zIdx {
		t.Errorf("metrics not sorted: a=%d, m=%d, z=%d", aIdx, mIdx, zIdx)
	}
}

func TestSnapshot(t *testing.T) {
	r := observability.NewRegistry()
	r.RegisterCounter("snap_counter", "Counter.")
	r.RegisterGauge("snap_gauge", "Gauge.")
	r.IncBy("snap_counter", 5)
	r.Set("snap_gauge", 10)

	snap := r.Snapshot()
	if snap["snap_counter"] != 5 {
		t.Errorf("snap_counter: expected 5, got %d", snap["snap_counter"])
	}
	if snap["snap_gauge"] != 10 {
		t.Errorf("snap_gauge: expected 10, got %d", snap["snap_gauge"])
	}
}

func TestConcurrentAccess(t *testing.T) {
	r := observability.NewRegistry()
	r.RegisterCounter("concurrent_counter", "Concurrent test.")

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.Inc("concurrent_counter")
		}()
	}
	wg.Wait()

	if got := r.Get("concurrent_counter"); got != 100 {
		t.Errorf("expected 100 after concurrent increments, got %d", got)
	}
}

// ── NexusMetrics tests ──────────────────────────────────────────────────────

func TestNexusMetrics_New(t *testing.T) {
	nm := observability.NewNexusMetrics()
	if nm == nil {
		t.Fatal("NewNexusMetrics returned nil")
	}
	if nm.Reg() == nil {
		t.Fatal("Reg() returned nil")
	}
}

func TestNexusMetrics_IncMemoryCount(t *testing.T) {
	nm := observability.NewNexusMetrics()
	nm.IncMemoryCount()
	nm.IncMemoryCount()
	nm.IncMemoryCountBy(3)

	got := nm.Reg().Get(observability.MetricMemoryCountTotal)
	if got != 5 {
		t.Errorf("expected 5, got %d", got)
	}
}

func TestNexusMetrics_WALPending(t *testing.T) {
	nm := observability.NewNexusMetrics()
	nm.SetWALPending(42)

	got := nm.Reg().Get(observability.MetricWALPending)
	if got != 42 {
		t.Errorf("expected 42, got %d", got)
	}
}

func TestNexusMetrics_WALReplayMS(t *testing.T) {
	nm := observability.NewNexusMetrics()
	nm.SetWALReplayMS(123.45)

	got := nm.Reg().GetFloat(observability.MetricWALReplayMS)
	if got < 123.0 || got > 124.0 {
		t.Errorf("expected ~123.45, got %f", got)
	}
}

func TestNexusMetrics_CascadeLatencyMS(t *testing.T) {
	nm := observability.NewNexusMetrics()
	nm.SetCascadeLatencyMS(5.5)

	got := nm.Reg().GetFloat(observability.MetricCascadeLatencyMS)
	if got < 5.0 || got > 6.0 {
		t.Errorf("expected ~5.5, got %f", got)
	}
}

func TestNexusMetrics_EmbeddingLatencyMS(t *testing.T) {
	nm := observability.NewNexusMetrics()
	nm.SetEmbeddingLatencyMS(22.2)

	got := nm.Reg().GetFloat(observability.MetricEmbeddingLatencyMS)
	if got < 22.0 || got > 23.0 {
		t.Errorf("expected ~22.2, got %f", got)
	}
}

func TestNexusMetrics_MCPRequests(t *testing.T) {
	nm := observability.NewNexusMetrics()
	for i := 0; i < 10; i++ {
		nm.IncMCPRequests()
	}

	got := nm.Reg().Get(observability.MetricMCPRequestsTotal)
	if got != 10 {
		t.Errorf("expected 10, got %d", got)
	}
}

func TestNexusMetrics_SubsystemHealth(t *testing.T) {
	nm := observability.NewNexusMetrics()
	nm.SetSubsystemHealth(0xFF)

	got := nm.Reg().Get(observability.MetricSubsystemHealth)
	if got != 0xFF {
		t.Errorf("expected 0xFF, got 0x%X", got)
	}
}

func TestNexusMetrics_WritePrometheus(t *testing.T) {
	nm := observability.NewNexusMetrics()
	nm.IncMemoryCount()
	nm.SetWALPending(3)
	nm.IncMCPRequests()

	var buf bytes.Buffer
	if err := nm.WritePrometheus(&buf); err != nil {
		t.Fatalf("WritePrometheus: %v", err)
	}

	output := buf.String()

	wantMetrics := []string{
		"nexus_uptime_seconds",
		"nexus_memory_count_total",
		"nexus_wal_pending",
		"nexus_wal_replay_ms",
		"nexus_cascade_latency_ms",
		"nexus_embedding_latency_ms",
		"nexus_mcp_requests_total",
		"nexus_subsystem_health",
	}
	for _, m := range wantMetrics {
		if !strings.Contains(output, m) {
			t.Errorf("missing metric %q in output", m)
		}
	}
}

func TestNexusMetrics_UptimeNonZero(t *testing.T) {
	nm := observability.NewNexusMetrics()
	nm.RecordUptime()

	got := nm.Reg().GetFloat(observability.MetricUptimeSeconds)
	if got < 0 {
		t.Errorf("uptime should be non-negative, got %f", got)
	}
}

func TestNexusMetrics_All8MetricsRegistered(t *testing.T) {
	nm := observability.NewNexusMetrics()

	var buf bytes.Buffer
	if err := nm.WritePrometheus(&buf); err != nil {
		t.Fatalf("WritePrometheus: %v", err)
	}

	output := buf.String()
	// Count HELP lines — should be exactly 8 metrics.
	helpCount := strings.Count(output, "# HELP nexus_")
	if helpCount != 8 {
		t.Errorf("expected 8 HELP lines, got %d", helpCount)
	}
}
