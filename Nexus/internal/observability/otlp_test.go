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

package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"
)

// mockPoster captures HTTP requests for assertion.
type mockPoster struct {
	mu       sync.Mutex
	requests []*http.Request
	bodies   [][]byte
	status   int
	err      error
}

func (m *mockPoster) Do(req *http.Request) (*http.Response, error) {
	if m.err != nil {
		return nil, m.err
	}
	body, _ := io.ReadAll(req.Body)
	m.mu.Lock()
	m.requests = append(m.requests, req)
	m.bodies = append(m.bodies, body)
	m.mu.Unlock()
	status := m.status
	if status == 0 {
		status = 200
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(bytes.NewReader(nil)),
	}, nil
}

func (m *mockPoster) requestCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.requests)
}

func TestNewOTLPExporter_NoEndpoint(t *testing.T) {
	_, err := NewOTLPExporter(ExporterConfig{})
	if err != ErrExporterNoEndpoint {
		t.Fatalf("expected ErrExporterNoEndpoint, got %v", err)
	}
}

func TestNewOTLPExporter_Defaults(t *testing.T) {
	e, err := NewOTLPExporter(ExporterConfig{
		Endpoint: "http://localhost:4318/v1/metrics",
	})
	if err != nil {
		t.Fatalf("NewOTLPExporter: %v", err)
	}
	if e.interval != 60*time.Second {
		t.Errorf("interval = %v, want 60s", e.interval)
	}
	if len(e.metrics) != len(DefaultMetrics) {
		t.Errorf("metrics count = %d, want %d", len(e.metrics), len(DefaultMetrics))
	}
}

func TestRecord_AddsToBuffer(t *testing.T) {
	e, err := NewOTLPExporter(ExporterConfig{
		Endpoint: "http://localhost:4318/v1/metrics",
	})
	if err != nil {
		t.Fatal(err)
	}

	e.Record(DataPoint{Name: MetricActiveAgents, Value: 5})
	e.Record(DataPoint{Name: MetricAgentRequestsTotal, Value: 100})

	if e.BufferLen() != 2 {
		t.Fatalf("buffer length = %d, want 2", e.BufferLen())
	}
}

func TestRecord_SetsTimestamp(t *testing.T) {
	e, err := NewOTLPExporter(ExporterConfig{
		Endpoint: "http://localhost:4318/v1/metrics",
	})
	if err != nil {
		t.Fatal(err)
	}

	before := time.Now()
	e.Record(DataPoint{Name: MetricActiveAgents, Value: 1})

	e.mu.Lock()
	ts := e.buffer[0].Timestamp
	e.mu.Unlock()

	if ts.Before(before) {
		t.Errorf("timestamp %v is before recording time %v", ts, before)
	}
}

