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
	"testing"

	"github.com/bubblefish-tech/nexus/internal/tui/pages"
	tea "github.com/charmbracelet/bubbletea"
)

func TestApp_SetupMode_Init(t *testing.T) {
	t.Helper()
	app := NewSetupApp("/tmp/test-config")
	if app.mode != modeSetup {
		t.Fatalf("expected modeSetup, got %d", app.mode)
	}
	cmd := app.Init()
	_ = cmd // init returns a command; just confirm no panic
}

func TestApp_SetupMode_View(t *testing.T) {
	t.Helper()
	app := NewSetupApp("/tmp/test-config")
	// Inject a window size so View doesn't return "too small".
	updated, _ := app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	view := updated.(App).View()
	if view == "" {
		t.Fatal("expected non-empty view")
	}
}

func TestApp_WizardCompleteMsg_Quits(t *testing.T) {
	t.Helper()
	app := NewSetupApp("/tmp/test-config")
	_, cmd := app.Update(pages.WizardCompleteMsg{ConfigDir: "/tmp/test-config"})
	if cmd == nil {
		t.Fatal("expected tea.Quit command, got nil")
	}
	// Execute the command and check it's a quit message.
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg, got %T", msg)
	}
}

func TestApp_WindowSize_Propagates(t *testing.T) {
	t.Helper()
	app := NewSetupApp("/tmp/test-config")
	updated, _ := app.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	a := updated.(App)
	if a.wizard.width != 100 || a.wizard.height != 30 {
		t.Fatalf("expected wizard size 100x30, got %dx%d", a.wizard.width, a.wizard.height)
	}
}
