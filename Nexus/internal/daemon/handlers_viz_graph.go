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
	"net/http"
	"time"

	"github.com/bubblefish-tech/nexus/internal/a2a/registry"
	dashboard "github.com/bubblefish-tech/nexus/web/dashboard"
)

// ---------------------------------------------------------------------------
// SHOW.2 — GET /api/viz/memory-graph
// ---------------------------------------------------------------------------

type graphNode struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Type     string `json:"type"` // "nexus", "tool", "agent"
	Count    int    `json:"count"`
	Endpoint string `json:"endpoint,omitempty"`
}

type graphEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Weight int    `json:"weight"`
}

type memoryGraphResponse struct {
	Nodes       []graphNode `json:"nodes"`
	Edges       []graphEdge `json:"edges"`
	GeneratedAt time.Time   `json:"generated_at"`
}

// handleMemoryGraph serves GET /api/viz/memory-graph.
//
// Returns a hub-and-spoke graph: a central Nexus node connected to every
// discovered AI tool and every registered A2A agent. The payload is consumed
// by the D3.js force-directed graph on /dashboard/memgraph.
func (d *Daemon) handleMemoryGraph(w http.ResponseWriter, r *http.Request) {
	var nodes []graphNode
	var edges []graphEdge

	nodes = append(nodes, graphNode{
		ID:    "nexus",
		Label: "BubbleFish Nexus",
		Type:  "nexus",
		Count: 0,
	})

	seen := map[string]bool{"nexus": true}

	// Discovered AI tools — use cached discovery result (no new scan triggered).
	d.lastDiscoveryMu.RLock()
	tools := d.lastDiscovery
	d.lastDiscoveryMu.RUnlock()

	for _, t := range tools {
		id := "tool:" + t.Name
		if seen[id] {
			continue
		}
		seen[id] = true
		nodes = append(nodes, graphNode{
			ID:       id,
			Label:    t.Name,
			Type:     "tool",
			Count:    1,
			Endpoint: t.Endpoint,
		})
		edges = append(edges, graphEdge{Source: id, Target: "nexus", Weight: 1})
	}

	// A2A registered agents.
	if d.registryStore != nil {
		agents, err := d.registryStore.List(context.Background(), registry.ListFilter{})
		if err == nil {
			for _, a := range agents {
				id := "agent:" + a.AgentID
				if seen[id] {
					continue
				}
				seen[id] = true
				label := a.DisplayName
				if label == "" {
					label = a.Name
				}
				nodes = append(nodes, graphNode{
					ID:    id,
					Label: label,
					Type:  "agent",
					Count: 1,
				})
				edges = append(edges, graphEdge{Source: id, Target: "nexus", Weight: 1})
			}
		}
	}

	d.writeJSON(w, http.StatusOK, memoryGraphResponse{
		Nodes:       nodes,
		Edges:       edges,
		GeneratedAt: time.Now().UTC(),
	})
}

// ---------------------------------------------------------------------------
// SHOW.2 — GET /dashboard/memgraph
// ---------------------------------------------------------------------------

func (d *Daemon) handleDashboardMemgraph(w http.ResponseWriter, r *http.Request) {
	d.serveDashboardPage(w, r, dashboard.MemgraphHTML)
}
