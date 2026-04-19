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

	"github.com/bubblefish-tech/nexus/internal/version"
)

// SourcePolicyInfo is a read-only summary of a source's policies for the
// security tab. Defined here to avoid importing config into web.
type SourcePolicyInfo struct {
	Name                string   `json:"name"`
	CanRead             bool     `json:"can_read"`
	CanWrite            bool     `json:"can_write"`
	AllowedDestinations []string `json:"allowed_destinations"`
	MaxResults          int      `json:"max_results"`
	MaxResponseBytes    int      `json:"max_response_bytes"`
	RateLimit           int      `json:"rate_limit_rpm"`
}

// AuthFailureInfo is a single auth failure event for the security tab.
type AuthFailureInfo struct {
	Timestamp  string `json:"timestamp"`
	Source     string `json:"source"`
	IP         string `json:"ip"`
	Endpoint   string `json:"endpoint"`
	TokenClass string `json:"token_class"`
	StatusCode int    `json:"status_code"`
}

// LintFinding is a single lint diagnostic for the security tab.
type LintFinding struct {
	Severity string `json:"severity"`
	Check    string `json:"check"`
	Message  string `json:"message"`
}

// SecurityProvider supplies data for the dashboard security tab.
// All methods must be safe for concurrent use.
type SecurityProvider interface {
	SourcePolicies() []SourcePolicyInfo
	AuthFailures(limit int) []AuthFailureInfo
	LintFindings() []LintFinding
}

// AuditRecordInfo is a flat summary of an interaction record for the audit tab.
// Defined here to avoid importing the audit package into web.
type AuditRecordInfo struct {
	RecordID       string  `json:"record_id"`
	Timestamp      string  `json:"timestamp"`
	Source         string  `json:"source"`
	ActorType      string  `json:"actor_type"`
	ActorID        string  `json:"actor_id"`
	OperationType  string  `json:"operation_type"`
	Endpoint       string  `json:"endpoint"`
	HTTPStatusCode int     `json:"http_status_code"`
	PolicyDecision string  `json:"policy_decision"`
	PolicyReason   string  `json:"policy_reason,omitempty"`
	LatencyMs      float64 `json:"latency_ms"`
	Destination    string  `json:"destination,omitempty"`
	Subject        string  `json:"subject,omitempty"`
	ResultCount    int     `json:"result_count,omitempty"`
}

// AuditStatsInfo holds summary statistics for the audit tab.
type AuditStatsInfo struct {
	TotalRecords      int            `json:"total_records"`
	InteractionsPerHr map[string]int `json:"interactions_per_hour"`
	DenialRate        float64        `json:"denial_rate"`
	FilterRate        float64        `json:"filter_rate"`
	TopSources        map[string]int `json:"top_sources"`
	TopActors         map[string]int `json:"top_actors"`
	ByOperation       map[string]int `json:"by_operation"`
	ByDecision        map[string]int `json:"by_decision"`
}

// AuditProvider supplies data for the dashboard audit tab.
// All methods must be safe for concurrent use.
//
// Reference: Tech Spec Addendum Section A2.7.
type AuditProvider interface {
	RecentInteractions(limit int) []AuditRecordInfo
	InteractionsByActor(actorID string, limit int) []AuditRecordInfo
	PolicyDenials(limit int) []AuditRecordInfo
	AuditStats() AuditStatsInfo
}

