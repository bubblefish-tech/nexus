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

// Package observability provides hand-rolled Prometheus text-format metrics
// and a dead-man's switch for BubbleFish Nexus. No external Prometheus client
// library is used; all counters use sync/atomic for lock-free concurrency.
package observability

import (
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// MetricType enumerates supported Prometheus metric types.
type MetricType int

const (
	// Counter is a monotonically increasing counter.
	Counter MetricType = iota
	// Gauge is a value that can go up and down.
	Gauge
)

// String returns the Prometheus TYPE line value.
func (mt MetricType) String() string {
	switch mt {
	case Counter:
		return "counter"
	case Gauge:
		return "gauge"
	default:
		return "untyped"
	}
}

// metric holds a single Prometheus metric's metadata and atomic value.
// The value is stored as int64 bits; for fractional gauges, use float64
// bit patterns via math.Float64bits/Float64frombits externally.
type metric struct {
	name     string
	help     string
	typ      MetricType
	value    atomic.Int64
	labels   map[string]string // nil for unlabeled metrics
	floatVal atomic.Uint64     // for float64 gauge values
	isFloat  bool
}

// Registry is a collection of hand-rolled Prometheus metrics. It is safe
// for concurrent use. All state lives in struct fields; no package-level
// variables are used.
type Registry struct {
	mu      sync.RWMutex
	metrics []*metric
	byName  map[string]*metric

	startTime time.Time
}

// NewRegistry creates a new metric registry. The start time is recorded
// for uptime calculations.
func NewRegistry() *Registry {
	return &Registry{
		byName:    make(map[string]*metric),
		startTime: time.Now(),
	}
}

// RegisterCounter registers a monotonically increasing counter metric.
// Panics if a metric with the same name is already registered.
func (r *Registry) RegisterCounter(name, help string) {
	r.register(name, help, Counter, nil, false)
}

// RegisterGauge registers a gauge metric (can increase or decrease).
// Panics if a metric with the same name is already registered.
func (r *Registry) RegisterGauge(name, help string) {
	r.register(name, help, Gauge, nil, false)
}

// RegisterFloatGauge registers a gauge metric that stores a float64 value.
// Panics if a metric with the same name is already registered.
func (r *Registry) RegisterFloatGauge(name, help string) {
	r.register(name, help, Gauge, nil, true)
}

// RegisterCounterWithLabels registers a counter with fixed labels.
// Panics if a metric with the same name is already registered.
func (r *Registry) RegisterCounterWithLabels(name, help string, labels map[string]string) {
	r.register(name, help, Counter, labels, false)
}

// RegisterGaugeWithLabels registers a gauge with fixed labels.
// Panics if a metric with the same name is already registered.
func (r *Registry) RegisterGaugeWithLabels(name, help string, labels map[string]string) {
	r.register(name, help, Gauge, labels, false)
}

func (r *Registry) register(name, help string, typ MetricType, labels map[string]string, isFloat bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := metricKey(name, labels)
	if _, exists := r.byName[key]; exists {
		panic("observability: duplicate metric registration: " + key)
	}

	m := &metric{
		name:    name,
		help:    help,
		typ:     typ,
		labels:  labels,
		isFloat: isFloat,
	}
	r.metrics = append(r.metrics, m)
	r.byName[key] = m
}

// Inc atomically increments a counter or gauge by 1.
func (r *Registry) Inc(name string) {
	r.IncBy(name, 1)
}

// IncBy atomically increments a counter or gauge by delta.
func (r *Registry) IncBy(name string, delta int64) {
	r.mu.RLock()
	m, ok := r.byName[name]
	r.mu.RUnlock()
	if !ok {
		return
	}
	m.value.Add(delta)
}

// Set atomically sets a gauge to the given value.
func (r *Registry) Set(name string, value int64) {
	r.mu.RLock()
	m, ok := r.byName[name]
	r.mu.RUnlock()
	if !ok {
		return
	}
	m.value.Store(value)
}

// SetFloat atomically sets a float64 gauge value.
func (r *Registry) SetFloat(name string, value float64) {
	r.mu.RLock()
	m, ok := r.byName[name]
	r.mu.RUnlock()
	if !ok {
		return
	}
	m.floatVal.Store(floatBits(value))
}

// Get returns the current int64 value of a metric. Returns 0 if not found.
func (r *Registry) Get(name string) int64 {
	r.mu.RLock()
	m, ok := r.byName[name]
	r.mu.RUnlock()
	if !ok {
		return 0
	}
	return m.value.Load()
}

// GetFloat returns the current float64 value of a float gauge. Returns 0 if not found.
func (r *Registry) GetFloat(name string) float64 {
	r.mu.RLock()
	m, ok := r.byName[name]
	r.mu.RUnlock()
	if !ok {
		return 0
	}
	return bitsFloat(m.floatVal.Load())
}

// IncLabeled atomically increments a labeled counter by 1.
func (r *Registry) IncLabeled(name string, labels map[string]string) {
	key := metricKey(name, labels)
	r.mu.RLock()
	m, ok := r.byName[key]
	r.mu.RUnlock()
	if !ok {
		return
	}
	m.value.Add(1)
}

// SetLabeled atomically sets a labeled gauge.
func (r *Registry) SetLabeled(name string, labels map[string]string, value int64) {
	key := metricKey(name, labels)
	r.mu.RLock()
	m, ok := r.byName[key]
	r.mu.RUnlock()
	if !ok {
		return
	}
	m.value.Store(value)
}

// WritePrometheus writes all metrics in Prometheus text exposition format.
// Metrics are grouped by name; each group gets one HELP and TYPE line.
func (r *Registry) WritePrometheus(w io.Writer) error {
	r.mu.RLock()
	snapshot := make([]*metric, len(r.metrics))
	copy(snapshot, r.metrics)
	r.mu.RUnlock()

	// Group by metric name for proper HELP/TYPE headers.
	type entry struct {
		m     *metric
		value string
	}
	groups := make(map[string][]entry)
	var names []string

	for _, m := range snapshot {
		if _, seen := groups[m.name]; !seen {
			names = append(names, m.name)
		}

		var val string
		if m.isFloat {
			val = formatFloat(bitsFloat(m.floatVal.Load()))
		} else {
			val = fmt.Sprintf("%d", m.value.Load())
		}
		groups[m.name] = append(groups[m.name], entry{m: m, value: val})
	}

	sort.Strings(names)

	for _, name := range names {
		entries := groups[name]
		first := entries[0]

		// HELP line.
		if _, err := fmt.Fprintf(w, "# HELP %s %s\n", name, first.m.help); err != nil {
			return err
		}
		// TYPE line.
		if _, err := fmt.Fprintf(w, "# TYPE %s %s\n", name, first.m.typ); err != nil {
			return err
		}

		for _, e := range entries {
			labelStr := formatLabels(e.m.labels)
			if _, err := fmt.Fprintf(w, "%s%s %s\n", name, labelStr, e.value); err != nil {
				return err
			}
		}
	}
	return nil
}

// Snapshot returns a map of metric keys to their current int64 values.
func (r *Registry) Snapshot() map[string]int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	snap := make(map[string]int64, len(r.metrics))
	for _, m := range r.metrics {
		key := metricKey(m.name, m.labels)
		if m.isFloat {
			// Store float as int64 bits for the snapshot.
			snap[key] = int64(m.floatVal.Load())
		} else {
			snap[key] = m.value.Load()
		}
	}
	return snap
}

