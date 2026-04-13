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

package ingest

import "github.com/prometheus/client_golang/prometheus"

// IngestMetrics holds Prometheus metrics for the Ingest subsystem.
// All metric names use the "nexus_ingest_" prefix.
type IngestMetrics struct {
	IngestionsTotal *prometheus.CounterVec
	ParseErrors     *prometheus.CounterVec
	ParseDuration   *prometheus.HistogramVec
	ActiveFiles     *prometheus.GaugeVec
	WatchersTotal   *prometheus.GaugeVec
}

// NewIngestMetrics creates and registers Ingest metrics on the given registry.
// If registry is nil, metrics are not registered (testing/disabled mode).
func NewIngestMetrics(reg *prometheus.Registry) *IngestMetrics {
	m := &IngestMetrics{
		IngestionsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "nexus_ingest_ingestions_total",
			Help: "Total number of memories ingested by watcher.",
		}, []string{"watcher"}),
		ParseErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "nexus_ingest_parse_errors_total",
			Help: "Total number of parse errors by watcher.",
		}, []string{"watcher"}),
		ParseDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "nexus_ingest_parse_duration_seconds",
			Help:    "Duration of parse operations by watcher.",
			Buckets: prometheus.DefBuckets,
		}, []string{"watcher"}),
		ActiveFiles: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "nexus_ingest_active_files",
			Help: "Number of actively watched files by watcher.",
		}, []string{"watcher"}),
		WatchersTotal: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "nexus_ingest_watchers_total",
			Help: "Number of watchers by state.",
		}, []string{"state"}),
	}

	if reg != nil {
		reg.MustRegister(
			m.IngestionsTotal,
			m.ParseErrors,
			m.ParseDuration,
			m.ActiveFiles,
			m.WatchersTotal,
		)
	}

	return m
}
