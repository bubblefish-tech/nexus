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

// Package transport provides physical transport implementations for A2A
// agent-to-agent communication: HTTP, stdio, tunnel, and WSL.
package transport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/BubbleFish-Nexus/internal/a2a/jsonrpc"
)

// Conn is a bidirectional JSON-RPC connection to a remote agent.
type Conn interface {
	// Send sends a JSON-RPC request and blocks until the response arrives.
	Send(ctx context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error)

	// Stream sends a JSON-RPC request and returns a channel of streaming events.
	// The channel is closed when the stream ends or an error occurs.
	Stream(ctx context.Context, req *jsonrpc.Request) (<-chan Event, error)

	// Close releases all resources held by this connection.
	Close() error
}

// Event is a streaming event received during a long-running task.
type Event struct {
	Kind    string          // "status-update", "artifact-update", "final"
	TaskID  string          // associated task ID
	Payload json.RawMessage // raw JSON payload
}

// Listener accepts incoming connections from remote agents.
type Listener interface {
	// Accept blocks until a new connection arrives or the context is cancelled.
	Accept(ctx context.Context) (Conn, error)

	// Close stops the listener.
	Close() error

	// Addr returns the address the listener is bound to.
	Addr() string
}

// Transport dials and listens for A2A connections.
type Transport interface {
	// Dial opens a client connection using the given config.
	Dial(ctx context.Context, config TransportConfig) (Conn, error)

	// Listen starts a server listener using the given config.
	Listen(ctx context.Context, config TransportConfig) (Listener, error)
}

// TransportConfig holds the configuration for dialing or listening.
type TransportConfig struct {
	Kind      string   `json:"kind" toml:"kind"`           // "http", "stdio", "tunnel", "wsl"
	URL       string   `json:"url,omitempty" toml:"url"`   // for HTTP/tunnel/WSL
	AuthType  string   `json:"authType,omitempty" toml:"auth_type"` // "bearer", "mtls", "none"
	AuthToken string   `json:"authToken,omitempty" toml:"auth_token"`
	Command   string   `json:"command,omitempty" toml:"command"` // for stdio: executable path
	Args      []string `json:"args,omitempty" toml:"args"`      // for stdio: command args
	TimeoutMs int64    `json:"timeoutMs,omitempty" toml:"timeout_ms"`
}

// Validate checks that the config is well-formed.
func (c TransportConfig) Validate() error {
	switch c.Kind {
	case "http", "tunnel", "wsl":
		if c.URL == "" {
			return fmt.Errorf("transport: %s config requires url", c.Kind)
		}
	case "stdio":
		if c.Command == "" {
			return errors.New("transport: stdio config requires command")
		}
	case "":
		return errors.New("transport: kind is required")
	default:
		return fmt.Errorf("transport: unknown kind %q", c.Kind)
	}
	return nil
}

// Get returns a Transport implementation for the given kind.
func Get(kind string) (Transport, error) {
	switch kind {
	case "http":
		return &HTTPTransport{}, nil
	case "stdio":
		return &StdioTransport{}, nil
	case "tunnel":
		return &TunnelTransport{}, nil
	case "wsl":
		return newWSLTransport()
	default:
		return nil, fmt.Errorf("transport: unknown kind %q", kind)
	}
}
