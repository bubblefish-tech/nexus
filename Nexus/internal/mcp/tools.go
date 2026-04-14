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

// Package mcp implements the Model Context Protocol JSON-RPC 2.0 server for
// BubbleFish Nexus. It exposes three tools -- nexus_write, nexus_search, and
// nexus_status -- to MCP clients (Claude Desktop, Cursor, etc.) via an HTTP
// server bound exclusively to 127.0.0.1.
//
// All tool calls route through the internal Pipeline interface, which
// applies the same auth, policy, WAL, and queue semantics as the HTTP
// handlers. MCP calls NEVER go through HTTP round-trips.
//
// Reference: Tech Spec Section 14.3.
package mcp

import (
	"context"

	"github.com/BubbleFish-Nexus/internal/destination"
	"github.com/BubbleFish-Nexus/internal/version"
)

// Pipeline is the internal interface the MCP server uses to route tool calls
// through the daemon write/query pipeline. Implementations MUST apply the
// same WAL, queue, policy, and idempotency semantics as the HTTP handlers.
//
// Reference: Tech Spec Section 14.3 -- "Internal pipeline -- not HTTP round-trip."
type Pipeline interface {
	// Write persists content through the WAL -> queue -> destination pipeline.
	// Returns a WriteResult containing the assigned payload_id on success.
	Write(ctx context.Context, params WriteParams) (WriteResult, error)

	// Search executes the 6-stage retrieval cascade and returns matching records.
	Search(ctx context.Context, params SearchParams) (SearchResult, error)

	// Status returns the current daemon health and queue state.
	Status(ctx context.Context) (StatusResult, error)
}

// WriteParams are the input parameters for the nexus_write tool.
type WriteParams struct {
	// Source is the source name resolved from MCPConfig.SourceName. Set by the
	// server before calling pipeline.Write -- never supplied by the MCP client.
	Source string

	// Content is the memory text to persist (required).
	Content string

	// Subject is the subject namespace for the memory (optional).
	Subject string

	// Collection is the destination collection (optional).
	Collection string

	// Destination is the target destination name (optional; pipeline may use source default).
	Destination string

	// ActorType is "user", "agent", or "system" (optional).
	ActorType string

	// ActorID is the identity of the actor writing the memory (optional).
	ActorID string

	// IdempotencyKey is an explicit idempotency key for deduplication.
	// If empty, the MCP server generates one automatically:
	// SHA-256(session_id || content || timestamp_second)[:64].
	// Reference: v0.1.3 Build Plan Phase 4 Subtask 4.5.
	IdempotencyKey string
}

// WriteResult is the output of the nexus_write tool.
type WriteResult struct {
	PayloadID string `json:"payload_id"`
	Status    string `json:"status"`
}

// SearchParams are the input parameters for the nexus_search tool.
type SearchParams struct {
	// Source is the source name resolved from MCPConfig.SourceName.
	Source string

	// Q is a free-text content filter (optional).
	Q string

	// Destination is the target destination name (optional).
	Destination string

	// Subject is a subject filter (optional; empty means all subjects).
	Subject string

	// Limit is the maximum number of results to return (optional; 0 -> default 20).
	Limit int

	// Profile is the retrieval profile: "fast", "balanced", or "deep" (optional).
	Profile string
}

// SearchResult is the output of the nexus_search tool.
type SearchResult struct {
	Records             []destination.TranslatedPayload `json:"records"`
	HasMore             bool                            `json:"has_more"`
	NextCursor          string                          `json:"next_cursor,omitempty"`
	RetrievalStage      int                             `json:"retrieval_stage"`
	SemanticUnavailable bool                            `json:"semantic_unavailable,omitempty"`
}

// StatusResult is the output of the nexus_status tool. It serves as an
// auto-teaching document: the FIRST thing an LLM client should call when
// connecting to Nexus. The response contains everything the LLM needs to
// use Nexus correctly without external docs.
type StatusResult struct {
	// Core status (backwards-compatible).
	Status     string `json:"status"`
	Version    string `json:"version"`
	QueueDepth int    `json:"queue_depth"`

	// Auto-teaching fields.
	Daemon   StatusDaemon    `json:"daemon"`
	Tools    []StatusTool    `json:"tools"`
	Profiles []StatusProfile `json:"profiles"`
	Sources  []string        `json:"sources"`
	Ingest   StatusIngest    `json:"ingest"`
}

// StatusDaemon holds daemon identity and uptime.
type StatusDaemon struct {
	Version       string `json:"version"`
	UptimeSeconds int64  `json:"uptime_seconds"`
}

// StatusTool describes an available MCP tool with a usage example.
type StatusTool struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Example     map[string]string `json:"example,omitempty"`
}

// StatusProfile describes a retrieval profile.
type StatusProfile struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// StatusIngest summarizes the Ingest subsystem state.
type StatusIngest struct {
	Enabled        bool `json:"enabled"`
	WatchersActive int  `json:"watchers_active"`
}

// ---------------------------------------------------------------------------
// Tool schemas (used by tools/list response)
// ---------------------------------------------------------------------------

// toolDef is the MCP tool definition returned by tools/list.
type toolDef struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema inputSchema `json:"inputSchema"`
}

type inputSchema struct {
	Type       string             `json:"type"`
	Properties map[string]propDef `json:"properties"`
	Required   []string           `json:"required,omitempty"`
}

type propDef struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

