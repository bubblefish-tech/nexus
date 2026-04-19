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

	"github.com/bubblefish-tech/nexus/internal/agent"
	"github.com/go-chi/chi/v5"
)

// handleAgentHeartbeat processes heartbeat pings from agents.
//
// POST /api/agents/{agent_id}/heartbeat
//
// Reference: AG.8.
func (d *Daemon) handleAgentHeartbeat(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "agent_id")
	if agentID == "" {
		d.writeErrorResponse(w, r, http.StatusBadRequest, "missing_agent_id", "agent_id is required", 0)
		return
	}

	if d.healthTracker != nil {
		d.healthTracker.Heartbeat(agentID)
	}

	// Record activity event (AG.7).
	if d.activityLog != nil {
		d.activityLog.Record(agent.ActivityEvent{
			AgentID:   agentID,
			EventType: "heartbeat",
			Resource:  "/api/agents/" + agentID + "/heartbeat",
			Result:    "ok",
		})
	}

	w.Header().Set("Content-Type", "application/json")
	resp := map[string]string{
		"status":   "ok",
		"agent_id": agentID,
	}
	json.NewEncoder(w).Encode(resp)
}
