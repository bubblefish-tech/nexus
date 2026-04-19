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

// Package orchestrate implements the multi-agent orchestration engine.
// It provides nexus_list_agents, nexus_orchestrate, nexus_council,
// nexus_broadcast, and nexus_collect MCP tool semantics.
package orchestrate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/bubblefish-tech/nexus/internal/immune"
)

// Agent describes a single connected agent available for orchestration.
type Agent struct {
	AgentID        string   `json:"agent_id"`
	Name           string   `json:"name"`
	DisplayName    string   `json:"display_name,omitempty"`
	Status         string   `json:"status"`
	TrustTier      string   `json:"trust_tier"`
	Orchestratable bool     `json:"orchestratable"`
	ConnectionKind string   `json:"connection_kind"` // "http", "stdio", "tunnel", "wsl"
	Endpoint       string   `json:"endpoint,omitempty"`
	Capabilities   []string `json:"capabilities,omitempty"`
}

// AgentResult is the result of dispatching a task to one agent.
type AgentResult struct {
	AgentID    string          `json:"agent_id"`
	Status     string          `json:"status"` // "ok", "error", "token_limit_error", "immune_scan_blocked", "unsupported"
	Output     string          `json:"output,omitempty"`
	Error      string          `json:"error,omitempty"`
	LatencyMs  int64           `json:"latency_ms"`
	ScanResult immune.ScanResult `json:"scan_result"`
}

// OrchestrationRequest describes a multi-agent dispatch operation.
type OrchestrationRequest struct {
	Task         string   `json:"task"`
	TargetAgents []string `json:"target_agents,omitempty"` // empty = all orchestratable
	TimeoutMs    int64    `json:"timeout_ms,omitempty"`    // per-agent; default 30000
	FailStrategy string   `json:"fail_strategy,omitempty"` // "wait_all" | "return_partial" | "fail_fast"
	StoreResults bool     `json:"store_results,omitempty"`
}

// OrchestrationResult is the collected outcome of a nexus_orchestrate call.
type OrchestrationResult struct {
	OrchestrationID string        `json:"orchestration_id"`
	Results         []AgentResult `json:"results"`
	FailStrategy    string        `json:"fail_strategy"`
	StoredMemoryIDs []string      `json:"stored_memory_ids,omitempty"`
}

// CouncilRequest describes a multi-agent deliberation.
type CouncilRequest struct {
	Question     string   `json:"question"`
	TargetAgents []string `json:"target_agents,omitempty"`
	TimeoutMs    int64    `json:"timeout_ms,omitempty"`
}

// CouncilResult is the collected outcome of a nexus_council call.
type CouncilResult struct {
	OrchestrationID string        `json:"orchestration_id"`
	Results         []AgentResult `json:"results"`
	Synthesis       string        `json:"synthesis,omitempty"`
}

// AgentLister enumerates connected agents. Implemented by the daemon adapter.
type AgentLister interface {
	ListOrchestratableAgents(ctx context.Context) ([]Agent, error)
}

// GrantChecker verifies whether an agent holds an active grant.
type GrantChecker interface {
	HasGrant(ctx context.Context, agentID, capability string) (bool, error)
}

// MemoryWriter persists orchestration results as memories.
type MemoryWriter interface {
	WriteMemory(ctx context.Context, content, subject, actorID, derivedFrom string) (string, error)
}

// Engine orchestrates multi-agent dispatch, policy enforcement, immune scanning,
// and optional memory persistence of results.
type Engine struct {
	agents  AgentLister
	grants  GrantChecker
	memory  MemoryWriter // nil when store_results is not wired
	scanner *immune.Scanner
	logger  *slog.Logger
	client  *http.Client

	// recent stores OrchestrationResult keyed by orchestration_id for nexus_collect.
	recentMu sync.Mutex
	recent   map[string]OrchestrationResult
}

// Config holds constructor parameters for Engine.
type Config struct {
	Agents  AgentLister
	Grants  GrantChecker
	Memory  MemoryWriter // optional
	Scanner *immune.Scanner
	Logger  *slog.Logger
}

