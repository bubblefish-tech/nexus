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

package mcp

import (
	"encoding/json"
	"net/http"
	"time"
)

// SubscribeStore is the interface for subscription CRUD operations.
type SubscribeStore interface {
	Add(agentID, filter string) (SubscribeResult, error)
	Remove(id string) error
	ListForAgent(agentID string) []SubscribeEntry
}

type SubscribeResult struct {
	ID        string    `json:"id"`
	AgentID   string    `json:"agent_id"`
	Filter    string    `json:"filter"`
	CreatedAt time.Time `json:"created_at"`
}

type SubscribeEntry struct {
	ID         string    `json:"id"`
	AgentID    string    `json:"agent_id"`
	Filter     string    `json:"filter"`
	CreatedAt  time.Time `json:"created_at"`
	MatchCount int       `json:"match_count"`
	Active     bool      `json:"active"`
}

func (s *Server) SetSubscribeStore(store SubscribeStore) {
	s.subscribeStore = store
}

func (s *Server) callNexusSubscribe(w http.ResponseWriter, r *http.Request, req rpcRequest, args json.RawMessage) {
	if s.subscribeStore == nil {
		s.writeRPCError(w, r, req.ID, rpcInternalError, "subscribe system not initialized")
		return
	}

	var a struct {
		Filter string `json:"filter"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &a); err != nil {
			s.writeRPCError(w, r, req.ID, rpcInvalidParams, "invalid arguments: "+err.Error())
			return
		}
	}
	if a.Filter == "" {
		s.writeRPCError(w, r, req.ID, rpcInvalidParams, "filter is required")
		return
	}

	agentID := s.sourceName
	if id := r.Header.Get("X-Agent-ID"); id != "" {
		agentID = id
	}

	sub, err := s.subscribeStore.Add(agentID, a.Filter)
	if err != nil {
		s.writeRPCError(w, r, req.ID, rpcInternalError, "failed to create subscription: "+err.Error())
		return
	}

	out, _ := json.Marshal(sub)
	s.writeRPCResult(w, r, req.ID, toolCallResult{
		Content: []contentBlock{{Type: "text", Text: string(out)}},
	})
}

func (s *Server) callNexusUnsubscribe(w http.ResponseWriter, r *http.Request, req rpcRequest, args json.RawMessage) {
	if s.subscribeStore == nil {
		s.writeRPCError(w, r, req.ID, rpcInternalError, "subscribe system not initialized")
		return
	}

	var a struct {
		SubscriptionID string `json:"subscription_id"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &a); err != nil {
			s.writeRPCError(w, r, req.ID, rpcInvalidParams, "invalid arguments: "+err.Error())
			return
		}
	}
	if a.SubscriptionID == "" {
		s.writeRPCError(w, r, req.ID, rpcInvalidParams, "subscription_id is required")
		return
	}

	if err := s.subscribeStore.Remove(a.SubscriptionID); err != nil {
		s.writeRPCError(w, r, req.ID, rpcInternalError, "failed to remove subscription: "+err.Error())
		return
	}

	s.writeRPCResult(w, r, req.ID, toolCallResult{
		Content: []contentBlock{{Type: "text", Text: `{"status":"removed","subscription_id":"` + a.SubscriptionID + `"}`}},
	})
}

func (s *Server) callNexusSubscriptions(w http.ResponseWriter, r *http.Request, req rpcRequest) {
	if s.subscribeStore == nil {
		s.writeRPCError(w, r, req.ID, rpcInternalError, "subscribe system not initialized")
		return
	}

	agentID := s.sourceName
	if id := r.Header.Get("X-Agent-ID"); id != "" {
		agentID = id
	}

	subs := s.subscribeStore.ListForAgent(agentID)
	out, _ := json.Marshal(map[string]any{
		"agent_id":      agentID,
		"subscriptions": subs,
		"count":         len(subs),
	})

	s.writeRPCResult(w, r, req.ID, toolCallResult{
		Content: []contentBlock{{Type: "text", Text: string(out)}},
	})
}
