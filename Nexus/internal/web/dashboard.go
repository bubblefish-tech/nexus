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

// Package web provides the BubbleFish Nexus web dashboard.
//
// The dashboard runs on a separate port (default 8081) and requires
// admin_token authentication on all endpoints. It uses textContent
// exclusively — inner HTML is NEVER used (XSS prevention).
//
// Reference: Tech Spec Section 13.2.
package web

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/BubbleFish-Nexus/internal/version"
)

// Config holds the settings for the web dashboard.
type Config struct {
	Port        int
	RequireAuth bool
	AdminKey    []byte // Resolved admin token bytes.
	Logger      *slog.Logger
}

// Dashboard is the web dashboard server. All state is held in struct fields.
type Dashboard struct {
	cfg      Config
	server   *http.Server
	stopOnce sync.Once
}

// New creates a Dashboard but does not start it.
func New(cfg Config) *Dashboard {
	return &Dashboard{cfg: cfg}
}

// Start starts the dashboard HTTP server. It blocks until Stop() is called
// or the listener fails.
func (d *Dashboard) Start() error {
	mux := http.NewServeMux()

	// All routes require admin auth when RequireAuth is true.
	mux.HandleFunc("/", d.withAuth(d.handleIndex))
	mux.HandleFunc("/api/dashboard/status", d.withAuth(d.handleStatus))
	mux.HandleFunc("/api/dashboard/events", d.withAuth(d.handleSSE))

	addr := fmt.Sprintf("127.0.0.1:%d", d.cfg.Port)
	d.server = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	d.cfg.Logger.Info("web: dashboard starting",
		"component", "web",
		"addr", addr,
	)

	if err := d.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("web: dashboard server: %w", err)
	}
	return nil
}

// Stop gracefully shuts down the dashboard server.
func (d *Dashboard) Stop() {
	d.stopOnce.Do(func() {
		if d.server == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := d.server.Shutdown(ctx); err != nil {
			d.cfg.Logger.Error("web: dashboard shutdown error",
				"component", "web",
				"error", err,
			)
		}
		d.cfg.Logger.Info("web: dashboard stopped",
			"component", "web",
		)
	})
}

// withAuth wraps a handler with admin token authentication.
// Uses subtle.ConstantTimeCompare to prevent timing attacks.
func (d *Dashboard) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !d.cfg.RequireAuth {
			next(w, r)
			return
		}

		auth := r.Header.Get("Authorization")
		token := strings.TrimPrefix(auth, "Bearer ")
		if auth == token {
			// No "Bearer " prefix found — reject.
			writeJSONError(w, http.StatusUnauthorized, "unauthorized", "missing or malformed Authorization header")
			return
		}

		if subtle.ConstantTimeCompare([]byte(token), d.cfg.AdminKey) != 1 {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized", "invalid admin token")
			return
		}

		next(w, r)
	}
}

// handleIndex serves the dashboard HTML skeleton.
// INVARIANT: Uses textContent exclusively. NEVER inner HTML.
func (d *Dashboard) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'unsafe-inline'; style-src 'unsafe-inline'")
	w.WriteHeader(http.StatusOK)

	// Dashboard HTML — all dynamic content uses textContent, NEVER inner HTML.
	fmt.Fprint(w, dashboardHTML)
}

// handleStatus returns dashboard status as JSON.
func (d *Dashboard) handleStatus(w http.ResponseWriter, r *http.Request) {
	status := map[string]interface{}{
		"status":  "ok",
		"version": version.Version,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// handleSSE serves Server-Sent Events for live pipeline visualization.
// Events are consumed by the dashboard for real-time updates.
// Lossy: if the client is slow, events are dropped (never blocks hot paths).
//
// Reference: Tech Spec Section 13.2 — Live Pipeline Visualization.
func (d *Dashboard) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "sse_unsupported", "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// Send initial keepalive.
	fmt.Fprintf(w, "data: {\"type\":\"connected\",\"version\":%q}\n\n", version.Version)
	flusher.Flush()

	// Keep connection alive with periodic heartbeats until client disconnects.
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()
		}
	}
}

// writeJSONError writes a standard error response.
func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error":   code,
		"message": message,
	})
}

