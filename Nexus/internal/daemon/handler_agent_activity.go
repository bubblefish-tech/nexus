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
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

// handleAgentActivity returns recent activity events for a specific agent.
// Requires admin token authentication.
//
// GET /api/agents/{agent_id}/activity?since=<RFC3339>&limit=<N>
//
// Reference: AG.7.
func (d *Daemon) handleAgentActivity(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "agent_id")
	if agentID == "" {
		d.writeErrorResponse(w, r, http.StatusBadRequest, "missing_agent_id", "agent_id is required", 0)
		return
	}

	if d.activityLog == nil {
		d.writeErrorResponse(w, r, http.StatusServiceUnavailable, "activity_disabled", "activity logging is not enabled", 0)
		return
	}

	var since time.Time
	if sinceStr := r.URL.Query().Get("since"); sinceStr != "" {
		var err error
		since, err = time.Parse(time.RFC3339, sinceStr)
		if err != nil {
			d.writeErrorResponse(w, r, http.StatusBadRequest, "invalid_since", "since must be RFC3339 format", 0)
			return
		}
	}

	limit := 100
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
			limit = n
			if limit > 1000 {
				limit = 1000
			}
		}
	}

	events := d.activityLog.Query(agentID, since, limit)

	w.Header().Set("Content-Type", "application/json")
	resp := map[string]interface{}{
		"agent_id": agentID,
		"events":   events,
		"count":    len(events),
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		d.logger.Error("daemon: encode agent activity response",
			"component", "daemon",
			"error", err,
		)
	}
}
