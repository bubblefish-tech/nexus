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

	"github.com/go-chi/chi/v5"
)

// handleAgentSessions returns active sessions for a specific agent.
// Requires admin token authentication.
//
// GET /api/agents/{agent_id}/sessions
//
// Reference: AG.2.
func (d *Daemon) handleAgentSessions(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "agent_id")
	if agentID == "" {
		d.writeErrorResponse(w, r, http.StatusBadRequest, "missing_agent_id", "agent_id is required", 0)
		return
	}

	sessions := d.sessionMgr.Sessions(agentID)

	w.Header().Set("Content-Type", "application/json")
	resp := map[string]interface{}{
		"agent_id": agentID,
		"sessions": sessions,
		"count":    len(sessions),
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		d.logger.Error("daemon: encode agent sessions response",
			"component", "daemon",
			"error", err,
		)
	}
}
