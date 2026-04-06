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

// Package metrics provides a private Prometheus registry and all initial
// BubbleFish Nexus metrics. Every metric registered here has at least one
// code path that increments or observes it — permanently-zero metrics are bugs.
//
// INVARIANT: Never use prometheus.DefaultRegisterer. All metrics live
// exclusively on the private registry returned by New().
//
// Reference: Tech Spec Section 11.3, Phase 0D Behavioral Contract items 1–3.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

// Metrics holds the private Prometheus registry and all registered counters,
// gauges, and histograms. All metric names use the "bubblefish_" prefix.
//
// Initialize with New(). Never embed or copy — pass by pointer.
type Metrics struct {
	reg *prometheus.Registry

	// ── Write path ──────────────────────────────────────────────────────────
	// PayloadProcessingLatency is the end-to-end write path latency, labeled
	// by source. Observed in handleWrite on each accepted payload.
	PayloadProcessingLatency *prometheus.HistogramVec

	// ThroughputPerSource counts successful writes per source.
	// Incremented in handleWrite after WAL + enqueue succeed.
	ThroughputPerSource *prometheus.CounterVec

	// ErrorsTotal counts errors by type label (e.g. "wal_append", "unmarshal").
	// Incremented whenever a write or queue operation fails fatally.
	ErrorsTotal *prometheus.CounterVec

	// ── Read path ────────────────────────────────────────────────────────────
	// ReadLatency is the end-to-end read path latency, labeled by source and
	// endpoint. Observed in handleQuery on each completed read.
	ReadLatency *prometheus.HistogramVec

	// ── Queue ────────────────────────────────────────────────────────────────
	// QueueDepth is the current number of entries buffered in the queue channel.
	// Updated by the daemon's watchdog goroutine and on each enqueue/dequeue.
	QueueDepth prometheus.Gauge

	// QueueProcessingRate counts payloads successfully dequeued and written.
	// Incremented by the queue worker on each successful destination write.
	QueueProcessingRate prometheus.Counter

	// ── WAL ──────────────────────────────────────────────────────────────────
	// WALPendingEntries is the count of WAL entries not yet DELIVERED.
	// Set after replay and updated by the WAL watchdog.
	WALPendingEntries prometheus.Gauge

	// WALDiskBytesFree is the available disk space on the WAL partition.
	// Updated by the WAL watchdog and doctor.
	WALDiskBytesFree prometheus.Gauge

	// WALHealthy is 1 when the WAL watchdog reports healthy, 0 otherwise.
	// Set by the WAL watchdog goroutine.
	WALHealthy prometheus.Gauge

	// WALAppendLatency is the per-Append fsync latency.
	// Observed in handleWrite around each wal.Append call.
	WALAppendLatency prometheus.Histogram

	// WALCRCFailures counts CRC32 mismatches detected during replay.
	// Incremented after replay by syncing WAL.CRCFailures().
	WALCRCFailures prometheus.Counter

	// WALIntegrityFailures counts HMAC mismatches during replay (integrity=mac).
	// Incremented when integrity checking is enabled and a MAC fails.
	WALIntegrityFailures prometheus.Counter

	// ── Replay ───────────────────────────────────────────────────────────────
	// ReplayEntriesTotal counts WAL entries processed during startup replay.
	// Incremented once per PENDING entry in replayWAL.
	ReplayEntriesTotal prometheus.Counter

	// ReplayDurationSeconds records the wall-clock time spent on WAL replay.
	// Set (not incremented) in replayWAL after replay completes.
	ReplayDurationSeconds prometheus.Gauge

	// ── Auth ─────────────────────────────────────────────────────────────────
	// AuthFailuresTotal counts authentication failures by source label.
	// Incremented in authenticate() when no key matches.
	AuthFailuresTotal *prometheus.CounterVec

	// ── Policy ───────────────────────────────────────────────────────────────
	// PolicyDenialsTotal counts policy gate rejections by source and reason.
	// Incremented whenever a policy check denies a request (403).
	// Reference: Tech Spec Section 11.3.
	PolicyDenialsTotal *prometheus.CounterVec

	// ── Rate limit ───────────────────────────────────────────────────────────
	// RateLimitHitsTotal counts rate limit rejections by source label.
	// Incremented in handleWrite and handleQuery when Allow() returns false.
	RateLimitHitsTotal *prometheus.CounterVec

	// ── Admin ────────────────────────────────────────────────────────────────
	// AdminCallsTotal counts admin endpoint calls by endpoint label.
	// Incremented in the admin middleware (requireAdminToken success path).
	AdminCallsTotal *prometheus.CounterVec

	// ── Config ───────────────────────────────────────────────────────────────
	// ConfigLintWarnings is the number of non-fatal config lint warnings.
	// Set after config load and after each hot reload.
	ConfigLintWarnings prometheus.Gauge

	// ── Embedding (Stage 4) ──────────────────────────────────────────────────
	// EmbeddingLatency is the end-to-end embedding provider call duration.
	// Observed in the cascade Stage 4 path on each Embed() call.
	// Reference: Tech Spec Section 11.3.
	EmbeddingLatency prometheus.Histogram

	// ── Temporal Decay (Stage 5) ─────────────────────────────────────────────
	// TemporalDecayApplied counts the number of times temporal decay reranking
	// was applied in Stage 5 (Hybrid Merge + Temporal Decay).
	// Reference: Tech Spec Section 11.3.
	TemporalDecayApplied prometheus.Counter

	// ── Consistency ──────────────────────────────────────────────────────────
	// ConsistencyScore is the WAL-to-destination consistency score (0.0–1.0).
	// Set by the consistency checker background goroutine.
	// Reference: Tech Spec Section 11.5.
	ConsistencyScore prometheus.Gauge
}

