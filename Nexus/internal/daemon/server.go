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

package daemon

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	// HTTP server timeout constants.
	// Reference: Tech Spec Section 6.6 (timeout table), Phase 0C Behavioral
	// Contract item 7.
	httpReadHeaderTimeout = 10 * time.Second
	httpReadTimeout       = 30 * time.Second
	httpWriteTimeout      = 60 * time.Second
	httpIdleTimeout       = 120 * time.Second
)

// buildRouter creates a chi.Router with all routes registered, using the
// daemon's middleware and handlers.
//
// Route layout:
//
//	POST   /inbound/{source}        — write path (data key required)
//	GET    /query/{destination}     — read path (data key required)
//	GET    /health                  — liveness probe (no auth)
//	GET    /ready                   — readiness probe (no auth)
//
// Reference: Tech Spec Section 12, Phase 0C Behavioral Contract item 13.
func (d *Daemon) buildRouter() http.Handler {
	r := chi.NewRouter()

	// CORS middleware — allow cross-origin requests from localhost origins.
	r.Use(corsMiddleware)

	// Request ID middleware — attach a unique request_id to every request.
	r.Use(middleware.RequestID)

	// Structured logging middleware — logs each request with component field.
	r.Use(d.loggingMiddleware)

	// Data-plane routes require a source API key (not admin token).
	r.Group(func(r chi.Router) {
		r.Use(d.requireDataToken)
		r.Post("/inbound/{source}", d.handleWrite)
		r.Get("/query/{destination}", d.handleQuery)
		// OpenAI-compatible write endpoint.
		// Reference: Tech Spec Section 12, Phase 7 Behavioral Contract 7.
		r.Post("/v1/memories", d.handleOpenAIWrite)
	})

	// Health / readiness probes — no authentication required.
	r.Get("/health", d.handleHealth)
	r.Get("/ready", d.handleReady)

	// Credential gateway proxy — authenticated by synthetic key, not admin
	// or data token. The proxy handlers validate the Bearer token internally.
	// Reference: AG.3.
	if d.credentialGateway != nil {
		r.Post("/v1/chat/completions", d.credentialOpenAIProxy().ServeHTTP)
		r.Post("/v1/messages", d.credentialAnthropicProxy().ServeHTTP)
	}

	// Admin routes — require admin token.
	// Reference: Tech Spec Section 12, Phase 0D Behavioral Contract item 4.
	r.Group(func(r chi.Router) {
		r.Use(d.requireAdminToken)
		r.Get("/api/status", d.handleAdminStatus)
		r.Get("/api/cache", d.handleAdminCache)
		r.Get("/api/policies", d.handleAdminPolicies)
		r.Get("/api/config", d.handleAdminConfig)
		r.Get("/api/lint", d.handleLint)
		r.Get("/api/health/memory", d.handleMemoryHealth)
		// Structured security events — admin only.
		// Reference: Tech Spec Section 12, Phase R-17.
		r.Get("/api/security/events", d.handleSecurityEvents)
		r.Get("/api/security/summary", d.handleSecuritySummary)
		// Conflict Inspector + Time-Travel — admin only, read-only.
		// Reference: Tech Spec Section 12, Phase R-22.
		r.Get("/api/conflicts", d.handleConflicts)
		r.Get("/api/timetravel", d.handleTimeTravel)
		// Live pipeline visualization SSE — admin only.
		// Moved to separate route below for query-param token support.
		// r.Get("/api/viz/events", d.handleVizEvents)
		// Reliability demo — admin only.
		// Reference: Tech Spec Section 12, Section 13.3, Phase R-26.
		r.Post("/api/demo/reliability", d.handleDemoReliability)
		// Audit Query API — admin only.
		// Reference: Tech Spec Addendum Section A2.5, A6.
		r.Get("/api/audit/log", d.handleAuditLog)
		r.Get("/api/audit/stats", d.handleAuditStats)
		r.Get("/api/audit/export", d.handleAuditExport)
		// Cryptographic provenance — admin only.
		// Reference: v0.1.3 Build Plan Phase 4 Subtasks 4.6, 4.9.
		r.Get("/verify/{memory_id}", d.handleVerify)
		r.Post("/api/prove", d.handleProve)
		// Admin memory list — stable cursor pagination over all rows.
		// Used by chaos verifier and ops debugging.
		r.Get("/admin/memories", d.handleAdminList)
		// Shutdown — admin only.
		r.Post("/api/shutdown", d.handleShutdown)

		// Control-plane (MT.2) — grants, approvals, tasks, actions. Routes
		// register only when setupControlPlane succeeded.
		if d.grantStore != nil {
			r.Post("/api/control/grants", d.handleControlGrantCreate)
			r.Get("/api/control/grants", d.handleControlGrantList)
			r.Delete("/api/control/grants/{id}", d.handleControlGrantRevoke)

			r.Post("/api/control/approvals", d.handleControlApprovalCreate)
			r.Get("/api/control/approvals", d.handleControlApprovalList)
			r.Post("/api/control/approvals/{id}", d.handleControlApprovalDecide)

			r.Post("/api/control/tasks", d.handleControlTaskCreate)
			r.Get("/api/control/tasks/{id}", d.handleControlTaskGet)
			r.Get("/api/control/tasks", d.handleControlTaskList)
			r.Patch("/api/control/tasks/{id}", d.handleControlTaskUpdate)

			r.Get("/api/control/actions", d.handleControlActionQuery)

			r.Get("/api/control/lineage/{id}", d.handleControlLineage)
		}

		// Agent list — MT.5, gated on registryStore.
		// Admin agent registration — POST /a2a/admin/register-agent.
		if d.registryStore != nil {
			r.Get("/api/control/agents", d.handleControlAgentList)
			r.Post("/a2a/admin/register-agent", d.handleA2AAdminRegisterAgent)
		}

		// Quarantine API (DEF.2) — immune-scanner interceptions. Gated on
		// quarantineStore so routes do not register when the DB failed to open.
		if d.quarantineStore != nil {
			r.Get("/api/quarantine", d.handleQuarantineList)
			r.Get("/api/quarantine/{id}", d.handleQuarantineGet)
			r.Post("/api/quarantine/{id}/approve", d.handleQuarantineApprove)
			r.Post("/api/quarantine/{id}/reject", d.handleQuarantineReject)
			// WEB.2: quarantine count for dashboard.
			r.Get("/api/quarantine/count", d.handleQuarantineCount)
		}

		// WEB.2: audit chain length for dashboard.
		r.Get("/api/audit/status", d.handleAuditStatus)
		// WEB.2: AI-tool discovery results for dashboard.
		r.Get("/api/discover/results", d.handleDiscoverResults)
		// SHOW.2: memory graph for D3.js visualization.
		r.Get("/api/viz/memory-graph", d.handleMemoryGraph)

		// /metrics serves Prometheus text format from the private registry.
		// INVARIANT: served only from private registry; DefaultRegisterer is never used.
		r.Get("/metrics", promhttp.HandlerFor(
			d.metrics.Registry(),
			promhttp.HandlerOpts{EnableOpenMetrics: false},
		).ServeHTTP)
	})

	// NA2A JSON-RPC endpoint — no daemon-level auth; methods authenticate
	// individually (e.g. agent/register uses registration_token param).
	// Gated on a2aServer so the route only appears when A2A is enabled.
	if d.a2aServer != nil {
		r.Post("/a2a/jsonrpc", d.handleA2AJSONRPC)
	}

	// SSE endpoints — accept admin token from Authorization header OR ?token=
	// query param (EventSource cannot send custom headers).
	// Reference: dashboard-contract.md Authentication section.
	r.Get("/api/viz/events", d.handleVizEventsWithQueryAuth)
	// WEB.3: WebUI activity feed SSE endpoint.
	r.Get("/api/events/stream", d.handleEventsStreamWithQueryAuth)

	// Control-plane dashboard HTML pages — MT.5. Accept token from
	// Authorization header or ?token= query param (browser navigation).
	// Gated on grantStore so they only appear when the control plane is up.
	if d.grantStore != nil {
		r.Get("/dashboard/agents", d.handleDashboardAgents)
		r.Get("/dashboard/grants", d.handleDashboardGrants)
		r.Get("/dashboard/approvals", d.handleDashboardApprovals)
		r.Get("/dashboard/tasks", d.handleDashboardTasks)
		r.Get("/dashboard/actions", d.handleDashboardActions)
	}

	// Quarantine dashboard (DEF.2) — gated on quarantineStore.
	if d.quarantineStore != nil {
		r.Get("/dashboard/quarantine", d.handleDashboardQuarantine)
	}

	// SHOW.2: memory graph dashboard — always registered.
	r.Get("/dashboard/memgraph", d.handleDashboardMemgraph)
	r.Get("/dashboard/memhealth", d.handleDashboardMemHealth)

	// Review routes — require bfn_review_list_ or bfn_review_read_ tokens.
	// Any other token class receives 401 wrong_token_class.
	// Reference: v0.1.3 Build Plan Phase 2 Subtask 2.3.
	r.Group(func(r chi.Router) {
		r.Use(d.requireReviewListToken)
		r.Get("/api/review/quarantine", d.handleReviewList)
	})
	r.Group(func(r chi.Router) {
		r.Use(d.requireReviewReadToken)
		r.Get("/api/review/quarantine/{id}", d.handleReviewRead)
	})

	// Global request timeout: catches rogue handlers that don't respect context cancellation.
	return http.TimeoutHandler(r, 60*time.Second, `{"error":"request_timeout","message":"request exceeded 60s deadline"}`)
}

