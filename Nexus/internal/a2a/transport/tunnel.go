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

package transport

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"time"

	"github.com/BubbleFish-Nexus/internal/a2a/jsonrpc"
)

const (
	tunnelBaseTimeout  = 120 * time.Second
	tunnelMaxRetries   = 5
	tunnelBaseBackoff  = 500 * time.Millisecond
	tunnelMaxBackoff   = 30 * time.Second
	tunnelJitterFactor = 0.3
)

// TunnelTransport wraps HTTP with higher timeouts and reconnect backoff.
// It is designed for long-lived connections through NAT gateways and proxies.
type TunnelTransport struct{}

// Dial opens a tunnel connection (HTTP with elevated timeouts and retry).
func (t *TunnelTransport) Dial(ctx context.Context, config TransportConfig) (Conn, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	// Override timeout if not explicitly set.
	if config.TimeoutMs <= 0 {
		config.TimeoutMs = int64(tunnelBaseTimeout / time.Millisecond)
	}

	httpT := &HTTPTransport{}
	inner, err := httpT.Dial(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("transport: tunnel dial: %w", err)
	}

	return &tunnelConn{
		inner:  inner,
		config: config,
		httpT:  httpT,
	}, nil
}

// Listen starts a tunnel listener (delegates to HTTP).
func (t *TunnelTransport) Listen(ctx context.Context, config TransportConfig) (Listener, error) {
	if config.TimeoutMs <= 0 {
		config.TimeoutMs = int64(tunnelBaseTimeout / time.Millisecond)
	}
	httpT := &HTTPTransport{}
	return httpT.Listen(ctx, config)
}

// tunnelConn wraps an HTTP connection with retry logic on transient failures.
type tunnelConn struct {
	inner  Conn
	config TransportConfig
	httpT  *HTTPTransport
}

// Send sends a request with automatic retry and exponential backoff.
func (c *tunnelConn) Send(ctx context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
	var lastErr error
	for attempt := 0; attempt <= tunnelMaxRetries; attempt++ {
		if attempt > 0 {
			backoff := tunnelBackoff(attempt)
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			// Reconnect.
			c.inner.Close()
			inner, err := c.httpT.Dial(ctx, c.config)
			if err != nil {
				lastErr = err
				continue
			}
			c.inner = inner
		}

		resp, err := c.inner.Send(ctx, req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("transport: tunnel send after %d retries: %w", tunnelMaxRetries, lastErr)
}

// Stream delegates to the inner connection's Stream with retry.
func (c *tunnelConn) Stream(ctx context.Context, req *jsonrpc.Request) (<-chan Event, error) {
	var lastErr error
	for attempt := 0; attempt <= tunnelMaxRetries; attempt++ {
		if attempt > 0 {
			backoff := tunnelBackoff(attempt)
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			c.inner.Close()
			inner, err := c.httpT.Dial(ctx, c.config)
			if err != nil {
				lastErr = err
				continue
			}
			c.inner = inner
		}

		ch, err := c.inner.Stream(ctx, req)
		if err == nil {
			return ch, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("transport: tunnel stream after %d retries: %w", tunnelMaxRetries, lastErr)
}

// Close closes the underlying connection.
func (c *tunnelConn) Close() error {
	return c.inner.Close()
}

// tunnelBackoff calculates exponential backoff with jitter.
func tunnelBackoff(attempt int) time.Duration {
	base := float64(tunnelBaseBackoff) * math.Pow(2, float64(attempt-1))
	if base > float64(tunnelMaxBackoff) {
		base = float64(tunnelMaxBackoff)
	}
	jitter := base * tunnelJitterFactor * (rand.Float64()*2 - 1) //nolint:gosec
	d := time.Duration(base + jitter)
	if d < 0 {
		d = tunnelBaseBackoff
	}
	return d
}
