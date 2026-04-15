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

package client

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/BubbleFish-Nexus/internal/a2a/registry"
	"github.com/BubbleFish-Nexus/internal/a2a/transport"
)

// Factory creates NA2A Clients by selecting the appropriate transport
// for each registered agent, dialing, and verifying connectivity.
type Factory struct {
	transports map[string]transport.Transport
	logger     *slog.Logger
}

// NewFactory creates a Factory with all four standard transports registered.
func NewFactory(logger *slog.Logger) *Factory {
	if logger == nil {
		logger = slog.Default()
	}
	f := &Factory{
		transports: make(map[string]transport.Transport),
		logger:     logger,
	}

	// Register all supported transport kinds. We use transport.Get which
	// returns the singleton implementations.
	for _, kind := range []string{"http", "stdio", "tunnel"} {
		t, err := transport.Get(kind)
		if err == nil {
			f.transports[kind] = t
		}
	}
	// WSL transport may fail on non-Windows/non-WSL, that's fine.
	if t, err := transport.Get("wsl"); err == nil {
		f.transports["wsl"] = t
	}

	return f
}

// RegisterTransport adds or replaces a transport for the given kind.
// This is useful for testing with mock transports.
func (f *Factory) RegisterTransport(kind string, t transport.Transport) {
	f.transports[kind] = t
}

// NewClient creates a Client for the given registered agent. It selects the
// transport based on the agent's TransportConfig, dials the connection, and
// pings the remote agent to verify connectivity.
func (f *Factory) NewClient(ctx context.Context, agent registry.RegisteredAgent) (*Client, error) {
	cfg := agent.TransportConfig
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("client: invalid transport config for %s: %w", agent.AgentID, err)
	}

	t, ok := f.transports[cfg.Kind]
	if !ok {
		return nil, fmt.Errorf("client: unknown transport kind %q for agent %s", cfg.Kind, agent.AgentID)
	}

	conn, err := t.Dial(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("client: dial %s (%s): %w", agent.AgentID, cfg.Kind, err)
	}

	c := NewClient(conn, agent.AgentID, f.logger)

	// Verify connectivity with agent/card (preferred) or agent/ping.
	// Non-fatal: some agents only implement a subset of methods.
	if _, err := c.GetAgentCard(ctx); err != nil {
		if pingErr := c.Ping(ctx); pingErr != nil {
			f.logger.Warn("client: agent health check failed (continuing anyway)",
				"agent_id", agent.AgentID,
				"card_error", err,
				"ping_error", pingErr,
			)
		}
	}

	f.logger.Info("client: connected to agent",
		"agent_id", agent.AgentID,
		"transport", cfg.Kind,
	)

	return c, nil
}
