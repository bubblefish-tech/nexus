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
	"github.com/bubblefish-tech/nexus/internal/mcp"
	"github.com/bubblefish-tech/nexus/internal/subscribe"
)

type subscribeStoreAdapter struct {
	store *subscribe.Store
}

func (a *subscribeStoreAdapter) Add(agentID, filter string) (mcp.SubscribeResult, error) {
	sub, err := a.store.Add(agentID, filter)
	if err != nil {
		return mcp.SubscribeResult{}, err
	}
	return mcp.SubscribeResult{
		ID:        sub.ID,
		AgentID:   sub.AgentID,
		Filter:    sub.Filter,
		CreatedAt: sub.CreatedAt,
	}, nil
}

func (a *subscribeStoreAdapter) Remove(id string) error {
	return a.store.Remove(id)
}

func (a *subscribeStoreAdapter) ListForAgent(agentID string) []mcp.SubscribeEntry {
	subs := a.store.ListForAgent(agentID)
	entries := make([]mcp.SubscribeEntry, len(subs))
	for i, s := range subs {
		entries[i] = mcp.SubscribeEntry{
			ID:         s.ID,
			AgentID:    s.AgentID,
			Filter:     s.Filter,
			CreatedAt:  s.CreatedAt,
			MatchCount: s.MatchCount,
			Active:     s.Active,
		}
	}
	return entries
}