// Config holds the settings for the web dashboard.
type Config struct {
	Port             int
	RequireAuth      bool
	AdminKey         []byte // Resolved admin token bytes.
	Logger           *slog.Logger
	SecurityProvider SecurityProvider // Optional; security tab disabled if nil.
	AuditProvider    AuditProvider   // Optional; audit tab disabled if nil.
	AdminHandler     http.Handler    // Optional; when set, /api/* routes are delegated to this handler.
	DashboardHTML    string          // Optional; v4 dashboard HTML content. When set, replaces the builtin skeleton.
	LogoPNG          []byte          // Optional; embedded logo PNG served at /logo_metal.png.
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

	// Delegate admin API routes and control-plane dashboard pages to the
	// daemon's admin handler when configured. This allows the v4 dashboard
	// (served on this port) to call admin endpoints and navigate to
	// control-plane pages on the same origin without CORS.
	if d.cfg.AdminHandler != nil {
		mux.Handle("/api/", d.cfg.AdminHandler)
		mux.Handle("/dashboard/", d.cfg.AdminHandler)
	}

	// Serve embedded logo PNG if available. The v4 HTML references it as
	// "logo_metal.png" (relative path), so serve at /logo_metal.png.
	if len(d.cfg.LogoPNG) > 0 {
		logoData := d.cfg.LogoPNG
		mux.HandleFunc("/logo_metal.png", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("Cache-Control", "public, max-age=86400")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(logoData)
		})
	}

	// The index page is served WITHOUT auth — the admin token is injected
	// server-side into the HTML, acting as session delivery. The server
	// only binds to 127.0.0.1 so only local processes can reach it.
	// API endpoints are protected by the daemon's own requireAdminToken middleware.
	mux.HandleFunc("/", d.handleIndex)
	mux.HandleFunc("/api/dashboard/status", d.withAuth(d.handleStatus))
	mux.HandleFunc("/api/dashboard/events", d.withAuth(d.handleSSE))
	mux.HandleFunc("/api/dashboard/security", d.withAuth(d.handleSecurity))
	mux.HandleFunc("/api/dashboard/audit", d.withAuth(d.handleAudit))

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

// handleIndex serves the dashboard HTML. When DashboardHTML is configured (v4),
// it injects the admin token and disables mock mode. Otherwise falls back to
// the built-in skeleton.
// INVARIANT: Uses textContent exclusively. NEVER inner HTML.
func (d *Dashboard) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	if d.cfg.DashboardHTML != "" {
		// v4 dashboard loads Google Fonts via @import — allow in CSP.
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; script-src 'unsafe-inline'; style-src 'unsafe-inline' https://fonts.googleapis.com; font-src https://fonts.gstatic.com")
		w.WriteHeader(http.StatusOK)
		// Serve v4 dashboard with token injection.
		html := d.cfg.DashboardHTML
		html = strings.Replace(html, "MOCK_MODE: true,", "MOCK_MODE: false,", 1)
		// JSON-escape the token to prevent XSS from token values.
		escapedToken := strings.ReplaceAll(string(d.cfg.AdminKey), `\`, `\\`)
		escapedToken = strings.ReplaceAll(escapedToken, `'`, `\'`)
		html = strings.Replace(html, "ADMIN_TOKEN: '',", "ADMIN_TOKEN: '"+escapedToken+"',", 1)
		_, _ = fmt.Fprint(w, html)
		return
	}

	// Fallback: built-in skeleton dashboard.
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'unsafe-inline'; style-src 'unsafe-inline'")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, dashboardHTML)
}

// handleStatus returns dashboard status as JSON.
func (d *Dashboard) handleStatus(w http.ResponseWriter, r *http.Request) {
	status := map[string]interface{}{
		"status":  "ok",
		"version": version.Version,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
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
	_, _ = fmt.Fprintf(w, "data: {\"type\":\"connected\",\"version\":%q}\n\n", version.Version)
	flusher.Flush()

	// Keep connection alive with periodic heartbeats until client disconnects.
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			_, _ = fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()
		}
	}
}

// handleSecurity returns security tab data as JSON: source policies,
// auth failure history, and lint warnings.
// Reference: Tech Spec Section 13.2 — Security Tab.
func (d *Dashboard) handleSecurity(w http.ResponseWriter, r *http.Request) {
	if d.cfg.SecurityProvider == nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"sources":       []SourcePolicyInfo{},
			"auth_failures": []AuthFailureInfo{},
			"lint_findings": []LintFinding{},
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"sources":       d.cfg.SecurityProvider.SourcePolicies(),
		"auth_failures": d.cfg.SecurityProvider.AuthFailures(100),
		"lint_findings": d.cfg.SecurityProvider.LintFindings(),
	})
}