func TestFlush_SendsPayload(t *testing.T) {
	poster := &mockPoster{}
	e, err := NewOTLPExporter(ExporterConfig{
		Endpoint:   "http://localhost:4318/v1/metrics",
		HTTPClient: poster,
	})
	if err != nil {
		t.Fatal(err)
	}

	e.Record(DataPoint{Name: MetricActiveAgents, Value: 3})
	e.Record(DataPoint{Name: MetricWALPendingEntries, Value: 42})

	if err := e.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	if poster.requestCount() != 1 {
		t.Fatalf("expected 1 request, got %d", poster.requestCount())
	}
	if e.BufferLen() != 0 {
		t.Errorf("buffer should be empty after flush, got %d", e.BufferLen())
	}

	// Verify JSON structure.
	var payload otlpPayload
	if err := json.Unmarshal(poster.bodies[0], &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if len(payload.ResourceMetrics) != 1 {
		t.Fatalf("resourceMetrics = %d", len(payload.ResourceMetrics))
	}
	rm := payload.ResourceMetrics[0]
	if len(rm.ScopeMetrics) != 1 {
		t.Fatalf("scopeMetrics = %d", len(rm.ScopeMetrics))
	}
	if len(rm.ScopeMetrics[0].Metrics) != 2 {
		t.Errorf("metrics = %d, want 2", len(rm.ScopeMetrics[0].Metrics))
	}
}

func TestFlush_EmptyBuffer_NoRequest(t *testing.T) {
	poster := &mockPoster{}
	e, err := NewOTLPExporter(ExporterConfig{
		Endpoint:   "http://localhost:4318/v1/metrics",
		HTTPClient: poster,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := e.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if poster.requestCount() != 0 {
		t.Errorf("expected 0 requests for empty buffer, got %d", poster.requestCount())
	}
}

func TestFlush_HTTPError(t *testing.T) {
	poster := &mockPoster{err: fmt.Errorf("connection refused")}
	e, err := NewOTLPExporter(ExporterConfig{
		Endpoint:   "http://localhost:4318/v1/metrics",
		HTTPClient: poster,
	})
	if err != nil {
		t.Fatal(err)
	}

	e.Record(DataPoint{Name: MetricActiveAgents, Value: 1})
	if err := e.Flush(context.Background()); err == nil {
		t.Fatal("expected error for HTTP failure")
	}
}

func TestFlush_Non2xxStatus(t *testing.T) {
	poster := &mockPoster{status: 503}
	e, err := NewOTLPExporter(ExporterConfig{
		Endpoint:   "http://localhost:4318/v1/metrics",
		HTTPClient: poster,
	})
	if err != nil {
		t.Fatal(err)
	}

	e.Record(DataPoint{Name: MetricActiveAgents, Value: 1})
	if err := e.Flush(context.Background()); err == nil {
		t.Fatal("expected error for 503 status")
	}
}

func TestFlush_Headers(t *testing.T) {
	poster := &mockPoster{}
	e, err := NewOTLPExporter(ExporterConfig{
		Endpoint:   "http://localhost:4318/v1/metrics",
		HTTPClient: poster,
		Headers:    map[string]string{"Authorization": "Bearer test-token"},
	})
	if err != nil {
		t.Fatal(err)
	}

	e.Record(DataPoint{Name: MetricActiveAgents, Value: 1})
	if err := e.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	if poster.requests[0].Header.Get("Authorization") != "Bearer test-token" {
		t.Errorf("missing Authorization header")
	}
	if poster.requests[0].Header.Get("Content-Type") != "application/json" {
		t.Errorf("missing Content-Type header")
	}
}

func TestPayload_MetricTypes(t *testing.T) {
	poster := &mockPoster{}
	e, err := NewOTLPExporter(ExporterConfig{
		Endpoint:   "http://localhost:4318/v1/metrics",
		HTTPClient: poster,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Record one of each type.
	e.Record(DataPoint{Name: MetricActiveAgents, Value: 5})           // Gauge
	e.Record(DataPoint{Name: MetricAgentRequestsTotal, Value: 100})   // Sum
	e.Record(DataPoint{Name: MetricMemoryWriteDuration, Value: 0.05}) // Histogram

	if err := e.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	var payload otlpPayload
	if err := json.Unmarshal(poster.bodies[0], &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	metricsByName := make(map[string]otlpMetric)
	for _, m := range payload.ResourceMetrics[0].ScopeMetrics[0].Metrics {
		metricsByName[m.Name] = m
	}

	// Check gauge.
	agents := metricsByName[MetricActiveAgents]
	if agents.Gauge == nil {
		t.Error("active_agents should be a Gauge")
	}

	// Check sum.
	requests := metricsByName[MetricAgentRequestsTotal]
	if requests.Sum == nil {
		t.Error("agent_requests_total should be a Sum")
	}
	if requests.Sum != nil && !requests.Sum.IsMonotonic {
		t.Error("agent_requests_total should be monotonic")
	}

	// Check histogram.
	writes := metricsByName[MetricMemoryWriteDuration]
	if writes.Histogram == nil {
		t.Error("memory_write_duration should be a Histogram")
	}
}

func TestPayload_Attributes(t *testing.T) {
	poster := &mockPoster{}
	e, err := NewOTLPExporter(ExporterConfig{
		Endpoint:   "http://localhost:4318/v1/metrics",
		HTTPClient: poster,
	})
	if err != nil {
		t.Fatal(err)
	}

	e.Record(DataPoint{
		Name:       MetricAgentRequestsTotal,
		Value:      1,
		Attributes: map[string]string{"agent_id": "agent-42", "method": "POST"},
	})

	if err := e.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	var payload otlpPayload
	if err := json.Unmarshal(poster.bodies[0], &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	m := payload.ResourceMetrics[0].ScopeMetrics[0].Metrics[0]
	if m.Sum == nil || len(m.Sum.DataPoints) == 0 {
		t.Fatal("expected sum data points")
	}
	dp := m.Sum.DataPoints[0]
	if len(dp.Attributes) != 2 {
		t.Errorf("attributes count = %d, want 2", len(dp.Attributes))
	}
}

func TestPayload_ResourceAttributes(t *testing.T) {
	poster := &mockPoster{}
	e, err := NewOTLPExporter(ExporterConfig{
		Endpoint:   "http://localhost:4318/v1/metrics",
		HTTPClient: poster,
	})
	if err != nil {
		t.Fatal(err)
	}

	e.Record(DataPoint{Name: MetricActiveAgents, Value: 1})
	if err := e.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	var payload otlpPayload
	if err := json.Unmarshal(poster.bodies[0], &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	attrs := payload.ResourceMetrics[0].Resource.Attributes
	found := false
	for _, a := range attrs {
		if a.Key == "service.name" && a.Value.StringValue == "nexus" {
			found = true
		}
	}
	if !found {
		t.Error("resource attributes should include service.name=nexus")
	}
}

func TestRecord_ConcurrentSafety(t *testing.T) {
	e, err := NewOTLPExporter(ExporterConfig{
		Endpoint: "http://localhost:4318/v1/metrics",
	})
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(v int) {
			defer wg.Done()
			e.Record(DataPoint{Name: MetricActiveAgents, Value: float64(v)})
		}(i)
	}
	wg.Wait()

	if e.BufferLen() != 100 {
		t.Errorf("buffer = %d, want 100", e.BufferLen())
	}
}

func TestDefaultMetrics_Count(t *testing.T) {
	if len(DefaultMetrics) != 7 {
		t.Errorf("DefaultMetrics count = %d, want 7", len(DefaultMetrics))
	}

	names := make(map[string]bool)
	for _, m := range DefaultMetrics {
		if names[m.Name] {
			t.Errorf("duplicate metric name: %s", m.Name)
		}
		names[m.Name] = true
	}
}

func TestStartStop(t *testing.T) {
	poster := &mockPoster{}
	e, err := NewOTLPExporter(ExporterConfig{
		Endpoint:   "http://localhost:4318/v1/metrics",
		HTTPClient: poster,
		Interval:   50 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}

	e.Record(DataPoint{Name: MetricActiveAgents, Value: 1})
	e.Start()

	// Wait a bit for at least one tick.
	time.Sleep(200 * time.Millisecond)

	e.Record(DataPoint{Name: MetricActiveAgents, Value: 2})
	e.Stop() // Should do a final flush.

	// Should have at least 1 flush (from the tick) and possibly the final flush.
	if poster.requestCount() < 1 {
		t.Errorf("expected at least 1 flush, got %d", poster.requestCount())
	}
}
