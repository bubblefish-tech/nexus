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

// Package observability provides a lightweight OTLP/HTTP JSON exporter for
// Nexus metrics. No external OTLP libraries are used; the export payload is
// built with encoding/json and sent via net/http.
//
// Reference: Tech Spec MT.14 — OTLP Observability Export.
package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// Metric names exported by Nexus.
const (
	MetricMemoryWriteDuration  = "nexus_memory_write_duration_seconds"
	MetricCascadeStageDuration = "nexus_cascade_stage_duration_seconds"
	MetricPolicyEvalDuration   = "nexus_policy_evaluation_duration_seconds"
	MetricWALPendingEntries    = "nexus_wal_pending_entries"
	MetricWALReplayDuration    = "nexus_wal_replay_duration_seconds"
	MetricAgentRequestsTotal   = "nexus_agent_requests_total"
	MetricActiveAgents         = "nexus_active_agents"
)

// MetricType identifies the OTLP metric data type.
type MetricType int

const (
	MetricTypeGauge MetricType = iota
	MetricTypeSum
	MetricTypeHistogram
)

// MetricDefinition describes a single metric for the exporter.
type MetricDefinition struct {
	Name        string
	Description string
	Unit        string
	Type        MetricType
}

// DefaultMetrics defines the 7 Nexus metrics per MT.14.
var DefaultMetrics = []MetricDefinition{
	{MetricMemoryWriteDuration, "Duration of memory write operations", "s", MetricTypeHistogram},
	{MetricCascadeStageDuration, "Duration of each cascade retrieval stage", "s", MetricTypeHistogram},
	{MetricPolicyEvalDuration, "Duration of policy evaluation", "s", MetricTypeHistogram},
	{MetricWALPendingEntries, "Number of pending WAL entries", "1", MetricTypeGauge},
	{MetricWALReplayDuration, "Duration of WAL replay at startup", "s", MetricTypeGauge},
	{MetricAgentRequestsTotal, "Total number of agent requests", "1", MetricTypeSum},
	{MetricActiveAgents, "Number of currently active agents", "1", MetricTypeGauge},
}

// DataPoint is a single metric observation.
type DataPoint struct {
	// Name is the metric name (must match a MetricDefinition.Name).
	Name string

	// Value is the numeric value of the observation.
	Value float64

	// Attributes are key-value pairs attached to the data point.
	Attributes map[string]string

	// Timestamp is the observation time. Zero means now.
	Timestamp time.Time
}

// OTLPExporter collects metrics and exports them to an OTLP/HTTP endpoint
// as JSON. It batches data points and flushes at a configurable interval.
//
// All state is held in struct fields; there are no package-level variables.
type OTLPExporter struct {
	endpoint   string
	interval   time.Duration
	logger     *slog.Logger
	httpClient HTTPPoster
	headers    map[string]string

	mu      sync.Mutex
	buffer  []DataPoint
	metrics map[string]MetricDefinition

	stopOnce sync.Once
	stopCh   chan struct{}
	doneCh   chan struct{}
}

// HTTPPoster is the interface for sending HTTP POST requests.
type HTTPPoster interface {
	Do(req *http.Request) (*http.Response, error)
}

// ExporterConfig configures the OTLPExporter.
type ExporterConfig struct {
	// Endpoint is the OTLP/HTTP metrics endpoint URL.
	// Example: "http://localhost:4318/v1/metrics"
	Endpoint string

	// Interval is the flush interval. Zero means 60 seconds.
	Interval time.Duration

	// Logger is the structured logger. Nil means slog.Default().
	Logger *slog.Logger

	// HTTPClient overrides the default HTTP client. Nil uses a default
	// client with a 10-second timeout.
	HTTPClient HTTPPoster

	// Headers are additional HTTP headers sent with each export request.
	// Common use: {"Authorization": "Bearer <token>"}.
	Headers map[string]string
}

// Errors returned by the exporter.
var (
	ErrExporterNoEndpoint = errors.New("observability: OTLP endpoint not configured")
	ErrExporterStopped    = errors.New("observability: exporter is stopped")
)

