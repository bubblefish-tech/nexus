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

// Package eventsink implements the optional webhook notification layer for
// BubbleFish Nexus. It tails the write path via a lossy buffered channel and
// delivers event payloads to configured HTTP sinks with per-sink exponential
// backoff retry.
//
// INVARIANT: The event sink NEVER blocks the write path. The channel is lossy
// by design — if the channel is full, events are dropped and a metric is
// incremented.
//
// Reference: Tech Spec Section 10.
package eventsink

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// Event is the payload sent to webhook sinks after a successful WAL append.
// Reference: Tech Spec Section 10.2.
type Event struct {
	EventType   string          `json:"event_type"`
	PayloadID   string          `json:"payload_id"`
	Source      string          `json:"source"`
	Subject     string          `json:"subject"`
	Destination string          `json:"destination"`
	Timestamp   time.Time       `json:"timestamp"`
	ActorType   string          `json:"actor_type"`
	ActorID     string          `json:"actor_id"`
	Content     json.RawMessage `json:"content,omitempty"` // only when sink content="full"
}

// SinkConfig describes a single event sink.
// Type determines the delivery protocol: "webhook" (default), "syslog",
// "fluentd", or "otlp".
// Reference: v0.1.3 Build Plan Section 6.4.
type SinkConfig struct {
	Name           string
	Type           string // "webhook", "syslog", "fluentd", "otlp"
	URL            string
	TimeoutSeconds int
	MaxRetries     int
	Content        string // "summary" or "full"
	Facility       string // syslog facility (e.g. "local0")
	Tag            string // syslog/fluentd tag
	Headers        map[string]string // OTLP custom headers
}

// Metrics is the interface for event sink Prometheus counters.
type Metrics interface {
	IncDropped()
	IncDelivered()
	IncFailed()
}

// Sink is the event sink engine. Call Emit from the write path (non-blocking).
// Call Stop during shutdown to drain pending events.
type Sink struct {
	ch      chan Event
	sinks   []SinkConfig
	backoff []time.Duration
	metrics Metrics
	logger  *slog.Logger

	wg   sync.WaitGroup
	stop chan struct{}
	once sync.Once
}

// Config holds the Sink configuration.
type Config struct {
	MaxInFlight         int
	RetryBackoffSeconds []int
	Sinks               []SinkConfig
	Metrics             Metrics
	Logger              *slog.Logger
}

// New creates a Sink but does not start any goroutines. Call Start() next.
func New(cfg Config) *Sink {
	if cfg.MaxInFlight <= 0 {
		cfg.MaxInFlight = 1000
	}

	backoff := make([]time.Duration, len(cfg.RetryBackoffSeconds))
	for i, s := range cfg.RetryBackoffSeconds {
		backoff[i] = time.Duration(s) * time.Second
	}
	if len(backoff) == 0 {
		backoff = []time.Duration{
			1 * time.Second,
			5 * time.Second,
			30 * time.Second,
			5 * time.Minute,
		}
	}

	return &Sink{
		ch:      make(chan Event, cfg.MaxInFlight),
		sinks:   cfg.Sinks,
		backoff: backoff,
		metrics: cfg.Metrics,
		logger:  cfg.Logger,
		stop:    make(chan struct{}),
	}
}

// Start launches a dispatcher goroutine that fans out events to per-sink
// delivery goroutines.
func (s *Sink) Start() {
	s.wg.Add(1)
	go s.dispatch()
}

// Emit sends an event to the sink channel. It NEVER blocks — if the channel
// is full, the event is dropped and nexus_events_dropped_total is
// incremented.
//
// INVARIANT: This function is called on the write hot path. It must be
// non-blocking. Reference: Tech Spec Section 10.1.
func (s *Sink) Emit(e Event) {
	select {
	case s.ch <- e:
	default:
		s.metrics.IncDropped()
		s.logger.Warn("eventsink: event dropped — channel full",
			"component", "events",
			"payload_id", e.PayloadID,
		)
	}
}

// Stop signals the dispatcher to drain remaining events and stop. It blocks
// until all pending deliveries complete or the channel is drained.
// Safe to call multiple times (sync.Once).
func (s *Sink) Stop() {
	s.once.Do(func() {
		close(s.stop)
		s.wg.Wait()
	})
}

// dispatch reads events from the channel and delivers them to all configured
// sinks. It runs until the stop channel is closed, then drains the remaining
// buffered events.
func (s *Sink) dispatch() {
	defer s.wg.Done()

	for {
		select {
		case e := <-s.ch:
			s.deliver(e)
		case <-s.stop:
			// Drain remaining buffered events.
			for {
				select {
				case e := <-s.ch:
					s.deliver(e)
				default:
					return
				}
			}
		}
	}
}

// deliver sends an event to all configured sinks. Each sink gets its own
// retry loop with exponential backoff.
func (s *Sink) deliver(e Event) {
	for i := range s.sinks {
		sink := &s.sinks[i]
		s.deliverToSink(sink, e)
	}
}

// deliverToSink sends an event to a single sink, dispatching by Type.
// Reference: v0.1.3 Build Plan Section 6.4.
func (s *Sink) deliverToSink(sink *SinkConfig, e Event) {
	switch sink.Type {
	case "syslog":
		s.deliverSyslog(sink, e)
		return
	case "fluentd":
		s.deliverFluentd(sink, e)
		return
	case "otlp":
		s.deliverOTLP(sink, e)
		return
	default:
		// "webhook" or empty — default behavior.
	}

	s.deliverWebhook(sink, e)
}

// deliverWebhook sends an event as an HTTP POST (original webhook behavior).
func (s *Sink) deliverWebhook(sink *SinkConfig, e Event) {
	// Build the payload. In summary mode, strip content.
	payload := e
	if sink.Content != "full" {
		payload.Content = nil
	}

	body, err := json.Marshal(payload)
	if err != nil {
		s.logger.Error("eventsink: marshal event failed",
			"component", "events",
			"sink", sink.Name,
			"error", err,
		)
		s.metrics.IncFailed()
		return
	}

	timeout := time.Duration(sink.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	maxRetries := sink.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff.
			idx := attempt - 1
			if idx >= len(s.backoff) {
				idx = len(s.backoff) - 1
			}
			// Check if we should stop before sleeping.
			select {
			case <-s.stop:
				s.logger.Warn("eventsink: stopping — dropping retry",
					"component", "events",
					"sink", sink.Name,
					"payload_id", e.PayloadID,
					"attempt", attempt,
				)
				s.metrics.IncFailed()
				return
			case <-time.After(s.backoff[idx]):
			}
		}

		if s.send(sink.URL, body, timeout) {
			s.metrics.IncDelivered()
			return
		}
	}

	// All retries exhausted.
	s.logger.Warn("eventsink: max retries exhausted — dropping event",
		"component", "events",
		"sink", sink.Name,
		"payload_id", e.PayloadID,
		"max_retries", maxRetries,
	)
	s.metrics.IncFailed()
}

// send performs a single HTTP POST to the sink URL. Returns true on success
// (2xx status).
func (s *Sink) send(url string, body []byte, timeout time.Duration) bool {
	client := &http.Client{Timeout: timeout}
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		s.logger.Debug("eventsink: delivery failed",
			"component", "events",
			"url", url,
			"error", err,
		)
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}
