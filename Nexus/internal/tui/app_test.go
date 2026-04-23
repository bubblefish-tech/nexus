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

package tui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bubblefish-tech/nexus/internal/tui/api"
	"github.com/bubblefish-tech/nexus/internal/tui/pages"
	"github.com/bubblefish-tech/nexus/internal/tui/tabs"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
)

func newMockDaemon(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/ready", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
	})
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ok", "version": "0.1.3",
			"queue_depth": 0, "consistency_score": 1.0,
			"memories_total": 42,
		})
	})
	mux.HandleFunc("/api/audit/log", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"records": []interface{}{}, "total_matching": 0,
		})
	})
	mux.HandleFunc("/api/config", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{})
	})
	mux.HandleFunc("/api/lint", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"findings": []interface{}{}})
	})
	mux.HandleFunc("/api/security/summary", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{})
	})
	mux.HandleFunc("/api/security/events", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"events": []interface{}{}})
	})
	mux.HandleFunc("/api/conflicts", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"groups": []interface{}{}})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{})
	})
	return httptest.NewServer(mux)
}

func newTestRunningApp(t *testing.T) (App, func()) {
	t.Helper()
	srv := newMockDaemon(t)
	client := api.NewClient(srv.URL, "test-token")
	tabList := []tabs.Tab{
		tabs.NewControlTab(),
		tabs.NewAuditTab(),
		tabs.NewSecurityTab(),
		tabs.NewPipelineTab(),
		tabs.NewConflictsTab(),
		tabs.NewTimeTravelTab(),
		tabs.NewSettingsTab(),
	}
	app := NewRunningApp(client, tabList, nil)
	return app, func() {
		client.Close()
		srv.Close()
	}
}

// ── Setup Mode ─ teatest ──────────────────────────────────────

func TestApp_SetupMode_RendersWelcome(t *testing.T) {
	app := NewSetupApp(t.TempDir())
	tm := teatest.NewTestModel(t, app, teatest.WithInitialTermSize(120, 40))

	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte("BubbleFish"))
	}, teatest.WithDuration(3*time.Second))

	tm.Quit()
	tm.WaitFinished(t, teatest.WithFinalTimeout(time.Second))
}

func TestApp_SetupMode_ShowsStepProgress(t *testing.T) {
	app := NewSetupApp(t.TempDir())
	tm := teatest.NewTestModel(t, app, teatest.WithInitialTermSize(120, 40))

	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte("Step 1"))
	}, teatest.WithDuration(3*time.Second))

	tm.Quit()
	tm.WaitFinished(t, teatest.WithFinalTimeout(time.Second))
}

// ── Setup Mode ─ manual ───────────────────────────────────────

func TestApp_SetupMode_WizardCompleteMsg_Quits(t *testing.T) {
	t.Helper()
	app := NewSetupApp(t.TempDir())
	_, cmd := app.Update(pages.WizardCompleteMsg{ConfigDir: t.TempDir()})
	if cmd == nil {
		t.Fatal("expected tea.Quit command, got nil")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg, got %T", msg)
	}
}

func TestApp_SetupMode_WindowSize_ViewUpdates(t *testing.T) {
	t.Helper()
	app := NewSetupApp(t.TempDir())

	updated, _ := app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	view := updated.(App).View()
	if !strings.Contains(view, "BubbleFish") {
		t.Fatalf("expected BubbleFish in view after resize")
	}
	if !strings.Contains(view, "Step 1") {
		t.Fatalf("expected Step 1 in view")
	}
}

func TestApp_RunningMode_QuitOnCtrlC(t *testing.T) {
	t.Helper()
	app, cleanup := newTestRunningApp(t)
	defer cleanup()

	updated, _ := app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	app = updated.(App)
	app.running.daemonUp = true

	updated, cmd := app.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("expected tea.Quit on ctrl+c")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("expected QuitMsg, got %T", msg)
	}
	_ = updated
}

// ── Running Mode ─ manual ─────────────────────────────────────

func TestApp_RunningMode_TabSwitch_UpdatesView(t *testing.T) {
	t.Helper()
	app, cleanup := newTestRunningApp(t)
	defer cleanup()

	updated, _ := app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	app = updated.(App)
	app.running.daemonUp = true

	view := app.View()
	if !strings.Contains(view, "Overview") {
		t.Fatalf("expected Overview in initial view")
	}

	updated, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	app = updated.(App)
	if app.running.activeTab != 1 {
		t.Fatalf("expected activeTab 1, got %d", app.running.activeTab)
	}
}

func TestApp_RunningMode_SidebarToggle_ChangesView(t *testing.T) {
	t.Helper()
	app, cleanup := newTestRunningApp(t)
	defer cleanup()

	updated, _ := app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	app = updated.(App)
	app.running.daemonUp = true

	viewWithSidebar := app.View()

	updated, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'H'}})
	app = updated.(App)
	viewWithoutSidebar := app.View()

	if viewWithSidebar == viewWithoutSidebar {
		t.Fatal("expected view to change after sidebar toggle")
	}
}

func TestApp_RunningMode_DaemonDown_ShowsMessage(t *testing.T) {
	t.Helper()
	app, cleanup := newTestRunningApp(t)
	defer cleanup()

	updated, _ := app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	app = updated.(App)
	app.running.daemonUp = false

	view := app.View()
	if !strings.Contains(view, "DAEMON NOT RUNNING") {
		t.Fatalf("expected 'DAEMON NOT RUNNING' in view")
	}
	if !strings.Contains(view, "nexus start") {
		t.Fatalf("expected 'nexus start' hint in view")
	}
}

func TestApp_RunningMode_Help_ShowsKeybindings(t *testing.T) {
	t.Helper()
	app, cleanup := newTestRunningApp(t)
	defer cleanup()

	updated, _ := app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	app = updated.(App)
	app.running.daemonUp = true

	updated, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	app = updated.(App)

	view := app.View()
	if !strings.Contains(view, "KEYBINDINGS") {
		t.Fatalf("expected KEYBINDINGS in help view")
	}
	if !strings.Contains(view, "ctrl+c") {
		t.Fatalf("expected ctrl+c in help view")
	}
}

func TestApp_RunningMode_TerminalTooSmall(t *testing.T) {
	t.Helper()
	app, cleanup := newTestRunningApp(t)
	defer cleanup()

	updated, _ := app.Update(tea.WindowSizeMsg{Width: 60, Height: 15})
	app = updated.(App)
	app.running.daemonUp = true

	view := app.View()
	if !strings.Contains(view, "too small") {
		t.Fatalf("expected 'too small' message for undersized terminal")
	}
}
