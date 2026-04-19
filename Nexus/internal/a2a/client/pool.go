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
	"log/slog"
	"sync"

	"github.com/bubblefish-tech/nexus/internal/a2a/registry"
)

// Pool manages a set of NA2A Clients keyed by agent ID. It lazily creates
// clients on first access and reuses them for subsequent calls.
type Pool struct {
	mu      sync.Mutex
	clients map[string]*Client
	factory *Factory
	logger  *slog.Logger
}

// NewPool creates a new connection pool backed by the given Factory.
func NewPool(factory *Factory, logger *slog.Logger) *Pool {
	if logger == nil {
		logger = slog.Default()
	}
	return &Pool{
		clients: make(map[string]*Client),
		factory: factory,
		logger:  logger,
	}
}

// Get returns an existing client for the agent, or creates a new one.
func (p *Pool) Get(ctx context.Context, agent registry.RegisteredAgent) (*Client, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if c, ok := p.clients[agent.AgentID]; ok {
		return c, nil
	}

	c, err := p.factory.NewClient(ctx, agent)
	if err != nil {
		return nil, err
	}

	p.clients[agent.AgentID] = c
	p.logger.Debug("pool: added client", "agent_id", agent.AgentID)
	return c, nil
}

// Close closes and removes the client for the given agent ID.
func (p *Pool) Close(agentID string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if c, ok := p.clients[agentID]; ok {
		c.Close()
		delete(p.clients, agentID)
		p.logger.Debug("pool: removed client", "agent_id", agentID)
	}
}

// CloseAll closes all clients and empties the pool.
func (p *Pool) CloseAll() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for id, c := range p.clients {
		c.Close()
		delete(p.clients, id)
	}
	p.logger.Info("pool: all clients closed")
}
