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
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// DeadManConfig configures the dead-man's switch.
type DeadManConfig struct {
	// URL is the endpoint to POST heartbeats to. Required.
	URL string

	// Interval is how often to send heartbeats. Defaults to 60s.
	Interval time.Duration

	// Timeout is the HTTP request timeout. Defaults to 10s.
	Timeout time.Duration

	// MaxFailures is the consecutive failure count that triggers a log alert.
	// Defaults to 3.
	MaxFailures int

	// Logger is the structured logger. Uses slog.Default() if nil.
	Logger *slog.Logger

	// Client is the HTTP client. Uses a default client with Timeout if nil.
	Client *http.Client
}

// DeadManSwitch sends periodic heartbeat POSTs to a configurable URL.
// If 3 consecutive POSTs fail, it logs at ERROR level. All state is in
// struct fields; no package-level variables are used.
type DeadManSwitch struct {
	url         string
	interval    time.Duration
	maxFailures int
	logger      *slog.Logger
	client      *http.Client

	failures     atomic.Int64
	totalPosts   atomic.Int64
	totalFails   atomic.Int64
	lastPostTime atomic.Int64 // unix nanos

	stopOnce sync.Once
	stopCh   chan struct{}
}

// NewDeadManSwitch creates a dead-man's switch. Call Start() to begin
// posting heartbeats. Call Stop() to shut down.
func NewDeadManSwitch(cfg DeadManConfig) *DeadManSwitch {
	if cfg.Interval <= 0 {
		cfg.Interval = 60 * time.Second
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	if cfg.MaxFailures <= 0 {
		cfg.MaxFailures = 3
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Client == nil {
		cfg.Client = &http.Client{Timeout: cfg.Timeout}
	}

	return &DeadManSwitch{
		url:         cfg.URL,
		interval:    cfg.Interval,
		maxFailures: cfg.MaxFailures,
		logger:      cfg.Logger,
		client:      cfg.Client,
		stopCh:      make(chan struct{}),
	}
}

// Start begins the heartbeat loop in a background goroutine.
// It sends an immediate heartbeat, then repeats at the configured interval.
func (d *DeadManSwitch) Start(ctx context.Context) {
	go d.loop(ctx)
}

// Stop shuts down the heartbeat loop. Safe to call multiple times (sync.Once).
func (d *DeadManSwitch) Stop() {
	d.stopOnce.Do(func() {
		close(d.stopCh)
	})
}

// ConsecutiveFailures returns the current consecutive failure count.
func (d *DeadManSwitch) ConsecutiveFailures() int64 {
	return d.failures.Load()
}

// TotalPosts returns the total number of heartbeat POST attempts.
func (d *DeadManSwitch) TotalPosts() int64 {
	return d.totalPosts.Load()
}

// TotalFailures returns the total number of failed heartbeat POSTs.
func (d *DeadManSwitch) TotalFailures() int64 {
	return d.totalFails.Load()
}

// LastPostTime returns the time of the last successful POST, or zero if none.
func (d *DeadManSwitch) LastPostTime() time.Time {
	nanos := d.lastPostTime.Load()
	if nanos == 0 {
		return time.Time{}
	}
	return time.Unix(0, nanos)
}

// Ping sends a single heartbeat POST synchronously. Returns any error.
func (d *DeadManSwitch) Ping(ctx context.Context) error {
	return d.sendHeartbeat(ctx)
}

func (d *DeadManSwitch) loop(ctx context.Context) {
	// Immediate first heartbeat.
	d.sendHeartbeat(ctx)

	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-d.stopCh:
			return
		case <-ticker.C:
			d.sendHeartbeat(ctx)
		}
	}
}

func (d *DeadManSwitch) sendHeartbeat(ctx context.Context) error {
	d.totalPosts.Add(1)

	body := fmt.Sprintf(`{"service":"nexus","timestamp":"%s"}`, time.Now().UTC().Format(time.RFC3339))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.url, strings.NewReader(body))
	if err != nil {
		d.recordFailure(err)
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		d.recordFailure(err)
		return err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		d.failures.Store(0)
		d.lastPostTime.Store(time.Now().UnixNano())
		d.logger.Debug("deadman: heartbeat sent",
			"component", "deadman",
			"url", d.url,
			"status", resp.StatusCode,
		)
		return nil
	}

	err = fmt.Errorf("deadman: heartbeat returned status %d", resp.StatusCode)
	d.recordFailure(err)
	return err
}

func (d *DeadManSwitch) recordFailure(err error) {
	d.totalFails.Add(1)
	current := d.failures.Add(1)

	if current >= int64(d.maxFailures) {
		d.logger.Error("deadman: heartbeat failures exceeded threshold",
			"component", "deadman",
			"consecutive_failures", current,
			"max_failures", d.maxFailures,
			"url", d.url,
			"error", err,
		)
	} else {
		d.logger.Warn("deadman: heartbeat failed",
			"component", "deadman",
			"consecutive_failures", current,
			"url", d.url,
			"error", err,
		)
	}
}
