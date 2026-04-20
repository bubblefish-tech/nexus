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
	"strings"

	"github.com/bubblefish-tech/nexus/internal/tui/api"
	"github.com/bubblefish-tech/nexus/internal/tui/commands"
	"github.com/bubblefish-tech/nexus/internal/tui/components"
	"github.com/bubblefish-tech/nexus/internal/tui/pages"
	"github.com/bubblefish-tech/nexus/internal/tui/tabs"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type appMode int

const (
	modeSetup   appMode = iota
	modeRunning appMode = iota
)

// allSlashCommands returns the full slash-command list wired to real command types.
func allSlashCommands() []components.SlashCommand {
	cmds := []commands.Command{
		commands.DoctorCommand{},
		commands.EnableCommand{},
		commands.DisableFeatureCommand{},
		commands.LogsCommand{},
		commands.ConnectCommand{},
		commands.DisconnectCommand{},
		commands.TestCommand{},
		commands.UpdateCommand{},
	}
	out := make([]components.SlashCommand, len(cmds))
	for i, c := range cmds {
		out[i] = components.SlashCommand{Name: c.Name(), Description: c.Description()}
	}
	return out
}

// commandRegistry maps slash-command names to Command implementations.
var commandRegistry = map[string]commands.Command{
	"doctor":     commands.DoctorCommand{},
	"enable":     commands.EnableCommand{},
	"disable":    commands.DisableFeatureCommand{},
	"logs":       commands.LogsCommand{},
	"connect":    commands.ConnectCommand{},
	"disconnect": commands.DisconnectCommand{},
	"test":       commands.TestCommand{},
	"update":     commands.UpdateCommand{},
}

// App is the top-level Bubble Tea model. It routes between the setup wizard
// (modeSetup) and the running dashboard (modeRunning).
type App struct {
	mode     appMode
	wizard   WizardModel
	running  Model
	slashCmd components.SlashCommandModel
	client   *api.Client
	width    int
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
		mode:     modeSetup,
		wizard:   NewWizardModel(state, pgs),
		slashCmd: components.NewSlashCommandModel(allSlashCommands()),
	}
}

// NewRunningApp creates an App in modeRunning backed by the existing Model.
func NewRunningApp(client *api.Client, tabList []tabs.Tab) App {
	return App{
		mode:     modeRunning,
		running:  NewModel(client, tabList),
		slashCmd: components.NewSlashCommandModel(allSlashCommands()),
		client:   client,
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

	// In running mode: route to slash command overlay when active.
	if a.mode == modeRunning {
		// Handle slash activation.
		if k, ok := msg.(tea.KeyMsg); ok && !a.slashCmd.Active() && k.String() == "/" {
			a.slashCmd.Activate(a.width)
			return a, nil
		}

		// Slash command overlay consumes all keys.
		if a.slashCmd.Active() {
			if sel, ok := msg.(components.SlashCommandSelectedMsg); ok {
				a.slashCmd, _ = a.slashCmd.Update(msg)
				return a, a.dispatchCommand(sel.Name)
			}
			updated, cmd := a.slashCmd.Update(msg)
			a.slashCmd = updated
			return a, cmd
		}

		// Handle slash command result messages to surface them — for now just
		// forward to the running model which will ignore unknown messages.
	}

	switch a.mode {
	case modeSetup:
		// WindowSizeMsg: track width for slash cmd overlay.
		if ws, ok := msg.(tea.WindowSizeMsg); ok {
			a.width = ws.Width
		}
		updated, cmd := a.wizard.Update(msg)
		a.wizard = updated
		return a, cmd
	default:
		if ws, ok := msg.(tea.WindowSizeMsg); ok {
			a.width = ws.Width
		}
		updated, cmd := a.running.Update(msg)
		if m, ok := updated.(Model); ok {
			a.running = m
		}
		return a, cmd
	}
}

// dispatchCommand finds and executes the named slash command.
func (a App) dispatchCommand(name string) tea.Cmd {
	if a.client == nil {
		return nil
	}
	if cmd, ok := commandRegistry[name]; ok {
		return cmd.Execute(a.client)
	}
	return nil
}

// View delegates to the active sub-model, overlaying the slash command dropdown
// at the bottom when active in running mode.
func (a App) View() string {
	switch a.mode {
	case modeSetup:
		return a.wizard.View()
	default:
		base := a.running.View()
		if !a.slashCmd.Active() {
			return base
		}
		// Overlay the dropdown at the bottom of the base view.
		overlay := a.slashCmd.View()
		if overlay == "" {
			return base
		}
		lines := strings.Split(base, "\n")
		drop := strings.Split(overlay, "\n")
		// Replace last len(drop) lines with the overlay.
		start := len(lines) - len(drop)
		if start < 0 {
			start = 0
		}
		result := make([]string, len(lines))
		copy(result, lines)
		for i, l := range drop {
			if start+i < len(result) {
				result[start+i] = lipgloss.NewStyle().Render(l)
			}
		}
		return strings.Join(result, "\n")
	}
}