// toolList returns all MCP tool definitions including agent coordination tools.
// Reference: Tech Spec Section 14.3, AG.5.
func toolList() []toolDef {
	return []toolDef{
		{
			Name:        "nexus_write",
			Description: "Write a memory to BubbleFish Nexus. Routes through the WAL -> queue -> destination pipeline with full policy enforcement.",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]propDef{
					"content":     {Type: "string", Description: "The memory content to persist (required)."},
					"subject":     {Type: "string", Description: "Subject namespace for the memory (optional)."},
					"collection":  {Type: "string", Description: "Destination collection name (optional)."},
					"destination": {Type: "string", Description: "Target destination name (optional)."},
					"actor_type":  {Type: "string", Description: "Actor type: user, agent, or system (optional)."},
					"actor_id":    {Type: "string", Description: "Actor identifier (optional)."},
				},
				Required: []string{"content"},
			},
		},
		{
			Name:        "nexus_search",
			Description: "Search memories in BubbleFish Nexus using the 6-stage retrieval cascade.",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]propDef{
					"q":           {Type: "string", Description: "Free-text content filter (optional)."},
					"destination": {Type: "string", Description: "Target destination name (optional)."},
					"subject":     {Type: "string", Description: "Subject namespace filter (optional)."},
					"limit":       {Type: "integer", Description: "Maximum number of results (optional; default 20, max 200)."},
					"profile":     {Type: "string", Description: "Retrieval profile: fast, balanced, or deep (optional)."},
				},
			},
		},
		{
			Name:        "nexus_status",
			Description: "Return the full BubbleFish Nexus daemon status: version, available tools with examples, retrieval profiles, active sources, and ingest watcher state. Call this first when connecting to learn how to use Nexus.",
			InputSchema: inputSchema{
				Type:       "object",
				Properties: map[string]propDef{},
			},
		},
		// Agent coordination tools (AG.5).
		{
			Name:        "agent_broadcast",
			Description: "Broadcast a signal to other agents. Signals can be ephemeral (in-memory) or persistent (WAL-backed, survives restart).",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]propDef{
					"type":       {Type: "string", Description: "Signal type identifier (required)."},
					"payload":    {Type: "object", Description: "Signal payload (optional)."},
					"persistent": {Type: "boolean", Description: "If true, signal is written to WAL and survives restart (default false)."},
					"targets":    {Type: "array", Description: "Target agent IDs. Empty = broadcast to all active agents."},
				},
				Required: []string{"type"},
			},
		},
		{
			Name:        "agent_pull_signals",
			Description: "Retrieve pending signals for the calling agent. Signals are removed from the queue after delivery.",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]propDef{
					"max_n": {Type: "integer", Description: "Maximum number of signals to retrieve (default 100)."},
				},
			},
		},
		{
			Name:        "agent_status_query",
			Description: "Query the status of another registered agent by ID or name.",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]propDef{
					"agent_id": {Type: "string", Description: "Agent ID or name to query (required)."},
				},
				Required: []string{"agent_id"},
			},
		},
	}
}

// ---------------------------------------------------------------------------
// TestPipeline -- minimal Pipeline for self-test and unit tests
// ---------------------------------------------------------------------------

// TestPipeline is a no-op Pipeline implementation that returns canned
// responses. Used by `bubblefish mcp test` and unit tests that need a
// Pipeline without a running daemon.
type TestPipeline struct{}

// Write returns a canned acceptance response without touching any storage.
func (p *TestPipeline) Write(_ context.Context, _ WriteParams) (WriteResult, error) {
	return WriteResult{PayloadID: "test-payload-00000000000000000000000000000001", Status: "accepted"}, nil
}

// Search returns an empty result set without touching any storage.
func (p *TestPipeline) Search(_ context.Context, _ SearchParams) (SearchResult, error) {
	return SearchResult{Records: []destination.TranslatedPayload{}, RetrievalStage: 0}, nil
}

// Status returns a canned "ok" status without querying the daemon.
func (p *TestPipeline) Status(_ context.Context) (StatusResult, error) {
	return StatusResult{
		Status:     "ok",
		Version:    version.Version,
		QueueDepth: 0,
		Daemon:     StatusDaemon{Version: version.Version, UptimeSeconds: 0},
		Tools:      DefaultStatusTools(),
		Profiles:   DefaultStatusProfiles(),
	}, nil
}

// defaultStatusTools returns the tool descriptions for auto-teaching.
func DefaultStatusTools() []StatusTool {
	return []StatusTool{
		{
			Name:        "nexus_write",
			Description: "Persist a memory to Nexus. Content is stored durably with WAL fsync, idempotency dedup, and cryptographic provenance.",
			Example:     map[string]string{"content": "User prefers dark mode in all editors"},
		},
		{
			Name:        "nexus_search",
			Description: "Search memories using the 6-stage retrieval cascade. Returns ranked results with optional semantic similarity.",
			Example:     map[string]string{"q": "user preferences", "profile": "balanced"},
		},
		{
			Name:        "nexus_status",
			Description: "Return daemon status, available tools, retrieval profiles, and ingest state. Call this first when connecting.",
		},
	}
}

// defaultStatusProfiles returns the retrieval profile descriptions.
func DefaultStatusProfiles() []StatusProfile {
	return []StatusProfile{
		{Name: "fast", Description: "Structured lookup only, lowest latency, p99 < 5ms"},
		{Name: "balanced", Description: "Full 6-stage cascade, best results, p99 < 50ms"},
		{Name: "deep", Description: "Maximum recall, skip cache, p99 varies with dataset size"},
		{Name: "wake", Description: "Alias for fast with top_k=20 and byte_budget=4096, returns critical context in ~170 tokens"},
	}
}