// handleAudit returns audit tab data as JSON: recent interactions, per-agent
// timeline, policy denials, or statistics depending on the "view" query param.
//
// Reference: Tech Spec Addendum Section A2.7.
func (d *Dashboard) handleAudit(w http.ResponseWriter, r *http.Request) {
	view := r.URL.Query().Get("view")
	if view == "" {
		view = "recent"
	}

	if d.cfg.AuditProvider == nil {
		w.Header().Set("Content-Type", "application/json")
		switch view {
		case "stats":
			_ = json.NewEncoder(w).Encode(AuditStatsInfo{
				InteractionsPerHr: map[string]int{},
				TopSources:        map[string]int{},
				TopActors:         map[string]int{},
				ByOperation:       map[string]int{},
				ByDecision:        map[string]int{},
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"records": []AuditRecordInfo{},
			})
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	switch view {
	case "agent":
		actorID := r.URL.Query().Get("actor_id")
		records := d.cfg.AuditProvider.InteractionsByActor(actorID, 50)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"records": records,
		})
	case "denials":
		records := d.cfg.AuditProvider.PolicyDenials(50)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"records": records,
		})
	case "stats":
		stats := d.cfg.AuditProvider.AuditStats()
		_ = json.NewEncoder(w).Encode(stats)
	default: // "recent"
		records := d.cfg.AuditProvider.RecentInteractions(50)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"records": records,
		})
	}
}

