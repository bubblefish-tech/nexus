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
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
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
	})

	// Health / readiness probes — no authentication required.
	r.Get("/health", d.handleHealth)
	r.Get("/ready", d.handleReady)

	// Admin routes (minimal for Phase 0C — expanded in later phases).
	r.Group(func(r chi.Router) {
		r.Use(d.requireAdminToken)
		r.Get("/api/status", d.handleAdminStatus)
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
		next.ServeHTTP(ww, r)
		d.logger.Info("http request",
			"component", "daemon",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"bytes", ww.BytesWritten(),
			"latency_ms", time.Since(start).Milliseconds(),
			"request_id", middleware.GetReqID(r.Context()),
			"remote_addr", r.RemoteAddr,
		)
	})
}
