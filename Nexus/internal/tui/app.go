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
	running    *RootModel
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
		mode:   modeSetup,
		wizard: NewWizardModel(state, pgs),
	}
}

// NewRunningApp creates an App in modeRunning backed by the new RootModel.
func NewRunningApp(client *api.Client, prefs *TUIPrefs) App {
	return App{
		mode:    modeRunning,
		running: NewRootModel(client, prefs),
		client:  client,
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
	if _, ok := msg.(DotTickMsg); ok && a.cmdResultT > 0 {
		a.cmdResultT--
	}

	// In running mode: handle slash command results.
	if a.mode == modeRunning {
		switch m := msg.(type) {
		case cmdResultMsg:
			a.cmdResult = string(m)
			a.cmdResultT = 10
			return a, nil
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
		case components.SlashCommandSelectedMsg:
			return a, a.dispatchCommand(m.Name)
		}
	}

	switch a.mode {
	case modeSetup:
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
		if rm, ok := updated.(*RootModel); ok {
			a.running = rm
		}
		return a, cmd
	}
}

// dispatchCommand finds and executes the named slash command.
func (a App) dispatchCommand(name string) tea.Cmd {
	if strings.HasPrefix(name, "theme ") {
		themeName := strings.TrimPrefix(name, "theme ")
		if t, ok := ThemeByName(themeName); ok {
			ActiveTheme = t
		}
		return func() tea.Msg { return cmdResultMsg("Theme: " + themeName) }
	}
	if a.client == nil {
		return nil
	}
	if cmd, ok := commandRegistry[name]; ok {
		return cmd.Execute(a.client)
	}
	return nil
}

// View delegates to the active sub-model, overlaying command results.
func (a App) View() string {
	switch a.mode {
	case modeSetup:
		return a.wizard.View()
	default:
		base := a.running.View()
		if a.cmdResult != "" && a.cmdResultT > 0 {
			base += "\n" + lipgloss.NewStyle().
				Foreground(lipgloss.Color("10")).
				Render("  "+a.cmdResult)
		}

		// Slash command overlay (if the running model's slash cmd is active).
		if a.running.slashCmd.Active() {
			overlay := a.running.slashCmd.View()
			if overlay != "" {
				lines := strings.Split(base, "\n")
				drop := strings.Split(overlay, "\n")
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
		return base
	}
}
