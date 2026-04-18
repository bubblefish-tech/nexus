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

package registry

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/BubbleFish-Nexus/internal/a2a/jsonrpc"
	"github.com/BubbleFish-Nexus/internal/a2a/transport"
)

// HealthChecker periodically pings registered agents and updates their status.
type HealthChecker struct {
	store   *Store
	logger  *slog.Logger
	timeout time.Duration
}

// HealthCheckerOption configures a HealthChecker.
type HealthCheckerOption func(*HealthChecker)

// WithHealthTimeout sets the per-agent ping timeout.
func WithHealthTimeout(d time.Duration) HealthCheckerOption {
	return func(hc *HealthChecker) { hc.timeout = d }
}

// WithHealthLogger sets the logger.
func WithHealthLogger(l *slog.Logger) HealthCheckerOption {
	return func(hc *HealthChecker) { hc.logger = l }
}

// NewHealthChecker creates a new HealthChecker.
func NewHealthChecker(store *Store, opts ...HealthCheckerOption) *HealthChecker {
	hc := &HealthChecker{
		store:   store,
		logger:  slog.Default(),
		timeout: 10 * time.Second,
	}
	for _, opt := range opts {
		opt(hc)
	}
	return hc
}

// Check pings a single agent and updates its last_seen_at or last_error.
func (hc *HealthChecker) Check(ctx context.Context, agent RegisteredAgent) error {
	if agent.Status != StatusActive {
		return nil // only check active agents
	}

	err := hc.ping(ctx, agent)
	now := time.Now()
	if err != nil {
		hc.logger.Warn("health check failed",
			"agent_id", agent.AgentID,
			"agent_name", agent.Name,
			"error", err,
		)
		updateErr := hc.store.UpdateLastSeen(ctx, agent.AgentID, now, err.Error())
		if updateErr != nil {
			return fmt.Errorf("registry: update after failed health check: %w", updateErr)
		}
		return err
	}

	hc.logger.Debug("health check passed",
		"agent_id", agent.AgentID,
		"agent_name", agent.Name,
	)
	return hc.store.UpdateLastSeen(ctx, agent.AgentID, now, "")
}

// CheckAll pings all active agents and updates their status.
func (hc *HealthChecker) CheckAll(ctx context.Context) error {
	agents, err := hc.store.List(ctx, ListFilter{Status: StatusActive})
	if err != nil {
		return fmt.Errorf("registry: list agents for health check: %w", err)
	}

	var lastErr error
	for _, agent := range agents {
		if err := hc.Check(ctx, agent); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

// ping sends an agent/ping request to the agent via its configured transport.
func (hc *HealthChecker) ping(ctx context.Context, agent RegisteredAgent) error {
	ctx, cancel := context.WithTimeout(ctx, hc.timeout)
	defer cancel()

	t, err := transport.Get(agent.TransportConfig.Kind)
	if err != nil {
		return fmt.Errorf("get transport: %w", err)
	}

	conn, err := t.Dial(ctx, agent.TransportConfig)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()

	req, err := jsonrpc.NewRequest(jsonrpc.StringID("health-"+agent.AgentID), "agent/ping", nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := conn.Send(ctx, req)
	if err != nil {
		return fmt.Errorf("send ping: %w", err)
	}

	if resp.Error != nil {
		return fmt.Errorf("ping error: %s", resp.Error.Message)
	}
	return nil
}