// UptimeSeconds returns seconds since the registry was created.
func (r *Registry) UptimeSeconds() float64 {
	return time.Since(r.startTime).Seconds()
}

// StartTime returns when the registry was created.
func (r *Registry) StartTime() time.Time {
	return r.startTime
}

// metricKey builds a unique key for a metric name + labels combo.
func metricKey(name string, labels map[string]string) string {
	if len(labels) == 0 {
		return name
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(name)
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(labels[k])
	}
	b.WriteByte('}')
	return b.String()
}

// formatLabels formats label map into Prometheus label syntax: {k="v",...}.
// Returns "" for nil/empty labels.
func formatLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%s=%q", k, labels[k])
	}
	b.WriteByte('}')
	return b.String()
}

// formatFloat formats a float64 for Prometheus output.
func formatFloat(f float64) string {
	return fmt.Sprintf("%g", f)
}

// floatBits converts float64 to uint64 bits for atomic storage.
func floatBits(f float64) uint64 {
	return math.Float64bits(f)
}

// bitsFloat converts uint64 bits back to float64.
func bitsFloat(b uint64) float64 {
	return math.Float64frombits(b)
}

// ── Standard Nexus Metrics ──────────────────────────────────────────────────

// NexusMetrics holds the 8 standard Prometheus metrics for Nexus observability.
// All counters use sync/atomic; no external Prometheus library is used.
type NexusMetrics struct {
	reg *Registry
}

