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

package components

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

var testCmds = []SlashCommand{
	{Name: "doctor", Description: "Run health check"},
	{Name: "logs", Description: "View audit logs"},
	{Name: "update", Description: "Check for updates"},
}

func TestSlashCmd_InactiveByDefault(t *testing.T) {
	t.Helper()
	s := NewSlashCommandModel(testCmds)
	if s.Active() {
		t.Fatal("should not be active by default")
	}
}

func TestSlashCmd_ActivateBecomesActive(t *testing.T) {
	t.Helper()
	s := NewSlashCommandModel(testCmds)
	s.Activate(80)
	if !s.Active() {
		t.Fatal("should be active after Activate()")
	}
	view := s.View()
	if view == "" {
		t.Fatal("active slash cmd should render non-empty view")
	}
}

func TestSlashCmd_EscCancels(t *testing.T) {
	t.Helper()
	s := NewSlashCommandModel(testCmds)
	s.Activate(80)
	updated, _ := s.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if updated.Active() {
		t.Fatal("should be inactive after Esc")
	}
	view := updated.View()
	if view != "" {
		t.Fatal("inactive slash cmd should render empty string")
	}
}

func TestSlashCmd_FilterByPrefix(t *testing.T) {
	t.Helper()
	s := NewSlashCommandModel(testCmds)
	s.Activate(80)
	s.input = "lo"
	s.filter()
	if len(s.filtered) != 1 || s.filtered[0].Name != "logs" {
		t.Fatalf("expected [logs], got %v", s.filtered)
	}
}

func TestSlashCmd_EmptyInputShowsAll(t *testing.T) {
	t.Helper()
	s := NewSlashCommandModel(testCmds)
	s.Activate(80)
	if len(s.filtered) != len(testCmds) {
		t.Fatalf("expected %d filtered, got %d", len(testCmds), len(s.filtered))
	}
	view := s.View()
	for _, cmd := range testCmds {
		if !strings.Contains(view, cmd.Name) {
			t.Errorf("expected %q in view", cmd.Name)
		}
	}
}

func TestSlashCmd_SelectReturnsMsg(t *testing.T) {
	t.Helper()
	s := NewSlashCommandModel(testCmds)
	s.Activate(80)
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected a tea.Cmd on Enter")
	}
	msg := cmd()
	sel, ok := msg.(SlashCommandSelectedMsg)
	if !ok {
		t.Fatalf("expected SlashCommandSelectedMsg, got %T", msg)
	}
	if sel.Name != "doctor" {
		t.Fatalf("expected 'doctor', got %q", sel.Name)
	}
}

func TestSlashCmd_NavigateDown(t *testing.T) {
	t.Helper()
	s := NewSlashCommandModel(testCmds)
	s.Activate(80)
	viewBefore := s.View()

	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if s.cursor != 1 {
		t.Fatalf("expected cursor 1, got %d", s.cursor)
	}
	viewAfter := s.View()
	if viewBefore == viewAfter {
		t.Fatal("expected view to change after cursor movement")
	}
}

func TestSlashCmd_ViewInactive(t *testing.T) {
	t.Helper()
	s := NewSlashCommandModel(testCmds)
	if s.View() != "" {
		t.Fatal("inactive slash cmd should render empty string")
	}
}

func TestSlashCmd_ViewActive_ContainsDescriptions(t *testing.T) {
	t.Helper()
	s := NewSlashCommandModel(testCmds)
	s.Activate(80)
	view := s.View()
	if !strings.Contains(view, "Run health check") {
		t.Fatalf("expected command description in view")
	}
}

func TestSlashCmd_BackspaceDeletesChar(t *testing.T) {
	t.Helper()
	s := NewSlashCommandModel(testCmds)
	s.Activate(80)
	s.input = "lo"
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if s.input != "l" {
		t.Fatalf("expected 'l' after backspace, got %q", s.input)
	}
}

func TestSlashCmd_FilterNoMatch(t *testing.T) {
	t.Helper()
	s := NewSlashCommandModel(testCmds)
	s.Activate(80)
	s.input = "zzz"
	s.filter()
	if len(s.filtered) != 0 {
		t.Fatalf("expected no matches for 'zzz', got %d", len(s.filtered))
	}
}

func TestSlashCmd_SelectAfterNavigate(t *testing.T) {
	t.Helper()
	s := NewSlashCommandModel(testCmds)
	s.Activate(80)

	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected Cmd after Enter")
	}
	msg := cmd()
	sel := msg.(SlashCommandSelectedMsg)
	if sel.Name != "logs" {
		t.Fatalf("expected 'logs' after navigating down, got %q", sel.Name)
	}
}
