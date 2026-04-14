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

package server

import (
	"context"
	"encoding/json"

	"github.com/BubbleFish-Nexus/internal/a2a"
	"github.com/BubbleFish-Nexus/internal/a2a/jsonrpc"
)

// handleAgentCard returns the server's AgentCard.
func (s *Server) handleAgentCard(_ context.Context, _ string, _ json.RawMessage) (interface{}, *jsonrpc.ErrorObject) {
	return &s.agentCard, nil
}

// pingResponse is the response for agent/ping.
type pingResponse struct {
	OK        bool   `json:"ok"`
	Timestamp string `json:"timestamp"`
	Version   string `json:"version"`
}

// handleAgentPing returns a health-check response.
func (s *Server) handleAgentPing(_ context.Context, _ string, _ json.RawMessage) (interface{}, *jsonrpc.ErrorObject) {
	return &pingResponse{
		OK:        true,
		Timestamp: a2a.Now(),
		Version:   a2a.ImplementationVersion(),
	}, nil
}

// handleAgentInvoke is a placeholder. Full implementation in A2A.11.
func (s *Server) handleAgentInvoke(_ context.Context, _ string, _ json.RawMessage) (interface{}, *jsonrpc.ErrorObject) {
	return nil, &jsonrpc.ErrorObject{
		Code:    a2a.CodeMethodNotFound,
		Message: "agent/invoke is not implemented yet",
	}
}