// BuildAdminRouter creates a chi router with all admin API routes and their
// auth middleware. This is used both by the daemon's data-plane router and
// by the web dashboard server (port 8081) to serve admin endpoints on the
// same origin as the dashboard HTML.
func (d *Daemon) BuildAdminRouter() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(d.loggingMiddleware)

	r.Group(func(r chi.Router) {
		r.Use(d.requireAdminToken)
		r.Get("/api/status", d.handleAdminStatus)
		r.Get("/api/cache", d.handleAdminCache)
		r.Get("/api/policies", d.handleAdminPolicies)
		r.Get("/api/config", d.handleAdminConfig)
		r.Get("/api/lint", d.handleLint)
		r.Get("/api/security/events", d.handleSecurityEvents)
		r.Get("/api/security/summary", d.handleSecuritySummary)
		r.Get("/api/conflicts", d.handleConflicts)
		r.Get("/api/timetravel", d.handleTimeTravel)
		r.Post("/api/demo/reliability", d.handleDemoReliability)
		r.Get("/api/audit/log", d.handleAuditLog)
		r.Get("/api/audit/stats", d.handleAuditStats)
		r.Get("/api/audit/export", d.handleAuditExport)
		r.Get("/admin/memories", d.handleAdminList)
		r.Get("/api/memories", d.handleAdminList)
		r.Post("/api/shutdown", d.handleShutdown)
		r.Get("/api/agents/{agent_id}/sessions", d.handleAgentSessions)
		r.Get("/api/agents/{agent_id}/activity", d.handleAgentActivity)
		r.Post("/api/agents/{agent_id}/heartbeat", d.handleAgentHeartbeat)

		// Crypto status endpoints (T1-5)
		r.Get("/api/crypto/signing", d.handleCryptoSigning)
		r.Get("/api/crypto/profile", d.handleCryptoProfile)
		r.Get("/api/crypto/master", d.handleCryptoMaster)
		r.Get("/api/crypto/ratchet", d.handleCryptoRatchet)

		// BF-Sketch substrate endpoints (BS.9)
		r.Get("/api/substrate/status", d.handleSubstrateStatus)
		r.Post("/api/substrate/rotate-ratchet", d.handleSubstrateRotateRatchet)
		r.Post("/api/substrate/prove-deletion", d.handleSubstrateProveDeletion)
		r.Post("/api/substrate/shred", d.handleSubstrateShred)

		// Control-plane (MT.2) — grants, approvals, tasks, actions.
		if d.grantStore != nil {
			r.Post("/api/control/grants", d.handleControlGrantCreate)
			r.Get("/api/control/grants", d.handleControlGrantList)
			r.Delete("/api/control/grants/{id}", d.handleControlGrantRevoke)

			r.Post("/api/control/approvals", d.handleControlApprovalCreate)
			r.Get("/api/control/approvals", d.handleControlApprovalList)
			r.Post("/api/control/approvals/{id}", d.handleControlApprovalDecide)

			r.Post("/api/control/tasks", d.handleControlTaskCreate)
			r.Get("/api/control/tasks/{id}", d.handleControlTaskGet)
			r.Get("/api/control/tasks", d.handleControlTaskList)
			r.Patch("/api/control/tasks/{id}", d.handleControlTaskUpdate)

			r.Get("/api/control/actions", d.handleControlActionQuery)

			r.Get("/api/control/lineage/{id}", d.handleControlLineage)
		}

		// Agent list — MT.5, gated on registryStore.
		// Admin agent registration — POST /a2a/admin/register-agent.
		if d.registryStore != nil {
			r.Get("/api/control/agents", d.handleControlAgentList)
			r.Post("/a2a/admin/register-agent", d.handleA2AAdminRegisterAgent)
		}

		// Quarantine API (DEF.2) — BuildAdminRouter mirror.
		if d.quarantineStore != nil {
			r.Get("/api/quarantine", d.handleQuarantineList)
			r.Get("/api/quarantine/{id}", d.handleQuarantineGet)
			r.Post("/api/quarantine/{id}/approve", d.handleQuarantineApprove)
			r.Post("/api/quarantine/{id}/reject", d.handleQuarantineReject)
			r.Get("/api/quarantine/count", d.handleQuarantineCount)
		}

		// WEB.2: audit status + discovery results.
		r.Get("/api/audit/status", d.handleAuditStatus)
		r.Get("/api/discover/results", d.handleDiscoverResults)
		// SHOW.2: memory graph for D3.js visualization.
		r.Get("/api/viz/memory-graph", d.handleMemoryGraph)

		r.Get("/metrics", promhttp.HandlerFor(
			d.metrics.Registry(),
			promhttp.HandlerOpts{EnableOpenMetrics: false},
		).ServeHTTP)

		// Secure pprof — uses chi's self-contained subrouter, NOT http.DefaultServeMux.
		r.Mount("/debug", middleware.Profiler())
	})

	// NA2A JSON-RPC endpoint — no daemon-level auth; methods authenticate
	// individually. Mirrored here so the admin router also accepts agent calls.
	if d.a2aServer != nil {
		r.Post("/a2a/jsonrpc", d.handleA2AJSONRPC)
	}

	// SSE with query-param auth.
	r.Get("/api/viz/events", d.handleVizEventsWithQueryAuth)
	// WEB.3: WebUI activity feed SSE.
	r.Get("/api/events/stream", d.handleEventsStreamWithQueryAuth)

	// Control-plane dashboard HTML pages — MT.5. Accept token from
	// Authorization header or ?token= query param (browser navigation).
	if d.grantStore != nil {
		r.Get("/dashboard/agents", d.handleDashboardAgents)
		r.Get("/dashboard/grants", d.handleDashboardGrants)
		r.Get("/dashboard/approvals", d.handleDashboardApprovals)
		r.Get("/dashboard/tasks", d.handleDashboardTasks)
		r.Get("/dashboard/actions", d.handleDashboardActions)
	}

	// Quarantine dashboard (DEF.2).
	if d.quarantineStore != nil {
		r.Get("/dashboard/quarantine", d.handleDashboardQuarantine)
	}

	// SHOW.2: memory graph dashboard — always registered.
	r.Get("/dashboard/memgraph", d.handleDashboardMemgraph)

	return r
}

