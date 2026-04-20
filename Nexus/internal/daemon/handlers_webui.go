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
	"crypto/subtle"
	"net/http"
	"time"

	"github.com/bubblefish-tech/nexus/internal/discover"
	"github.com/bubblefish-tech/nexus/internal/eventbus"
)

// ---------------------------------------------------------------------------
// WEB.2 — GET /api/quarantine/count
// ---------------------------------------------------------------------------

type quarantineCountResponse struct {
	Total   int `json:"total"`
	Pending int `json:"pending"`
}

// handleQuarantineCount serves GET /api/quarantine/count — returns a fast
// summary of total and pending quarantine records for the WebUI dashboard.
func (d *Daemon) handleQuarantineCount(w http.ResponseWriter, r *http.Request) {
	d.metrics.AdminCallsTotal.WithLabelValues("/api/quarantine/count").Inc()

	if d.quarantineStore == nil {
		d.writeJSON(w, http.StatusOK, quarantineCountResponse{})
		return
	}

	total, pending, err := d.quarantineStore.Count()
	if err != nil {
		d.writeErrorResponse(w, r, http.StatusInternalServerError, "internal_error",
			"quarantine count failed", 0)
		return
	}
	d.writeJSON(w, http.StatusOK, quarantineCountResponse{
		Total:   total,
		Pending: pending,
	})
}

// ---------------------------------------------------------------------------
// WEB.2 — GET /api/discover/results
// ---------------------------------------------------------------------------

const discoveryTTL = 5 * time.Minute

type discoveredToolDTO struct {
	Name            string   `json:"name"`
	DetectionMethod string   `json:"detection_method"`
	ConnectionType  string   `json:"connection_type"`
	Endpoint        string   `json:"endpoint,omitempty"`
	Orchestratable  bool     `json:"orchestratable"`
	IngestCapable   bool     `json:"ingest_capable"`
	MCPServers      []string `json:"mcp_servers,omitempty"`
}

type discoverResultsResponse struct {
	Tools    []discoveredToolDTO `json:"tools"`
	Total    int                 `json:"total"`
	ScannedAt time.Time          `json:"scanned_at"`
}

// handleDiscoverResults serves GET /api/discover/results — runs the 5-tier
// AI-tool discovery scan (cached for 5 minutes) and returns the results.
func (d *Daemon) handleDiscoverResults(w http.ResponseWriter, r *http.Request) {
	d.metrics.AdminCallsTotal.WithLabelValues("/api/discover/results").Inc()

	if d.discoveryScanner == nil {
		d.writeJSON(w, http.StatusOK, discoverResultsResponse{Tools: []discoveredToolDTO{}})
		return
	}

	// Return cached result if within TTL.
	d.lastDiscoveryMu.RLock()
	cached := d.lastDiscovery
	scannedAt := d.lastDiscoveryAt
	d.lastDiscoveryMu.RUnlock()

	if cached != nil && time.Since(scannedAt) < discoveryTTL {
		d.writeJSON(w, http.StatusOK, toDiscoverResponse(cached, scannedAt))
		return
	}

	// Run a fresh scan with a 10-second timeout.
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	tools, err := d.discoveryScanner.FullScan(ctx)
	if err != nil {
		d.logger.Warn("daemon: discovery scan failed",
			"component", "discover",
			"error", err,
		)
		// Return last cached result on error, if available.
		if cached != nil {
			d.writeJSON(w, http.StatusOK, toDiscoverResponse(cached, scannedAt))
			return
		}
		d.writeJSON(w, http.StatusOK, discoverResultsResponse{Tools: []discoveredToolDTO{}})
		return
	}

	now := time.Now().UTC()
	d.lastDiscoveryMu.Lock()
	d.lastDiscovery = tools
	d.lastDiscoveryAt = now
	d.lastDiscoveryMu.Unlock()

	d.eventBus.Publish(eventbus.Event{
		Type: eventbus.EventDiscoveryEvent,
		Meta: map[string]string{"count": intToStr(len(tools))},
	})

	d.writeJSON(w, http.StatusOK, toDiscoverResponse(tools, now))
}

func toDiscoverResponse(tools []discover.DiscoveredTool, scannedAt time.Time) discoverResultsResponse {
	dtos := make([]discoveredToolDTO, len(tools))
	for i, t := range tools {
		servers := make([]string, len(t.MCPServers))
		for j, s := range t.MCPServers {
			servers[j] = s.Name
		}
		dtos[i] = discoveredToolDTO{
			Name:            t.Name,
			DetectionMethod: t.DetectionMethod,
			ConnectionType:  t.ConnectionType,
			Endpoint:        t.Endpoint,
			Orchestratable:  t.Orchestratable,
			IngestCapable:   t.IngestCapable,
			MCPServers:      servers,
		}
	}
	return discoverResultsResponse{
		Tools:     dtos,
		Total:     len(dtos),
		ScannedAt: scannedAt,
	}
}

func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	b := make([]byte, 0, 10)
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// ---------------------------------------------------------------------------
// WEB.3 — GET /api/events/stream (SSE activity feed)
// ---------------------------------------------------------------------------

// handleEventsStream serves GET /api/events/stream — SSE stream of WebUI
// activity events from the Event Bus Lite.
//
// Events: memory_written, memory_queried, agent_connected,
// agent_disconnected, quarantine_event, sentinel_ingest, discovery_event.
//
// Admin auth required (via wrapper handleEventsStreamWithQueryAuth).
func (d *Daemon) handleEventsStream(w http.ResponseWriter, r *http.Request) {
	d.metrics.AdminCallsTotal.WithLabelValues("/api/events/stream").Inc()

	flusher, ok := w.(http.Flusher)
	if !ok {
		d.writeErrorResponse(w, r, http.StatusInternalServerError, "internal_error",
			"streaming not supported", 0)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ch, unsub := d.eventBus.Subscribe()
	defer unsub()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case e, ok := <-ch:
			if !ok {
				return
			}
			data, err := eventbus.MarshalSSE(e)
			if err != nil {
				continue
			}
			if _, err := w.Write(data); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// handleEventsStreamWithQueryAuth wraps handleEventsStream with auth that
// accepts the admin token from either the Authorization header OR ?token=
// query param (EventSource cannot send custom headers).
func (d *Daemon) handleEventsStreamWithQueryAuth(w http.ResponseWriter, r *http.Request) {
	result, ok := d.authenticate(r)
	if !ok || !result.isAdmin {
		token := r.URL.Query().Get("token")
		if token == "" {
			d.emitAuthFailure(r, "missing")
			d.writeErrorResponse(w, r, http.StatusUnauthorized, "unauthorized",
				"admin token required (header or ?token= query param)", 0)
			return
		}
		cfg := d.getConfig()
		if subtle.ConstantTimeCompare([]byte(token), cfg.ResolvedAdminKey) != 1 {
			d.emitAuthFailure(r, "invalid_query_token")
			d.writeErrorResponse(w, r, http.StatusUnauthorized, "unauthorized",
				"invalid admin token", 0)
			return
		}
	}
	d.emitAdminAccess(r)
	d.handleEventsStream(w, r)
}