// dashboardHTML is the HTML skeleton for the web dashboard.
// CRITICAL: All dynamic content uses textContent, NEVER inner HTML.
const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>BubbleFish Nexus Dashboard</title>
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body { font-family: system-ui, -apple-system, sans-serif; background: #0a0e17; color: #e0e6ed; }
  .header { background: #131a2b; padding: 1rem 2rem; border-bottom: 1px solid #1e2a42; display: flex; align-items: center; gap: 1rem; }
  .header h1 { font-size: 1.25rem; font-weight: 600; }
  .header .version { color: #6b7fa3; font-size: 0.85rem; }
  .status-bar { display: flex; gap: 1rem; padding: 1rem 2rem; background: #0d1220; flex-wrap: wrap; }
  .status-card { background: #131a2b; border: 1px solid #1e2a42; border-radius: 8px; padding: 1rem 1.5rem; min-width: 180px; }
  .status-card .label { color: #6b7fa3; font-size: 0.75rem; text-transform: uppercase; letter-spacing: 0.05em; }
  .status-card .value { font-size: 1.5rem; font-weight: 700; margin-top: 0.25rem; }
  .status-card .value.ok { color: #34d399; }
  .status-card .value.warn { color: #fbbf24; }
  .tabs { display: flex; gap: 0; border-bottom: 1px solid #1e2a42; padding: 0 2rem; }
  .tab { padding: 0.75rem 1.5rem; color: #6b7fa3; cursor: pointer; border-bottom: 2px solid transparent; }
  .tab.active { color: #e0e6ed; border-bottom-color: #3b82f6; }
  .content { padding: 2rem; }
  .pipeline { display: flex; gap: 0.5rem; align-items: center; flex-wrap: wrap; }
  .stage { background: #131a2b; border: 1px solid #1e2a42; border-radius: 6px; padding: 0.75rem 1rem; text-align: center; min-width: 120px; }
  .stage .name { font-size: 0.8rem; color: #6b7fa3; }
  .stage .timing { font-size: 1.1rem; font-weight: 600; }
  .arrow { color: #3b82f6; font-size: 1.2rem; }
  #error-banner { display: none; background: #7f1d1d; color: #fecaca; padding: 0.75rem 2rem; }
</style>
</head>
<body>
<div class="header">
  <h1>BubbleFish Nexus</h1>
  <span class="version" id="version-label"></span>
</div>
<div id="error-banner"></div>
<div class="status-bar">
  <div class="status-card"><div class="label">Status</div><div class="value ok" id="daemon-status">Loading</div></div>
  <div class="status-card"><div class="label">Queue Depth</div><div class="value" id="queue-depth">—</div></div>
  <div class="status-card"><div class="label">WAL Pending</div><div class="value" id="wal-pending">—</div></div>
  <div class="status-card"><div class="label">Cache Hit Rate</div><div class="value" id="cache-hit">—</div></div>
</div>
<div class="tabs">
  <div class="tab active" data-tab="overview">Overview</div>
  <div class="tab" data-tab="pipeline">Pipeline</div>
  <div class="tab" data-tab="security">Security</div>
</div>
<div class="content" id="tab-content">
  <div id="overview-tab">
    <p>Dashboard connected. Waiting for status data...</p>
  </div>
</div>
<script>
(function() {
  "use strict";
  // INVARIANT: All dynamic content uses textContent. NEVER use inner-HTML.

  var versionLabel = document.getElementById("version-label");
  var statusEl = document.getElementById("daemon-status");
  var queueEl = document.getElementById("queue-depth");
  var walEl = document.getElementById("wal-pending");
  var cacheEl = document.getElementById("cache-hit");
  var errorBanner = document.getElementById("error-banner");

  // Tab switching.
  var tabs = document.querySelectorAll(".tab");
  tabs.forEach(function(tab) {
    tab.addEventListener("click", function() {
      tabs.forEach(function(t) { t.classList.remove("active"); });
      tab.classList.add("active");
    });
  });

  // Fetch dashboard status periodically (5s).
  function fetchStatus() {
    fetch("/api/dashboard/status", {
      headers: { "Authorization": "Bearer " + getToken() }
    })
    .then(function(r) { return r.json(); })
    .then(function(data) {
      // textContent only — NEVER inner HTML.
      statusEl.textContent = data.status || "unknown";
      versionLabel.textContent = "v" + (data.version || "?");
      if (data.queue_depth !== undefined) queueEl.textContent = data.queue_depth;
      if (data.wal_pending !== undefined) walEl.textContent = data.wal_pending;
      if (data.cache_hit_rate !== undefined) cacheEl.textContent = data.cache_hit_rate + "%";
      errorBanner.style.display = "none";
    })
    .catch(function(err) {
      statusEl.textContent = "unreachable";
      statusEl.className = "value warn";
      // textContent only.
      errorBanner.textContent = "Dashboard disconnected: " + err.message;
      errorBanner.style.display = "block";
    });
  }

  function getToken() {
    // Token is provided via query param or stored in sessionStorage.
    var params = new URLSearchParams(window.location.search);
    var token = params.get("token") || sessionStorage.getItem("nexus_admin_token") || "";
    if (token) sessionStorage.setItem("nexus_admin_token", token);
    return token;
  }

  fetchStatus();
  setInterval(fetchStatus, 5000);
})();
</script>
</body>
</html>`