// New creates a Metrics with a private Prometheus registry. All metrics are
// registered exclusively on this private registry — prometheus.DefaultRegisterer
// is never touched.
//
// New() never returns an error; MustRegister panics only on programming errors
// (duplicate names), which cannot occur with a fresh private registry.
func New() *Metrics {
	reg := prometheus.NewRegistry()

	// Register standard Go runtime and process metrics on the private registry.
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	m := &Metrics{reg: reg}

	// ── Write path ──────────────────────────────────────────────────────────
	m.PayloadProcessingLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "bubblefish_payload_processing_latency_seconds",
			Help:    "Full write path latency by source.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"source"},
	)
	m.ThroughputPerSource = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "bubblefish_throughput_per_source_total",
			Help: "Successful writes by source.",
		},
		[]string{"source"},
	)
	m.ErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "bubblefish_errors_total",
			Help: "Errors by type label.",
		},
		[]string{"type"},
	)

	// ── Read path ────────────────────────────────────────────────────────────
	m.ReadLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "bubblefish_read_latency_seconds",
			Help:    "Full read path latency by source and endpoint.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"source", "endpoint"},
	)

	// ── Queue ────────────────────────────────────────────────────────────────
	m.QueueDepth = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "bubblefish_queue_depth",
		Help: "Current queue depth.",
	})
	m.QueueProcessingRate = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "bubblefish_queue_processing_rate_total",
		Help: "Payloads dequeued and successfully written.",
	})

	// ── WAL ──────────────────────────────────────────────────────────────────
	m.WALPendingEntries = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "bubblefish_wal_pending_entries",
		Help: "WAL entries not yet DELIVERED.",
	})
	m.WALDiskBytesFree = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "bubblefish_wal_disk_bytes_free",
		Help: "Free disk on WAL partition.",
	})
	m.WALHealthy = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "bubblefish_wal_healthy",
		Help: "1 if WAL watchdog healthy, 0 if not.",
	})
	m.WALAppendLatency = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "bubblefish_wal_append_latency_seconds",
		Help:    "WAL append + fsync latency.",
		Buckets: prometheus.DefBuckets,
	})
	m.WALCRCFailures = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "bubblefish_wal_crc_failures_total",
		Help: "CRC32 mismatches on replay.",
	})
	m.WALIntegrityFailures = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "bubblefish_wal_integrity_failures_total",
		Help: "HMAC mismatches on replay (integrity=mac).",
	})

	// ── Replay ───────────────────────────────────────────────────────────────
	m.ReplayEntriesTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "bubblefish_replay_entries_total",
		Help: "WAL entries processed during replay.",
	})
	m.ReplayDurationSeconds = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "bubblefish_replay_duration_seconds",
		Help: "Time spent on WAL replay at startup.",
	})

	// ── Auth ─────────────────────────────────────────────────────────────────
	m.AuthFailuresTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "bubblefish_auth_failures_total",
			Help: "Auth failures by source label.",
		},
		[]string{"source"},
	)

	// ── Policy ──────────────────────────────────────────────────────────────
	m.PolicyDenialsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "bubblefish_policy_denials_total",
			Help: "Policy gate rejections by source and reason.",
		},
		[]string{"source", "reason"},
	)

	// ── Rate limit ───────────────────────────────────────────────────────────
	m.RateLimitHitsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "bubblefish_rate_limit_hits_total",
			Help: "Rate limit hits by source label.",
		},
		[]string{"source"},
	)

	// ── Admin ────────────────────────────────────────────────────────────────
	m.AdminCallsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "bubblefish_admin_calls_total",
			Help: "Admin endpoint calls by endpoint label.",
		},
		[]string{"endpoint"},
	)

	// ── Config ───────────────────────────────────────────────────────────────
	m.ConfigLintWarnings = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "bubblefish_config_lint_warnings",
		Help: "Number of config lint warnings.",
	})

	// ── Embedding ────────────────────────────────────────────────────────────
	m.EmbeddingLatency = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "bubblefish_embedding_latency_seconds",
		Help:    "Embedding provider call duration.",
		Buckets: prometheus.DefBuckets,
	})

	// ── Temporal Decay ───────────────────────────────────────────────────────
	m.TemporalDecayApplied = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "bubblefish_temporal_decay_applied_total",
		Help: "Number of times temporal decay reranking was applied in Stage 5.",
	})

	// ── Consistency ──────────────────────────────────────────────────────────
	m.ConsistencyScore = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "bubblefish_consistency_score",
		Help: "WAL-to-destination consistency score (0.0-1.0).",
	})

	// Register all application metrics on the private registry.
	reg.MustRegister(
		m.PayloadProcessingLatency,
		m.ThroughputPerSource,
		m.ErrorsTotal,
		m.ReadLatency,
		m.QueueDepth,
		m.QueueProcessingRate,
		m.WALPendingEntries,
		m.WALDiskBytesFree,
		m.WALHealthy,
		m.WALAppendLatency,
		m.WALCRCFailures,
		m.WALIntegrityFailures,
		m.ReplayEntriesTotal,
		m.ReplayDurationSeconds,
		m.AuthFailuresTotal,
		m.PolicyDenialsTotal,
		m.RateLimitHitsTotal,
		m.AdminCallsTotal,
		m.ConfigLintWarnings,
		m.EmbeddingLatency,
		m.TemporalDecayApplied,
		m.ConsistencyScore,
	)

	return m
}

// Registry returns the private Prometheus registry. Pass to
// promhttp.HandlerFor to serve the /metrics endpoint.
//
// Reference: Tech Spec Section 12 (/metrics endpoint), Phase 0D item 4.
func (m *Metrics) Registry() *prometheus.Registry {
	return m.reg
}
