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
	"fmt"
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
	mode       appMode
	wizard     WizardModel
	running    Model
	slashCmd   components.SlashCommandModel
	client     *api.Client
	width      int
	cmdResult  string // last slash-command result text
	cmdResultT int    // frames remaining to show result
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
func NewRunningApp(client *api.Client, tabList []tabs.Tab, prefs *TUIPrefs) App {
	// Wire client into tabs that need it for manual queries.
	for _, t := range tabList {
		if tt, ok := t.(*tabs.TimeTravelTab); ok {
			tt.SetClient(client)
		}
	}
	return App{
		mode:     modeRunning,
		running:  NewModel(client, tabList, prefs),
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

	// Decrement command result display counter on ticks.
	if _, ok := msg.(dotTickMsg); ok && a.cmdResultT > 0 {
		a.cmdResultT--
	}

	// In running mode: route to slash command overlay when active.
	if a.mode == modeRunning {
		// Handle slash activation.
		if k, ok := msg.(tea.KeyMsg); ok && !a.slashCmd.Active() && k.String() == "/" {
			a.slashCmd.Activate(a.width)
			return a, nil
		}

		// Slash command overlay consumes keys only.
		if a.slashCmd.Active() {
			if sel, ok := msg.(components.SlashCommandSelectedMsg); ok {
				a.slashCmd, _ = a.slashCmd.Update(msg)
				return a, a.dispatchCommand(sel.Name)
			}
			if _, ok := msg.(tea.KeyMsg); ok {
				updated, cmd := a.slashCmd.Update(msg)
				a.slashCmd = updated
				return a, cmd
			}
			// Non-key messages (ticks, health checks) fall through to running model.
		}

		// Handle slash command result messages.
		switch m := msg.(type) {
		case commands.DoctorResultMsg:
			if m.Err != nil {
				a.cmdResult = "doctor: " + m.Err.Error()
			} else if m.Healthy {
				a.cmdResult = "doctor: all checks HEALTHY"
			} else {
				a.cmdResult = "doctor: UNHEALTHY — run `nexus doctor` for details"
			}
			a.cmdResultT = 10
			return a, nil
		case commands.ConnectResultMsg:
			if m.Err != nil {
				a.cmdResult = "connect: " + m.Err.Error()
			} else {
				a.cmdResult = fmt.Sprintf("connect: %d agent(s) active", len(m.Agents))
			}
			a.cmdResultT = 10
			return a, nil
		case commands.LogsResultMsg:
			if m.Err != nil {
				a.cmdResult = "logs: " + m.Err.Error()
			} else {
				a.cmdResult = fmt.Sprintf("logs: %d recent records", len(m.Records))
			}
			a.cmdResultT = 10
			return a, nil
		case commands.TestResultMsg:
			if m.Err != nil {
				a.cmdResult = "test: " + m.Err.Error()
			} else {
				a.cmdResult = fmt.Sprintf("test [%s]: %d passed, %d failed", m.Category, m.Passed, m.Failed)
			}
			a.cmdResultT = 10
			return a, nil
		case commands.FeatureResultMsg:
			if m.Err != nil {
				a.cmdResult = "feature: " + m.Err.Error()
			} else {
				a.cmdResult = "feature: configuration updated"
			}
			a.cmdResultT = 10
			return a, nil
		case commands.UpdateResultMsg:
			if m.Message != "" {
				a.cmdResult = "update: " + m.Message
			} else {
				a.cmdResult = fmt.Sprintf("update: current version %s", m.CurrentVersion)
			}
			a.cmdResultT = 10
			return a, nil
		}
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
		if a.cmdResult != "" && a.cmdResultT > 0 && !a.slashCmd.Active() {
			base += "\n" + lipgloss.NewStyle().
				Foreground(lipgloss.Color("10")).
				Render("  "+a.cmdResult)
		}
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