// Metric name constants for the 8 standard Nexus metrics.
const (
	MetricUptimeSeconds      = "nexus_uptime_seconds"
	MetricMemoryCountTotal   = "nexus_memory_count_total"
	MetricWALPending         = "nexus_wal_pending"
	MetricWALReplayMS        = "nexus_wal_replay_ms"
	MetricCascadeLatencyMS   = "nexus_cascade_latency_ms"
	MetricEmbeddingLatencyMS = "nexus_embedding_latency_ms"
	MetricMCPRequestsTotal   = "nexus_mcp_requests_total"
	MetricSubsystemHealth    = "nexus_subsystem_health"
)

// NewNexusMetrics creates the 8 standard Nexus metrics and registers them
// on a new Registry. The registry is returned via Reg() for custom extensions.
func NewNexusMetrics() *NexusMetrics {
	r := NewRegistry()

	r.RegisterFloatGauge(MetricUptimeSeconds,
		"Seconds since the Nexus daemon started.")
	r.RegisterCounter(MetricMemoryCountTotal,
		"Total number of memory entries stored.")
	r.RegisterGauge(MetricWALPending,
		"Number of WAL entries pending delivery.")
	r.RegisterFloatGauge(MetricWALReplayMS,
		"Duration of last WAL replay in milliseconds.")
	r.RegisterFloatGauge(MetricCascadeLatencyMS,
		"Latest cascade query latency in milliseconds.")
	r.RegisterFloatGauge(MetricEmbeddingLatencyMS,
		"Latest embedding computation latency in milliseconds.")
	r.RegisterCounter(MetricMCPRequestsTotal,
		"Total MCP tool invocations handled.")
	r.RegisterGauge(MetricSubsystemHealth,
		"Subsystem health bitmap (1=healthy).")

	return &NexusMetrics{reg: r}
}

// Reg returns the underlying Registry for direct access or extension.
func (n *NexusMetrics) Reg() *Registry {
	return n.reg
}

// RecordUptime snapshots the current uptime.
func (n *NexusMetrics) RecordUptime() {
	n.reg.SetFloat(MetricUptimeSeconds, n.reg.UptimeSeconds())
}

// IncMemoryCount increments the total memory entry count by 1.
func (n *NexusMetrics) IncMemoryCount() {
	n.reg.Inc(MetricMemoryCountTotal)
}

// IncMemoryCountBy increments the total memory entry count by delta.
func (n *NexusMetrics) IncMemoryCountBy(delta int64) {
	n.reg.IncBy(MetricMemoryCountTotal, delta)
}

// SetWALPending sets the count of WAL entries pending delivery.
func (n *NexusMetrics) SetWALPending(count int64) {
	n.reg.Set(MetricWALPending, count)
}

// SetWALReplayMS sets the last WAL replay duration.
func (n *NexusMetrics) SetWALReplayMS(ms float64) {
	n.reg.SetFloat(MetricWALReplayMS, ms)
}

// SetCascadeLatencyMS sets the latest cascade query latency.
func (n *NexusMetrics) SetCascadeLatencyMS(ms float64) {
	n.reg.SetFloat(MetricCascadeLatencyMS, ms)
}

// SetEmbeddingLatencyMS sets the latest embedding computation latency.
func (n *NexusMetrics) SetEmbeddingLatencyMS(ms float64) {
	n.reg.SetFloat(MetricEmbeddingLatencyMS, ms)
}

// IncMCPRequests increments the MCP request counter by 1.
func (n *NexusMetrics) IncMCPRequests() {
	n.reg.Inc(MetricMCPRequestsTotal)
}

// SetSubsystemHealth sets the subsystem health bitmap.
func (n *NexusMetrics) SetSubsystemHealth(bitmap int64) {
	n.reg.Set(MetricSubsystemHealth, bitmap)
}

// WritePrometheus writes all metrics in Prometheus text exposition format.
func (n *NexusMetrics) WritePrometheus(w io.Writer) error {
	n.RecordUptime()
	return n.reg.WritePrometheus(w)
}