// NewOTLPExporter creates a new exporter. Call Start() to begin periodic
// flushing, or call Flush() manually.
func NewOTLPExporter(cfg ExporterConfig) (*OTLPExporter, error) {
	if cfg.Endpoint == "" {
		return nil, ErrExporterNoEndpoint
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	interval := cfg.Interval
	if interval <= 0 {
		interval = 60 * time.Second
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	metricDefs := make(map[string]MetricDefinition, len(DefaultMetrics))
	for _, m := range DefaultMetrics {
		metricDefs[m.Name] = m
	}

	return &OTLPExporter{
		endpoint:   cfg.Endpoint,
		interval:   interval,
		logger:     logger,
		httpClient: client,
		headers:    cfg.Headers,
		buffer:     make([]DataPoint, 0, 128),
		metrics:    metricDefs,
		stopCh:     make(chan struct{}),
		doneCh:     make(chan struct{}),
	}, nil
}

// Record adds a data point to the export buffer. Thread-safe.
func (e *OTLPExporter) Record(dp DataPoint) {
	if dp.Timestamp.IsZero() {
		dp.Timestamp = time.Now()
	}
	e.mu.Lock()
	e.buffer = append(e.buffer, dp)
	e.mu.Unlock()
}

// Start begins periodic flushing in a background goroutine.
func (e *OTLPExporter) Start() {
	go func() {
		defer close(e.doneCh)
		ticker := time.NewTicker(e.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if err := e.Flush(context.Background()); err != nil {
					e.logger.Warn("observability: OTLP flush failed",
						"component", "otlp",
						"error", err,
					)
				}
			case <-e.stopCh:
				// Final flush on stop.
				_ = e.Flush(context.Background())
				return
			}
		}
	}()
}

// Stop signals the exporter to perform a final flush and stop. Uses
// sync.Once so multiple calls are safe. Blocks until the flush goroutine
// exits.
func (e *OTLPExporter) Stop() {
	e.stopOnce.Do(func() {
		close(e.stopCh)
	})
	<-e.doneCh
}

// Flush drains the buffer and sends all collected data points to the OTLP
// endpoint as a single JSON payload.
func (e *OTLPExporter) Flush(ctx context.Context) error {
	e.mu.Lock()
	if len(e.buffer) == 0 {
		e.mu.Unlock()
		return nil
	}
	points := e.buffer
	e.buffer = make([]DataPoint, 0, cap(points))
	e.mu.Unlock()

	payload := e.buildPayload(points)

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("observability: marshal OTLP payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("observability: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range e.headers {
		req.Header.Set(k, v)
	}

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("observability: send OTLP payload: %w", err)
	}
	resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("observability: OTLP endpoint returned %d", resp.StatusCode)
	}

	e.logger.Debug("observability: OTLP flush complete",
		"component", "otlp",
		"data_points", len(points),
	)
	return nil
}

// BufferLen returns the number of buffered data points. Thread-safe.
func (e *OTLPExporter) BufferLen() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.buffer)
}

// ---------------------------------------------------------------------------
// OTLP JSON payload construction
// ---------------------------------------------------------------------------

// otlpPayload is the top-level OTLP/HTTP JSON structure.
type otlpPayload struct {
	ResourceMetrics []resourceMetrics `json:"resourceMetrics"`
}

type resourceMetrics struct {
	Resource    otlpResource    `json:"resource"`
	ScopeMetrics []scopeMetrics `json:"scopeMetrics"`
}

type otlpResource struct {
	Attributes []otlpAttribute `json:"attributes"`
}

type scopeMetrics struct {
	Scope   otlpScope    `json:"scope"`
	Metrics []otlpMetric `json:"metrics"`
}

type otlpScope struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type otlpMetric struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Unit        string      `json:"unit"`
	Gauge       *otlpGauge  `json:"gauge,omitempty"`
	Sum         *otlpSum    `json:"sum,omitempty"`
	Histogram   *otlpHisto  `json:"histogram,omitempty"`
}

type otlpGauge struct {
	DataPoints []otlpNumberDP `json:"dataPoints"`
}

