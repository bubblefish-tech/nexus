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
	"encoding/json"
	"io"
	"net/http"

	"github.com/bubblefish-tech/nexus/internal/a2a/jsonrpc"
	a2aserver "github.com/bubblefish-tech/nexus/internal/a2a/server"
)

const a2aJSONRPCMaxBodyBytes = 1 << 20 // 1 MiB

// handleA2AJSONRPC serves POST /a2a/jsonrpc — the NA2A JSON-RPC endpoint.
// Authentication is method-level (e.g. agent/register checks registration_token).
// No daemon-level bearer token is required so external agents can reach this
// endpoint without an admin or data key.
func (d *Daemon) handleA2AJSONRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeA2AJSONRPCError(w, http.StatusMethodNotAllowed, nil, jsonrpc.ErrorObject{
			Code:    -32600,
			Message: "use POST",
		})
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, a2aJSONRPCMaxBodyBytes+1))
	if err != nil {
		writeA2AJSONRPCError(w, http.StatusBadRequest, nil, jsonrpc.ErrorObject{
			Code:    -32700,
			Message: "parse error: failed to read body",
		})
		return
	}
	if int64(len(body)) > a2aJSONRPCMaxBodyBytes {
		writeA2AJSONRPCError(w, http.StatusRequestEntityTooLarge, nil, jsonrpc.ErrorObject{
			Code:    -32600,
			Message: "request body exceeds 1 MiB limit",
		})
		return
	}

	var req jsonrpc.Request
	if err := json.Unmarshal(body, &req); err != nil {
		writeA2AJSONRPCError(w, http.StatusOK, nil, jsonrpc.ErrorObject{
			Code:    -32700,
			Message: "parse error: " + err.Error(),
		})
		return
	}

	// Inject X-Agent-ID header into context so agent/invoke and other methods
	// that require a source identity can identify the calling agent.
	ctx := r.Context()
	if agentID := r.Header.Get("X-Agent-ID"); agentID != "" {
		ctx = context.WithValue(ctx, a2aserver.CtxKeySourceAgent, agentID)
	}

	resp := d.a2aServer.Dispatch(ctx, &req)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// writeA2AJSONRPCError writes a JSON-RPC 2.0 error response.
func writeA2AJSONRPCError(w http.ResponseWriter, httpStatus int, id interface{}, errObj jsonrpc.ErrorObject) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpStatus)
	resp := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"error":   errObj,
	}
	_ = json.NewEncoder(w).Encode(resp)
}
