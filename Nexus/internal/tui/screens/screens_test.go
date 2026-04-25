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

package screens

import (
	"strings"
	"testing"

	"github.com/bubblefish-tech/nexus/internal/tui/api"
)

func TestDashboardScreen_ImplementsScreen(t *testing.T) {
	t.Helper()
	var _ Screen = (*DashboardScreen)(nil)
}

func TestDashboardScreen_Name(t *testing.T) {
	t.Helper()
	d := NewDashboardScreen()
	if d.Name() != "Dashboard" {
		t.Fatalf("expected name Dashboard, got %s", d.Name())
	}
}

func TestDashboardScreen_SetSize(t *testing.T) {
	t.Helper()
	d := NewDashboardScreen()
	d.SetSize(140, 40)
	if d.width != 140 || d.height != 40 {
		t.Fatalf("expected 140x40, got %dx%d", d.width, d.height)
	}
}

func TestDashboardScreen_View_EmptyOnSmallSize(t *testing.T) {
	t.Helper()
	d := NewDashboardScreen()
	d.SetSize(20, 5)
	v := d.View()
	if v != "" {
		t.Fatalf("expected empty view for tiny terminal, got %q", v)
	}
}

func TestDashboardScreen_View_ShowsBrand(t *testing.T) {
	t.Helper()
	d := NewDashboardScreen()
	d.SetSize(140, 40)
	v := d.View()
	if !strings.Contains(v, "N E X U S") {
		t.Fatalf("expected N E X U S in view")
	}
	if !strings.Contains(v, "BubbleFish") {
		t.Fatalf("expected BubbleFish in view")
	}
}

func TestDashboardScreen_View_ShowsStatCards(t *testing.T) {
	t.Helper()
	d := NewDashboardScreen()
	d.SetSize(140, 40)
	v := d.View()
	if !strings.Contains(v, "M E M O R I E S") {
		t.Fatalf("expected MEMORIES stat card in view")
	}
	if !strings.Contains(v, "H E A L T H") {
		t.Fatalf("expected HEALTH stat card in view")
	}
}

func TestDashboardScreen_Update_StatusBroadcast(t *testing.T) {
	t.Helper()
	d := NewDashboardScreen()
	d.SetSize(140, 40)

	status := &api.StatusResponse{
		Status:        "ok",
		Version:       "0.1.3",
		MemoriesTotal: 42,
		Writes1m:      10,
		Reads1m:       5,
		AuditEnabled:  true,
	}

	updated, _ := d.Update(api.StatusBroadcastMsg{Data: status})
	ds := updated.(*DashboardScreen)
	if ds.status == nil {
		t.Fatal("expected status to be set")
	}
	if ds.status.MemoriesTotal != 42 {
		t.Fatalf("expected 42 memories, got %d", ds.status.MemoriesTotal)
	}
	if !ds.healthy {
		t.Fatal("expected healthy to be true after status broadcast")
	}
}

func TestDashboardScreen_Update_AgentsMsg(t *testing.T) {
	t.Helper()
	d := NewDashboardScreen()

	agents := []api.AgentSummary{
		{AgentID: "a1", DisplayName: "Claude Desktop", Status: "active"},
		{AgentID: "a2", DisplayName: "Ollama", Status: "idle"},
	}
	updated, _ := d.Update(dashAgentsMsg{data: agents})
	ds := updated.(*DashboardScreen)
	if len(ds.agents) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(ds.agents))
	}
}

func TestDashboardScreen_View_WithStatus(t *testing.T) {
	t.Helper()
	d := NewDashboardScreen()
	d.SetSize(140, 40)
	d.status = &api.StatusResponse{
		Status:        "ok",
		Version:       "0.1.3",
		MemoriesTotal: 1234,
		Writes1m:      15,
		Reads1m:       8,
		AuditEnabled:  true,
		Goroutines:    42,
		PID:           12345,
	}
	d.healthy = true

	v := d.View()
	if !strings.Contains(v, "1234") {
		t.Fatalf("expected memory count 1234 in view")
	}
	if !strings.Contains(v, "NOMINAL") {
		t.Fatalf("expected NOMINAL health in view")
	}
}

func TestDashboardScreen_ThroughputGauge(t *testing.T) {
	t.Helper()
	d := NewDashboardScreen()

	d.Update(api.StatusBroadcastMsg{Data: &api.StatusResponse{Writes1m: 10}})
	if d.curWrites != 10 {
		t.Fatalf("expected curWrites=10, got %d", d.curWrites)
	}
	if d.maxWrites != 10 {
		t.Fatalf("expected maxWrites=10, got %d", d.maxWrites)
	}

	d.Update(api.StatusBroadcastMsg{Data: &api.StatusResponse{Writes1m: 5}})
	if d.curWrites != 5 {
		t.Fatalf("expected curWrites=5 after decrease, got %d", d.curWrites)
	}
	if d.maxWrites != 10 {
		t.Fatalf("expected maxWrites=10 (peak retained), got %d", d.maxWrites)
	}
}