type otlpSum struct {
	AggregationTemporality int            `json:"aggregationTemporality"`
	IsMonotonic            bool           `json:"isMonotonic"`
	DataPoints             []otlpNumberDP `json:"dataPoints"`
}

type otlpHisto struct {
	AggregationTemporality int              `json:"aggregationTemporality"`
	DataPoints             []otlpHistoDP    `json:"dataPoints"`
}

type otlpNumberDP struct {
	Attributes       []otlpAttribute `json:"attributes,omitempty"`
	TimeUnixNano     string          `json:"timeUnixNano"`
	AsDouble         float64         `json:"asDouble"`
}

type otlpHistoDP struct {
	Attributes       []otlpAttribute `json:"attributes,omitempty"`
	TimeUnixNano     string          `json:"timeUnixNano"`
	Count            int             `json:"count"`
	Sum              float64         `json:"sum"`
}

type otlpAttribute struct {
	Key   string         `json:"key"`
	Value otlpAttrValue  `json:"value"`
}

type otlpAttrValue struct {
	StringValue string `json:"stringValue"`
}

// buildPayload groups data points by metric name and builds the OTLP JSON
// payload structure.
func (e *OTLPExporter) buildPayload(points []DataPoint) otlpPayload {
	// Group points by metric name.
	grouped := make(map[string][]DataPoint)
	for _, p := range points {
		grouped[p.Name] = append(grouped[p.Name], p)
	}

	metrics := make([]otlpMetric, 0, len(grouped))
	for name, dps := range grouped {
		def, ok := e.metrics[name]
		if !ok {
			def = MetricDefinition{Name: name, Type: MetricTypeGauge}
		}

		m := otlpMetric{
			Name:        def.Name,
			Description: def.Description,
			Unit:        def.Unit,
		}

		switch def.Type {
		case MetricTypeGauge:
			m.Gauge = &otlpGauge{DataPoints: toNumberDPs(dps)}
		case MetricTypeSum:
			m.Sum = &otlpSum{
				AggregationTemporality: 2, // CUMULATIVE
				IsMonotonic:            true,
				DataPoints:             toNumberDPs(dps),
			}
		case MetricTypeHistogram:
			m.Histogram = &otlpHisto{
				AggregationTemporality: 2, // CUMULATIVE
				DataPoints:             toHistoDPs(dps),
			}
		}

		metrics = append(metrics, m)
	}

	return otlpPayload{
		ResourceMetrics: []resourceMetrics{{
			Resource: otlpResource{
				Attributes: []otlpAttribute{{
					Key:   "service.name",
					Value: otlpAttrValue{StringValue: "nexus"},
				}},
			},
			ScopeMetrics: []scopeMetrics{{
				Scope: otlpScope{
					Name:    "github.com/bubblefish-tech/nexus/internal/observability",
					Version: "0.1.3",
				},
				Metrics: metrics,
			}},
		}},
	}
}

func toNumberDPs(dps []DataPoint) []otlpNumberDP {
	out := make([]otlpNumberDP, len(dps))
	for i, dp := range dps {
		out[i] = otlpNumberDP{
			Attributes:   toAttributes(dp.Attributes),
			TimeUnixNano: fmt.Sprintf("%d", dp.Timestamp.UnixNano()),
			AsDouble:     dp.Value,
		}
	}
	return out
}

func toHistoDPs(dps []DataPoint) []otlpHistoDP {
	out := make([]otlpHistoDP, len(dps))
	for i, dp := range dps {
		out[i] = otlpHistoDP{
			Attributes:   toAttributes(dp.Attributes),
			TimeUnixNano: fmt.Sprintf("%d", dp.Timestamp.UnixNano()),
			Count:        1,
			Sum:          dp.Value,
		}
	}
	return out
}

func toAttributes(attrs map[string]string) []otlpAttribute {
	if len(attrs) == 0 {
		return nil
	}
	out := make([]otlpAttribute, 0, len(attrs))
	for k, v := range attrs {
		out = append(out, otlpAttribute{
			Key:   k,
			Value: otlpAttrValue{StringValue: v},
		})
	}
	return out
}
