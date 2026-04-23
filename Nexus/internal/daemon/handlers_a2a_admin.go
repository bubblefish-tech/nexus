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
	"strings"
	"time"

	"github.com/bubblefish-tech/nexus/internal/a2a"
	"github.com/bubblefish-tech/nexus/internal/a2a/registry"
	"github.com/bubblefish-tech/nexus/internal/a2a/transport"
)

// registerAgentRequest is the body accepted by POST /a2a/admin/register-agent.
type registerAgentRequest struct {
	Name            string   `json:"name"`
	URL             string   `json:"url"`
	CardURL         string   `json:"card_url,omitempty"`
	Methods         []string `json:"methods,omitempty"`
	ProtocolVersion string   `json:"protocol_version,omitempty"`
	BearerTokenEnv  string   `json:"bearer_token_env,omitempty"`
}

// registerAgentResponse is returned on success.
type registerAgentResponse struct {
	AgentID    string `json:"agent_id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Upserted   bool   `json:"upserted"`
	RegisteredAt string `json:"registered_at"`
}

// handleA2AAdminRegisterAgent serves POST /a2a/admin/register-agent.
// Auth: admin bearer token (requireAdminToken middleware).
// Upserts an agent into the registry and evicts any stale pool connection
// when the URL changes.
func (d *Daemon) handleA2AAdminRegisterAgent(w http.ResponseWriter, r *http.Request) {
	if d.registryStore == nil {
		d.writeErrorResponse(w, r, http.StatusServiceUnavailable,
			"control_unavailable", "agent registry not available", 0)
		return
	}

	var req registerAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		d.writeErrorResponse(w, r, http.StatusBadRequest,
			"invalid_json", "request body is not valid JSON", 0)
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	req.URL = strings.TrimSpace(req.URL)

	if req.Name == "" {
		d.writeErrorResponse(w, r, http.StatusBadRequest,
			"missing_field", "name is required", 0)
		return
	}
	if req.URL == "" {
		d.writeErrorResponse(w, r, http.StatusBadRequest,
			"missing_field", "url is required", 0)
		return
	}

	tc := transport.TransportConfig{
		Kind:           "http",
		URL:            strings.TrimSuffix(req.URL, "/"),
		BearerTokenEnv: req.BearerTokenEnv,
	}
	if req.BearerTokenEnv != "" {
		tc.AuthType = "bearer"
	}

	proto := req.ProtocolVersion
	if proto == "" {
		proto = "0.1.0"
	}
	card := a2a.AgentCard{
		Name:            req.Name,
		ProtocolVersion: proto,
		Methods:         req.Methods,
	}

	ctx := r.Context()
	existing, _ := d.registryStore.GetByName(ctx, req.Name)

	if existing != nil {
		// Evict stale pool connection if URL changed.
		if d.a2aPool != nil && existing.TransportConfig.URL != tc.URL {
			d.a2aPool.Close(existing.AgentID)
		}
		if err := d.registryStore.UpdateTransportAndCard(ctx, existing.AgentID, card, req.Name, tc); err != nil {
			d.logger.Error("a2a admin: update agent", "name", req.Name, "error", err)
			d.writeErrorResponse(w, r, http.StatusInternalServerError, "store_error", err.Error(), 0)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(registerAgentResponse{
			AgentID:      existing.AgentID,
			Name:         req.Name,
			Status:       existing.Status,
			Upserted:     true,
			RegisteredAt: existing.CreatedAt.Format(time.RFC3339),
		})
		return
	}

	agentID := a2a.NewAgentID()
	now := time.Now()
	agent := registry.RegisteredAgent{
		AgentID:         agentID,
		Name:            req.Name,
		DisplayName:     req.Name,
		AgentCard:       card,
		TransportConfig: tc,
		Status:          registry.StatusActive,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := d.registryStore.Register(ctx, agent); err != nil {
		d.logger.Error("a2a admin: register agent", "name", req.Name, "error", err)
		d.writeErrorResponse(w, r, http.StatusInternalServerError, "store_error", err.Error(), 0)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(registerAgentResponse{
		AgentID:      agentID,
		Name:         req.Name,
		Status:       registry.StatusActive,
		Upserted:     false,
		RegisteredAt: now.Format(time.RFC3339),
	})
}