// New creates a ready-to-use Engine.
func New(cfg Config) *Engine {
	sc := cfg.Scanner
	if sc == nil {
		sc = immune.New()
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Engine{
		agents:  cfg.Agents,
		grants:  cfg.Grants,
		memory:  cfg.Memory,
		scanner: sc,
		logger:  logger,
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
		recent: make(map[string]OrchestrationResult),
	}
}

// ListAgents returns all active agents registered in the daemon's registry.
func (e *Engine) ListAgents(ctx context.Context) ([]Agent, error) {
	return e.agents.ListOrchestratableAgents(ctx)
}

// Orchestrate validates grants, dispatches tasks to target agents in parallel,
// immune-scans each result, applies the fail strategy, and optionally stores
// results as memories. The returned OrchestrationResult is also cached for
// subsequent nexus_collect calls.
func (e *Engine) Orchestrate(ctx context.Context, callerID string, req OrchestrationRequest) (OrchestrationResult, error) {
	if req.Task == "" {
		return OrchestrationResult{}, fmt.Errorf("orchestrate: task is required")
	}

	// Caller must hold the "orchestrate" capability grant.
	ok, err := e.grants.HasGrant(ctx, callerID, "orchestrate")
	if err != nil {
		return OrchestrationResult{}, fmt.Errorf("orchestrate: grant check: %w", err)
	}
	if !ok {
		return OrchestrationResult{}, fmt.Errorf("orchestrate: caller %q does not hold an orchestrate grant", callerID)
	}

	targets, err := e.resolveTargets(ctx, req.TargetAgents)
	if err != nil {
		return OrchestrationResult{}, err
	}

	// Caller must hold dispatch grant for each target.
	for _, ag := range targets {
		dispatchCap := "dispatch:" + ag.AgentID
		ok, err := e.grants.HasGrant(ctx, callerID, dispatchCap)
		if err != nil {
			return OrchestrationResult{}, fmt.Errorf("orchestrate: grant check for target %q: %w", ag.AgentID, err)
		}
		if !ok {
			return OrchestrationResult{}, fmt.Errorf("orchestrate: caller %q lacks dispatch grant for agent %q", callerID, ag.AgentID)
		}
	}

	strategy := req.FailStrategy
	if strategy == "" {
		strategy = "wait_all"
	}

	timeout := time.Duration(req.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	results := e.dispatchAll(ctx, targets, req.Task, timeout, strategy, false)

	orchID := newOrchID()
	var memIDs []string
	if req.StoreResults && e.memory != nil {
		for _, r := range results {
			if r.Status == "ok" {
				id, werr := e.memory.WriteMemory(ctx, r.Output, "orchestration", callerID, orchID)
				if werr == nil {
					memIDs = append(memIDs, id)
				}
			}
		}
	}

	out := OrchestrationResult{
		OrchestrationID: orchID,
		Results:         results,
		FailStrategy:    strategy,
		StoredMemoryIDs: memIDs,
	}
	e.cacheResult(orchID, out)

	e.logger.Info("orchestrate: completed",
		"orchestration_id", orchID,
		"caller", callerID,
		"target_count", len(targets),
		"strategy", strategy,
	)
	return out, nil
}

// Council dispatches a question to agents with require_reasoning semantics and
// synthesises the responses into a brief summary.
func (e *Engine) Council(ctx context.Context, callerID string, req CouncilRequest) (CouncilResult, error) {
	if req.Question == "" {
		return CouncilResult{}, fmt.Errorf("council: question is required")
	}

	ok, err := e.grants.HasGrant(ctx, callerID, "orchestrate")
	if err != nil {
		return CouncilResult{}, fmt.Errorf("council: grant check: %w", err)
	}
	if !ok {
		return CouncilResult{}, fmt.Errorf("council: caller %q does not hold an orchestrate grant", callerID)
	}

	targets, err := e.resolveTargets(ctx, req.TargetAgents)
	if err != nil {
		return CouncilResult{}, err
	}

	timeout := time.Duration(req.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	task := "Please reason step-by-step and then answer: " + req.Question
	results := e.dispatchAll(ctx, targets, task, timeout, "wait_all", true)

	orchID := newOrchID()
	synthesis := synthesiseCouncil(results)

	out := CouncilResult{
		OrchestrationID: orchID,
		Results:         results,
		Synthesis:       synthesis,
	}
	orchResult := OrchestrationResult{
		OrchestrationID: orchID,
		Results:         results,
		FailStrategy:    "wait_all",
	}
	e.cacheResult(orchID, orchResult)

	e.logger.Info("council: completed",
		"orchestration_id", orchID,
		"caller", callerID,
		"target_count", len(targets),
	)
	return out, nil
}

// Broadcast sends a fire-and-forget signal to all (or specified) active
// orchestratable agents. Errors from individual agents are logged, not returned.
func (e *Engine) Broadcast(ctx context.Context, callerID, signal string, agentIDs []string) error {
	if signal == "" {
		return fmt.Errorf("broadcast: signal is required")
	}

	ok, err := e.grants.HasGrant(ctx, callerID, "orchestrate")
	if err != nil {
		return fmt.Errorf("broadcast: grant check: %w", err)
	}
	if !ok {
		return fmt.Errorf("broadcast: caller %q does not hold an orchestrate grant", callerID)
	}

	targets, err := e.resolveTargets(ctx, agentIDs)
	if err != nil {
		return err
	}

	// Fire-and-forget: launch goroutines without waiting for results.
	for _, ag := range targets {
		ag := ag
		go func() {
			dispCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			r := e.dispatchOne(dispCtx, ag, signal, false)
			if r.Status != "ok" {
				e.logger.Warn("broadcast: agent dispatch failed",
					"agent_id", ag.AgentID,
					"status", r.Status,
					"error", r.Error,
				)
			}
		}()
	}

	e.logger.Info("broadcast: dispatched",
		"caller", callerID,
		"target_count", len(targets),
	)
	return nil
}

// Collect retrieves the result of a prior orchestration by ID. Results are
// cached in memory for the lifetime of the daemon process.
func (e *Engine) Collect(ctx context.Context, callerID, orchID string) (OrchestrationResult, error) {
	if orchID == "" {
		return OrchestrationResult{}, fmt.Errorf("collect: orchestration_id is required")
	}
	e.recentMu.Lock()
	result, ok := e.recent[orchID]
	e.recentMu.Unlock()
	if !ok {
		return OrchestrationResult{}, fmt.Errorf("collect: orchestration %q not found (results are in-process only)", orchID)
	}
	return result, nil
}

// resolveTargets returns the agents to dispatch to. If ids is non-empty, only
// those agents are returned (filtered by orchestratable). Otherwise all
// orchestratable agents are returned.
func (e *Engine) resolveTargets(ctx context.Context, ids []string) ([]Agent, error) {
	all, err := e.agents.ListOrchestratableAgents(ctx)
	if err != nil {
		return nil, fmt.Errorf("orchestrate: list agents: %w", err)
	}
	if len(ids) == 0 {
		return all, nil
	}
	want := make(map[string]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	var out []Agent
	for _, ag := range all {
		if want[ag.AgentID] {
			out = append(out, ag)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("orchestrate: no matching agents found for ids %v", ids)
	}
	return out, nil
}

// dispatchAll launches one goroutine per target agent and collects results
// according to the fail strategy.
func (e *Engine) dispatchAll(ctx context.Context, targets []Agent, task string, timeout time.Duration, strategy string, requireReasoning bool) []AgentResult {
	type indexed struct {
		i int
		r AgentResult
	}

	results := make([]AgentResult, len(targets))
	ch := make(chan indexed, len(targets))

	dispCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	for i, ag := range targets {
		wg.Add(1)
		go func(idx int, a Agent) {
			defer wg.Done()
			agCtx, agCancel := context.WithTimeout(dispCtx, timeout)
			defer agCancel()
			r := e.dispatchOne(agCtx, a, task, requireReasoning)
			r = e.applyImmuneScan(a.AgentID, r)
			select {
			case ch <- indexed{idx, r}:
			default:
			}
		}(i, ag)
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	for item := range ch {
		results[item.i] = item.r
		if strategy == "fail_fast" && item.r.Status == "error" {
			cancel()
		}
	}

	if strategy == "return_partial" {
		var kept []AgentResult
		for _, r := range results {
			if r.Status == "ok" {
				kept = append(kept, r)
			}
		}
		if len(kept) == 0 {
			return results // return all if none succeeded
		}
		return kept
	}

	return results
}

// dispatchOne sends a task to a single agent and returns its result.
func (e *Engine) dispatchOne(ctx context.Context, ag Agent, task string, requireReasoning bool) AgentResult {
	start := time.Now()

	if ag.ConnectionKind != "http" {
		return AgentResult{
			AgentID:   ag.AgentID,
			Status:    "unsupported",
			Error:     fmt.Sprintf("connection kind %q is not supported for orchestration", ag.ConnectionKind),
			LatencyMs: time.Since(start).Milliseconds(),
		}
	}

	if ag.Endpoint == "" {
		return AgentResult{
			AgentID:   ag.AgentID,
			Status:    "error",
			Error:     "agent has no endpoint configured",
			LatencyMs: time.Since(start).Milliseconds(),
		}
	}

	payload := map[string]any{
		"task":              task,
		"calling_agent":     "nexus",
		"require_reasoning": requireReasoning,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ag.Endpoint, bytes.NewReader(body))
	if err != nil {
		return AgentResult{
			AgentID:   ag.AgentID,
			Status:    "error",
			Error:     err.Error(),
			LatencyMs: time.Since(start).Milliseconds(),
		}
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	latencyMs := time.Since(start).Milliseconds()
	if err != nil {
		return AgentResult{
			AgentID:   ag.AgentID,
			Status:    "error",
			Error:     err.Error(),
			LatencyMs: latencyMs,
		}
	}
	defer resp.Body.Close()

	rawBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if isTokenLimitStatus(resp.StatusCode) || isTokenLimitBody(string(rawBody)) {
		return AgentResult{
			AgentID:   ag.AgentID,
			Status:    "token_limit_error",
			Error:     fmt.Sprintf("agent returned token limit error (HTTP %d)", resp.StatusCode),
			LatencyMs: latencyMs,
		}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return AgentResult{
			AgentID:   ag.AgentID,
			Status:    "error",
			Error:     fmt.Sprintf("HTTP %d: %s", resp.StatusCode, truncate(string(rawBody), 200)),
			LatencyMs: latencyMs,
		}
	}

	output := extractOutput(rawBody)
	return AgentResult{
		AgentID:   ag.AgentID,
		Status:    "ok",
		Output:    output,
		LatencyMs: latencyMs,
	}
}

// applyImmuneScan runs the immune scanner on a completed result and updates its
// status if the scan blocks the content.
func (e *Engine) applyImmuneScan(agentID string, r AgentResult) AgentResult {
	if r.Status != "ok" {
		return r
	}
	scan := e.scanner.ScanOrchestrationResult(agentID, r.Output)
	r.ScanResult = scan
	if scan.Action == "reject" || scan.Action == "quarantine" {
		r.Status = "immune_scan_blocked"
		r.Error = fmt.Sprintf("immune scanner blocked result (rule %s: %s)", scan.Rule, scan.Details)
		r.Output = ""
	}
	return r
}

// cacheResult stores an OrchestrationResult for nexus_collect access.
func (e *Engine) cacheResult(orchID string, result OrchestrationResult) {
	e.recentMu.Lock()
	e.recent[orchID] = result
	e.recentMu.Unlock()
}

// isTokenLimitStatus returns true for HTTP status codes that indicate a token
// limit has been exceeded.
func isTokenLimitStatus(code int) bool {
	return code == http.StatusRequestEntityTooLarge || code == http.StatusTooManyRequests
}

// isTokenLimitBody returns true if the response body contains token-limit
// error phrases used by common LLM APIs.
func isTokenLimitBody(body string) bool {
	lower := strings.ToLower(body)
	return strings.Contains(lower, "token limit") ||
		strings.Contains(lower, "context length exceeded") ||
		strings.Contains(lower, "context_length_exceeded") ||
		strings.Contains(lower, "maximum context length") ||
		strings.Contains(lower, "max_tokens") && strings.Contains(lower, "exceeded")
}

// extractOutput pulls the text output from a raw JSON response body.
// It handles OpenAI-compatible, generic {result}, {output}, and raw text formats.
func extractOutput(body []byte) string {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(body, &obj); err != nil {
		return strings.TrimSpace(string(body))
	}

	// OpenAI-compatible: choices[0].message.content
	if raw, ok := obj["choices"]; ok {
		var choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal(raw, &choices); err == nil && len(choices) > 0 {
			return choices[0].Message.Content
		}
	}

	// Generic: result or output string field.
	for _, key := range []string{"result", "output", "response", "text", "content"} {
		if raw, ok := obj[key]; ok {
			var s string
			if err := json.Unmarshal(raw, &s); err == nil {
				return s
			}
		}
	}

	return strings.TrimSpace(string(body))
}

// synthesiseCouncil builds a brief synthesis from successful council responses.
func synthesiseCouncil(results []AgentResult) string {
	var parts []string
	for _, r := range results {
		if r.Status == "ok" && r.Output != "" {
			parts = append(parts, fmt.Sprintf("[%s]: %s", r.AgentID, truncate(r.Output, 500)))
		}
	}
	if len(parts) == 0 {
		return "No responses received from council agents."
	}
	return strings.Join(parts, "\n\n")
}

// truncate shortens s to at most n bytes, appending "…" if cut.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// newOrchID generates a time-sortable unique ID for orchestration events.
func newOrchID() string {
	return fmt.Sprintf("orch_%d", time.Now().UnixNano())
}