// writeJSONError writes a standard error response.
func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
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
  .tab-panel { display: none; }
  .tab-panel.active { display: block; }
  .pipeline { display: flex; gap: 0.5rem; align-items: center; flex-wrap: wrap; }
  .stage { background: #131a2b; border: 1px solid #1e2a42; border-radius: 6px; padding: 0.75rem 1rem; text-align: center; min-width: 120px; }
  .stage .name { font-size: 0.8rem; color: #6b7fa3; }
  .stage .timing { font-size: 1.1rem; font-weight: 600; }
  .arrow { color: #3b82f6; font-size: 1.2rem; }
  #error-banner { display: none; background: #7f1d1d; color: #fecaca; padding: 0.75rem 2rem; }
  table { width: 100%; border-collapse: collapse; margin-top: 1rem; }
  th, td { text-align: left; padding: 0.5rem 0.75rem; border-bottom: 1px solid #1e2a42; }
  th { color: #6b7fa3; font-size: 0.75rem; text-transform: uppercase; letter-spacing: 0.05em; font-weight: 600; }
  td { font-size: 0.875rem; }
  .section-title { font-size: 1rem; font-weight: 600; margin-bottom: 0.5rem; margin-top: 1.5rem; }
  .section-title:first-child { margin-top: 0; }
  .badge { display: inline-block; padding: 0.15rem 0.5rem; border-radius: 4px; font-size: 0.75rem; font-weight: 600; }
  .badge-ok { background: #064e3b; color: #34d399; }
  .badge-deny { background: #7f1d1d; color: #fca5a5; }
  .badge-warn { background: #78350f; color: #fbbf24; }
  .badge-error { background: #7f1d1d; color: #fca5a5; }
  .sec-note { color: #6b7fa3; font-size: 0.8rem; margin-top: 0.5rem; }
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
  <div class="tab" data-tab="audit">Audit</div>
</div>
<div class="content" id="tab-content">
  <div id="overview-tab" class="tab-panel active">
    <p>Dashboard connected. Waiting for status data...</p>
  </div>
  <div id="pipeline-tab" class="tab-panel">
    <p>Pipeline visualization. Waiting for events...</p>
  </div>
  <div id="security-tab" class="tab-panel">
    <div class="section-title" id="sec-policies-title">Source Policies</div>
    <p class="sec-note">Read-only view. Config edits via TOML or CLI.</p>
    <table id="sec-policies-table">
      <thead><tr>
        <th>Source</th><th>Read</th><th>Write</th>
        <th>Allowed Destinations</th><th>Max Results</th><th>Max Response Bytes</th><th>Rate Limit (RPM)</th>
      </tr></thead>
      <tbody id="sec-policies-body"></tbody>
    </table>
    <div class="section-title">Auth Failure History</div>
    <table id="sec-failures-table">
      <thead><tr>
        <th>Timestamp</th><th>Source</th><th>IP</th>
        <th>Endpoint</th><th>Token Class</th><th>Status</th>
      </tr></thead>
      <tbody id="sec-failures-body"></tbody>
    </table>
    <div class="section-title">Config Lint Warnings</div>
    <table id="sec-lint-table">
      <thead><tr>
        <th>Severity</th><th>Check</th><th>Message</th>
      </tr></thead>
      <tbody id="sec-lint-body"></tbody>
    </table>
  </div>
  <div id="audit-tab" class="tab-panel">
    <div class="section-title" id="audit-stats-title">Audit Statistics</div>
    <div class="status-bar" id="audit-stats-bar" style="padding:0;margin-bottom:1rem;">
      <div class="status-card"><div class="label">Total Records</div><div class="value" id="audit-total">—</div></div>
      <div class="status-card"><div class="label">Writes/hr</div><div class="value" id="audit-writes-hr">—</div></div>
      <div class="status-card"><div class="label">Queries/hr</div><div class="value" id="audit-queries-hr">—</div></div>
      <div class="status-card"><div class="label">Denial Rate</div><div class="value" id="audit-denial-rate">—</div></div>
      <div class="status-card"><div class="label">Filter Rate</div><div class="value" id="audit-filter-rate">—</div></div>
    </div>
    <div class="section-title">Recent Interactions (last 50)</div>
    <div style="margin-bottom:0.75rem;display:flex;gap:0.5rem;align-items:center;">
      <label style="color:#6b7fa3;font-size:0.8rem;">Agent ID:</label>
      <input type="text" id="audit-agent-filter" placeholder="Filter by actor_id" style="background:#131a2b;border:1px solid #1e2a42;color:#e0e6ed;padding:0.3rem 0.5rem;border-radius:4px;font-size:0.8rem;width:200px;">
      <button id="audit-agent-btn" style="background:#3b82f6;color:#fff;border:none;padding:0.3rem 0.75rem;border-radius:4px;cursor:pointer;font-size:0.8rem;">View Timeline</button>
      <button id="audit-recent-btn" style="background:#1e2a42;color:#e0e6ed;border:1px solid #1e2a42;padding:0.3rem 0.75rem;border-radius:4px;cursor:pointer;font-size:0.8rem;">Recent</button>
      <button id="audit-denials-btn" style="background:#1e2a42;color:#e0e6ed;border:1px solid #1e2a42;padding:0.3rem 0.75rem;border-radius:4px;cursor:pointer;font-size:0.8rem;">Denials</button>
    </div>
    <div id="audit-view-label" class="sec-note" style="margin-bottom:0.5rem;">Showing: recent interactions</div>
    <table id="audit-records-table">
      <thead><tr>
        <th>Timestamp</th><th>Source</th><th>Actor</th><th>Operation</th>
        <th>Endpoint</th><th>Status</th><th>Decision</th><th>Latency</th>
      </tr></thead>
      <tbody id="audit-records-body"></tbody>
    </table>
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

  // Tab switching — shows matching tab-panel, hides others.
  var tabs = document.querySelectorAll(".tab");
  var panels = document.querySelectorAll(".tab-panel");
  tabs.forEach(function(tab) {
    tab.addEventListener("click", function() {
      tabs.forEach(function(t) { t.classList.remove("active"); });
      tab.classList.add("active");
      var target = tab.getAttribute("data-tab");
      panels.forEach(function(p) {
        if (p.id === target + "-tab") {
          p.classList.add("active");
        } else {
          p.classList.remove("active");
        }
      });
      // Fetch security data when switching to the security tab.
      if (target === "security") { fetchSecurity(); }
      // Fetch audit data when switching to the audit tab.
      if (target === "audit") { fetchAuditRecent(); fetchAuditStats(); }
    });
  });

  // Fetch dashboard status periodically (5s).
  function fetchStatus() {
    fetch("/api/dashboard/status", {
      headers: { "Authorization": "Bearer " + getToken() }
    })
    .then(function(r) { return r.json(); })
    .then(function(data) {
      // textContent only — NEVER inner-HTML.
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

  // fetchSecurity loads security tab data from the dashboard API.
  function fetchSecurity() {
    fetch("/api/dashboard/security", {
      headers: { "Authorization": "Bearer " + getToken() }
    })
    .then(function(r) { return r.json(); })
    .then(function(data) {
      renderPolicies(data.sources || []);
      renderAuthFailures(data.auth_failures || []);
      renderLintFindings(data.lint_findings || []);
    })
    .catch(function() {});
  }

  // renderPolicies populates the source policies table.
  // INVARIANT: textContent only, NEVER inner-HTML.
  function renderPolicies(sources) {
    var tbody = document.getElementById("sec-policies-body");
    // Clear existing rows by removing children.
    while (tbody.firstChild) { tbody.removeChild(tbody.firstChild); }
    sources.forEach(function(src) {
      var tr = document.createElement("tr");

      var tdName = document.createElement("td");
      tdName.textContent = src.name;
      tr.appendChild(tdName);

      var tdRead = document.createElement("td");
      var readBadge = document.createElement("span");
      readBadge.className = src.can_read ? "badge badge-ok" : "badge badge-deny";
      readBadge.textContent = src.can_read ? "yes" : "no";
      tdRead.appendChild(readBadge);
      tr.appendChild(tdRead);

      var tdWrite = document.createElement("td");
      var writeBadge = document.createElement("span");
      writeBadge.className = src.can_write ? "badge badge-ok" : "badge badge-deny";
      writeBadge.textContent = src.can_write ? "yes" : "no";
      tdWrite.appendChild(writeBadge);
      tr.appendChild(tdWrite);

      var tdDests = document.createElement("td");
      tdDests.textContent = (src.allowed_destinations || []).join(", ") || "all";
      tr.appendChild(tdDests);

      var tdMax = document.createElement("td");
      tdMax.textContent = src.max_results || "—";
      tr.appendChild(tdMax);

      var tdBytes = document.createElement("td");
      tdBytes.textContent = src.max_response_bytes || "—";
      tr.appendChild(tdBytes);

      var tdRPM = document.createElement("td");
      tdRPM.textContent = src.rate_limit_rpm || "—";
      tr.appendChild(tdRPM);

      tbody.appendChild(tr);
    });
  }

  // renderAuthFailures populates the auth failure history table.
  // INVARIANT: textContent only, NEVER inner-HTML.
  function renderAuthFailures(failures) {
    var tbody = document.getElementById("sec-failures-body");
    while (tbody.firstChild) { tbody.removeChild(tbody.firstChild); }
    if (failures.length === 0) {
      var tr = document.createElement("tr");
      var td = document.createElement("td");
      td.setAttribute("colspan", "6");
      td.textContent = "No auth failures recorded.";
      td.style.color = "#6b7fa3";
      tr.appendChild(td);
      tbody.appendChild(tr);
      return;
    }
    failures.forEach(function(f) {
      var tr = document.createElement("tr");

      var tdTime = document.createElement("td");
      tdTime.textContent = f.timestamp;
      tr.appendChild(tdTime);

      var tdSrc = document.createElement("td");
      tdSrc.textContent = f.source;
      tr.appendChild(tdSrc);

      var tdIP = document.createElement("td");
      tdIP.textContent = f.ip;
      tr.appendChild(tdIP);

      var tdEP = document.createElement("td");
      tdEP.textContent = f.endpoint;
      tr.appendChild(tdEP);

      var tdClass = document.createElement("td");
      tdClass.textContent = f.token_class;
      tr.appendChild(tdClass);

      var tdStatus = document.createElement("td");
      var statusBadge = document.createElement("span");
      statusBadge.className = "badge badge-error";
      statusBadge.textContent = f.status_code;
      tdStatus.appendChild(statusBadge);
      tr.appendChild(tdStatus);

      tbody.appendChild(tr);
    });
  }

  // renderLintFindings populates the lint warnings table.
  // INVARIANT: textContent only, NEVER inner-HTML.
  function renderLintFindings(findings) {
    var tbody = document.getElementById("sec-lint-body");
    while (tbody.firstChild) { tbody.removeChild(tbody.firstChild); }
    if (findings.length === 0) {
      var tr = document.createElement("tr");
      var td = document.createElement("td");
      td.setAttribute("colspan", "3");
      td.textContent = "No lint warnings. Configuration looks good.";
      td.style.color = "#34d399";
      tr.appendChild(td);
      tbody.appendChild(tr);
      return;
    }
    findings.forEach(function(f) {
      var tr = document.createElement("tr");

      var tdSev = document.createElement("td");
      var sevBadge = document.createElement("span");
      sevBadge.className = f.severity === "error" ? "badge badge-error" : "badge badge-warn";
      sevBadge.textContent = f.severity;
      tdSev.appendChild(sevBadge);
      tr.appendChild(tdSev);

      var tdCheck = document.createElement("td");
      tdCheck.textContent = f.check;
      tr.appendChild(tdCheck);

      var tdMsg = document.createElement("td");
      tdMsg.textContent = f.message;
      tr.appendChild(tdMsg);

      tbody.appendChild(tr);
    });
  }

  // ── Audit tab ──────────────────────────────────────────
  // INVARIANT: All dynamic content uses textContent. NEVER use inner-HTML.

  var auditRefreshTimer = null;

  function fetchAuditRecent() {
    var label = document.getElementById("audit-view-label");
    if (label) label.textContent = "Showing: recent interactions";
    fetchAuditView("recent");
  }

  function fetchAuditDenials() {
    var label = document.getElementById("audit-view-label");
    if (label) label.textContent = "Showing: policy denials and filtered";
    fetchAuditView("denials");
  }

  function fetchAuditAgent() {
    var input = document.getElementById("audit-agent-filter");
    var actorID = input ? input.value.trim() : "";
    if (!actorID) return;
    var label = document.getElementById("audit-view-label");
    if (label) label.textContent = "Showing: timeline for " + actorID;
    fetchAuditView("agent&actor_id=" + encodeURIComponent(actorID));
  }

  function fetchAuditView(viewParam) {
    fetch("/api/dashboard/audit?view=" + viewParam, {
      headers: { "Authorization": "Bearer " + getToken() }
    })
    .then(function(r) { return r.json(); })
    .then(function(data) {
      renderAuditRecords(data.records || []);
    })
    .catch(function() {});

    // Auto-refresh every 10 seconds.
    if (auditRefreshTimer) clearInterval(auditRefreshTimer);
    auditRefreshTimer = setInterval(function() {
      fetch("/api/dashboard/audit?view=" + viewParam, {
        headers: { "Authorization": "Bearer " + getToken() }
      })
      .then(function(r) { return r.json(); })
      .then(function(data) {
        renderAuditRecords(data.records || []);
      })
      .catch(function() {});
    }, 10000);
  }

  function fetchAuditStats() {
    fetch("/api/dashboard/audit?view=stats", {
      headers: { "Authorization": "Bearer " + getToken() }
    })
    .then(function(r) { return r.json(); })
    .then(function(data) {
      var totalEl = document.getElementById("audit-total");
      var writesEl = document.getElementById("audit-writes-hr");
      var queriesEl = document.getElementById("audit-queries-hr");
      var denialEl = document.getElementById("audit-denial-rate");
      var filterEl = document.getElementById("audit-filter-rate");
      if (totalEl) totalEl.textContent = data.total_records || 0;
      var perHr = data.interactions_per_hour || {};
      if (writesEl) writesEl.textContent = perHr["write"] || 0;
      if (queriesEl) queriesEl.textContent = perHr["query"] || 0;
      if (denialEl) denialEl.textContent = ((data.denial_rate || 0) * 100).toFixed(1) + "%";
      if (filterEl) filterEl.textContent = ((data.filter_rate || 0) * 100).toFixed(1) + "%";
    })
    .catch(function() {});
  }

  // renderAuditRecords populates the interaction records table.
  // INVARIANT: textContent only, NEVER inner-HTML.
  function renderAuditRecords(records) {
    var tbody = document.getElementById("audit-records-body");
    if (!tbody) return;
    while (tbody.firstChild) { tbody.removeChild(tbody.firstChild); }
    if (records.length === 0) {
      var tr = document.createElement("tr");
      var td = document.createElement("td");
      td.setAttribute("colspan", "8");
      td.textContent = "No interaction records found.";
      td.style.color = "#6b7fa3";
      tr.appendChild(td);
      tbody.appendChild(tr);
      return;
    }
    records.forEach(function(rec) {
      var tr = document.createElement("tr");

      var tdTime = document.createElement("td");
      tdTime.textContent = rec.timestamp || "";
      tdTime.style.fontSize = "0.75rem";
      tr.appendChild(tdTime);

      var tdSrc = document.createElement("td");
      tdSrc.textContent = rec.source || "";
      tr.appendChild(tdSrc);

      var tdActor = document.createElement("td");
      tdActor.textContent = rec.actor_id || rec.actor_type || "";
      tdActor.style.fontSize = "0.8rem";
      tr.appendChild(tdActor);

      var tdOp = document.createElement("td");
      var opBadge = document.createElement("span");
      opBadge.className = "badge " + (rec.operation_type === "write" ? "badge-ok" : "badge-warn");
      opBadge.textContent = rec.operation_type || "";
      tdOp.appendChild(opBadge);
      tr.appendChild(tdOp);

      var tdEndpoint = document.createElement("td");
      tdEndpoint.textContent = rec.endpoint || "";
      tdEndpoint.style.fontSize = "0.8rem";
      tr.appendChild(tdEndpoint);

      var tdStatus = document.createElement("td");
      var statusBadge = document.createElement("span");
      var sc = rec.http_status_code || 0;
      statusBadge.className = "badge " + (sc < 300 ? "badge-ok" : sc < 500 ? "badge-warn" : "badge-error");
      statusBadge.textContent = sc;
      tdStatus.appendChild(statusBadge);
      tr.appendChild(tdStatus);

      var tdDecision = document.createElement("td");
      var decBadge = document.createElement("span");
      var dec = rec.policy_decision || "";
      decBadge.className = "badge " + (dec === "allowed" ? "badge-ok" : dec === "denied" ? "badge-deny" : "badge-warn");
      decBadge.textContent = dec;
      tdDecision.appendChild(decBadge);
      tr.appendChild(tdDecision);

      var tdLatency = document.createElement("td");
      tdLatency.textContent = (rec.latency_ms || 0).toFixed(1) + "ms";
      tr.appendChild(tdLatency);

      tbody.appendChild(tr);
    });
  }

  // Wire audit tab buttons.
  var auditRecentBtn = document.getElementById("audit-recent-btn");
  var auditDenialsBtn = document.getElementById("audit-denials-btn");
  var auditAgentBtn = document.getElementById("audit-agent-btn");
  if (auditRecentBtn) auditRecentBtn.addEventListener("click", fetchAuditRecent);
  if (auditDenialsBtn) auditDenialsBtn.addEventListener("click", fetchAuditDenials);
  if (auditAgentBtn) auditAgentBtn.addEventListener("click", fetchAuditAgent);

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
