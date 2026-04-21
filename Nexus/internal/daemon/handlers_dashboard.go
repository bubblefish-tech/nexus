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
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/bubblefish-tech/nexus/internal/a2a/registry"
	dashboard "github.com/bubblefish-tech/nexus/web/dashboard"
)

// ---------------------------------------------------------------------------
// Dashboard HTML handlers (MT.5)
// These handlers serve HTML pages for the control-plane dashboard.
// Auth accepts either Authorization: Bearer <token> or ?token= query param
// so the browser can navigate to dashboard pages directly.
// ---------------------------------------------------------------------------

// serveDashboardPage validates the admin token, injects it into the HTML, and
// writes a text/html response. It returns false if auth failed (already written).
func (d *Daemon) serveDashboardPage(w http.ResponseWriter, r *http.Request, html string) bool {
	token := d.dashboardToken(w, r)
	if token == "" {
		return false
	}
	d.emitAdminAccess(r)
	tokenJSON, _ := json.Marshal(token)
	tokenStr := string(tokenJSON[1 : len(tokenJSON)-1])
	html = strings.Replace(html, "ADMIN_TOKEN: '',", "ADMIN_TOKEN: '"+tokenStr+"',", 1)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(html))
	return true
}

// dashboardToken extracts and validates the admin token from the Authorization
// header (preferred) or ?token= query parameter (for direct browser navigation).
// Returns "" and writes a 401 response if no valid token is found.
func (d *Daemon) dashboardToken(w http.ResponseWriter, r *http.Request) string {
	// Try Authorization: Bearer header first — most callers use this.
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		token := strings.TrimPrefix(auth, "Bearer ")
		cfg := d.getConfig()
		if cfg != nil && subtle.ConstantTimeCompare([]byte(token), cfg.ResolvedAdminKey) == 1 {
			return token
		}
	}
	// Fall back to ?token= query param for direct browser navigation.
	token := r.URL.Query().Get("token")
	if token == "" {
		d.writeErrorResponse(w, r, http.StatusUnauthorized, "unauthorized",
			"admin token required (Authorization header or ?token= query param)", 0)
		return ""
	}
	cfg := d.getConfig()
	if cfg == nil || subtle.ConstantTimeCompare([]byte(token), cfg.ResolvedAdminKey) != 1 {
		d.writeErrorResponse(w, r, http.StatusUnauthorized, "unauthorized", "invalid admin token", 0)
		return ""
	}
	return token
}

func (d *Daemon) handleDashboardAgents(w http.ResponseWriter, r *http.Request) {
	d.serveDashboardPage(w, r, dashboard.AgentsHTML)
}

func (d *Daemon) handleDashboardGrants(w http.ResponseWriter, r *http.Request) {
	d.serveDashboardPage(w, r, dashboard.GrantsHTML)
}

func (d *Daemon) handleDashboardApprovals(w http.ResponseWriter, r *http.Request) {
	d.serveDashboardPage(w, r, dashboard.ApprovalsHTML)
}

func (d *Daemon) handleDashboardTasks(w http.ResponseWriter, r *http.Request) {
	d.serveDashboardPage(w, r, dashboard.TasksHTML)
}

func (d *Daemon) handleDashboardActions(w http.ResponseWriter, r *http.Request) {
	d.serveDashboardPage(w, r, dashboard.ActionsHTML)
}

func (d *Daemon) handleDashboardQuarantine(w http.ResponseWriter, r *http.Request) {
	d.serveDashboardPage(w, r, dashboard.QuarantineHTML)
}

func (d *Daemon) handleDashboardMemHealth(w http.ResponseWriter, r *http.Request) {
	d.serveDashboardPage(w, r, dashboard.MemHealthHTML)
}

// ---------------------------------------------------------------------------
// GET /api/control/agents
// ---------------------------------------------------------------------------

// agentListItem is one entry in the GET /api/control/agents response.
type agentListItem struct {
	AgentID     string  `json:"agent_id"`
	Name        string  `json:"name"`
	DisplayName string  `json:"display_name"`
	Status      string  `json:"status"`
	LastSeenAt  *string `json:"last_seen_at,omitempty"`
	CreatedAt   string  `json:"created_at"`
}

// handleControlAgentList serves GET /api/control/agents.
// Lists all agents in the registry. Gated on registryStore != nil.
func (d *Daemon) handleControlAgentList(w http.ResponseWriter, r *http.Request) {
	if d.registryStore == nil {
		d.writeErrorResponse(w, r, http.StatusServiceUnavailable,
			"control_unavailable", "agent registry not available", 0)
		return
	}
	agents, err := d.registryStore.List(r.Context(), registry.ListFilter{})
	if err != nil {
		d.writeErrorResponse(w, r, http.StatusInternalServerError, "internal_error", err.Error(), 0)
		return
	}
	out := make([]agentListItem, len(agents))
	for i, a := range agents {
		item := agentListItem{
			AgentID:     a.AgentID,
			Name:        a.Name,
			DisplayName: a.DisplayName,
			Status:      a.Status,
			CreatedAt:   a.CreatedAt.Format(time.RFC3339),
		}
		if a.LastSeenAt != nil {
			s := a.LastSeenAt.Format(time.RFC3339)
			item.LastSeenAt = &s
		}
		out[i] = item
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"agents": out})
}
