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

	// Admin routes — require admin token.
	// Reference: Tech Spec Section 12, Phase 0D Behavioral Contract item 4.
	r.Group(func(r chi.Router) {
		r.Use(d.requireAdminToken)
		r.Get("/api/status", d.handleAdminStatus)
		r.Get("/api/config", d.handleAdminConfig)
		r.Get("/api/lint", d.handleLint)
		// Structured security events — admin only.
		// Reference: Tech Spec Section 12, Phase R-17.
		r.Get("/api/security/events", d.handleSecurityEvents)
		r.Get("/api/security/summary", d.handleSecuritySummary)
		// Conflict Inspector + Time-Travel — admin only, read-only.
		// Reference: Tech Spec Section 12, Phase R-22.
		r.Get("/api/conflicts", d.handleConflicts)
		r.Get("/api/timetravel", d.handleTimeTravel)
		// Live pipeline visualization SSE — admin only.
		// Reference: Tech Spec Section 12, Phase R-21.
		r.Get("/api/viz/events", d.handleVizEvents)
		// Reliability demo — admin only.
		// Reference: Tech Spec Section 12, Section 13.3, Phase R-26.
		r.Post("/api/demo/reliability", d.handleDemoReliability)
		// Audit Query API — admin only.
		// Reference: Tech Spec Addendum Section A2.5, A6.
		r.Get("/api/audit/log", d.handleAuditLog)
		r.Get("/api/audit/stats", d.handleAuditStats)
		r.Get("/api/audit/export", d.handleAuditExport)
		// Shutdown — admin only.
		r.Post("/api/shutdown", d.handleShutdown)
		// /metrics serves Prometheus text format from the private registry.
		// INVARIANT: served only from private registry; DefaultRegisterer is never used.
		r.Get("/metrics", promhttp.HandlerFor(
			d.metrics.Registry(),
			promhttp.HandlerOpts{EnableOpenMetrics: false},
		).ServeHTTP)
	})

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
