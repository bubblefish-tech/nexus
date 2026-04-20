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
	"github.com/bubblefish-tech/nexus/internal/tui/api"
	"github.com/bubblefish-tech/nexus/internal/tui/pages"
	"github.com/bubblefish-tech/nexus/internal/tui/tabs"
	tea "github.com/charmbracelet/bubbletea"
)

type appMode int

const (
	modeSetup   appMode = iota
	modeRunning appMode = iota
)

// App is the top-level Bubble Tea model. It routes between the setup wizard
// (modeSetup) and the running dashboard (modeRunning).
type App struct {
	mode    appMode
	wizard  WizardModel
	running Model
}

// NewSetupApp creates an App in modeSetup with all nine wizard pages.
func NewSetupApp(configDir string) App {
	state := &pages.WizardState{ConfigDir: configDir}
	pgs := []pages.Page{
		pages.NewWelcomePage(),
		pages.NewScanPage(),
		pages.NewFeaturesPage(),
		pages.NewToolsPage(),
		pages.NewDatabasePage(),
		pages.NewSecurityPage(),
		pages.NewTunnelPage(),
		pages.NewDirectoryPage(),
		pages.NewSummaryPage(),
	}
	return App{
		mode:   modeSetup,
		wizard: NewWizardModel(state, pgs),
	}
}

// NewRunningApp creates an App in modeRunning backed by the existing Model.
func NewRunningApp(client *api.Client, tabList []tabs.Tab) App {
	return App{
		mode:    modeRunning,
		running: NewModel(client, tabList),
	}
}

// Init delegates to the active sub-model.
func (a App) Init() tea.Cmd {
	switch a.mode {
	case modeSetup:
		return a.wizard.Init()
	default:
		return a.running.Init()
	}
}

// Update routes messages to the active sub-model. WizardCompleteMsg triggers
// tea.Quit so the caller can proceed with installation output.
func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(pages.WizardCompleteMsg); ok {
		return a, tea.Quit
	}

	switch a.mode {
	case modeSetup:
		updated, cmd := a.wizard.Update(msg)
		a.wizard = updated
		return a, cmd
	default:
		updated, cmd := a.running.Update(msg)
		if m, ok := updated.(Model); ok {
			a.running = m
		}
		return a, cmd
	}
}

// View delegates to the active sub-model.
func (a App) View() string {
	switch a.mode {
	case modeSetup:
		return a.wizard.View()
	default:
		return a.running.View()
	}
}
