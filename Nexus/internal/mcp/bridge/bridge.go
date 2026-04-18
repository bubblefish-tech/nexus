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

// Package bridge implements the MCP-to-NA2A bridge, exposing A2A agent
// communication as MCP tools. This lets MCP clients (Claude Desktop, etc.)
// discover and invoke NA2A agents through the standard MCP tool interface.
package bridge

import (
	"context"
	"log/slog"

	"github.com/BubbleFish-Nexus/internal/a2a/client"
	"github.com/BubbleFish-Nexus/internal/a2a/governance"
	"github.com/BubbleFish-Nexus/internal/a2a/registry"
	"github.com/BubbleFish-Nexus/internal/a2a/server"
)

// Bridge connects MCP tool calls to the NA2A client layer. It translates
// MCP tool arguments into NA2A JSON-RPC calls and converts responses back
// into MCP tool results.
type Bridge struct {
	clientPool    *client.Pool
	governance    *governance.Engine
	registry      *registry.Store
	auditSink     server.AuditSink
	identityStore *IdentityStore
	logger        *slog.Logger
}

// NewBridge creates a new MCP-to-NA2A Bridge.
func NewBridge(
	pool *client.Pool,
	gov *governance.Engine,
	reg *registry.Store,
	audit server.AuditSink,
	logger *slog.Logger,
) *Bridge {
	if logger == nil {
		logger = slog.Default()
	}
	return &Bridge{
		clientPool:    pool,
		governance:    gov,
		registry:      reg,
		auditSink:     audit,
		identityStore: NewIdentityStore(),
		logger:        logger,
	}
}

// contextKey is an unexported type for bridge context keys.
type contextKey string

const (
	// CtxKeyClientName is the MCP client name from clientInfo.
	CtxKeyClientName contextKey = "mcp_client_name"
	// CtxKeyClientVersion is the MCP client version.
	CtxKeyClientVersion contextKey = "mcp_client_version"
)

// WithClientInfo returns a context with MCP client metadata attached.
func WithClientInfo(ctx context.Context, name, version string) context.Context {
	ctx = context.WithValue(ctx, CtxKeyClientName, name)
	ctx = context.WithValue(ctx, CtxKeyClientVersion, version)
	return ctx
}

// clientNameFromCtx extracts the MCP client name from context.
func clientNameFromCtx(ctx context.Context) string {
	if v, ok := ctx.Value(CtxKeyClientName).(string); ok {
		return v
	}
	return ""
}