// newHTTPServer creates an *http.Server with all four required timeouts set.
// The addr argument is "host:port" format.
//
// All four timeouts MUST be set. Omitting any one is a security defect
// (slowloris, resource exhaustion).
//
// Reference: Phase 0C Behavioral Contract item 7, Invariant 6.
func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: httpReadHeaderTimeout,
		ReadTimeout:       httpReadTimeout,
		WriteTimeout:      httpWriteTimeout,
		IdleTimeout:       httpIdleTimeout,
	}
}

// serverAddr returns the "bind:port" address string for the HTTP server.
func (d *Daemon) serverAddr() string {
	return fmt.Sprintf("%s:%d", d.cfg.Daemon.Bind, d.cfg.Daemon.Port)
}

// loggingMiddleware is a chi-compatible middleware that logs each request
// at INFO level with structured fields required by the spec.
//
// Required fields: time, level, msg, component, source (request path),
// subject, request_id.
// Reference: Tech Spec Section 11.1, Phase 0C Behavioral Contract item 19.
func (d *Daemon) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		start := time.Now()

		// Derive effective client IP from trusted proxy headers or TCP source.
		// Reference: Tech Spec Section 6.3.
		clientIP := d.proxies.effectiveClientIP(r)

		// Store effective_client_ip in context for downstream use (rate limiting,
		// security events). Reference: Tech Spec Section 6.3.
		ctx := context.WithValue(r.Context(), ctxEffectiveClientIP, clientIP)
		next.ServeHTTP(ww, r.WithContext(ctx))

		d.logger.Info("http request",
			"component", "daemon",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"bytes", ww.BytesWritten(),
			"latency_ms", time.Since(start).Milliseconds(),
			"request_id", middleware.GetReqID(r.Context()),
			"effective_client_ip", clientIP,
			"remote_addr", r.RemoteAddr,
		)
	})
}
