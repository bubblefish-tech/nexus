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

package pages

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/bubblefish-tech/nexus/internal/tui/styles"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// DirectoryPage lets the user choose the install directory.
type DirectoryPage struct {
	input        textinput.Model
	presets      []string
	presetCursor int
	useCustom    bool
}

var _ Page = (*DirectoryPage)(nil)

// NewDirectoryPage returns a DirectoryPage pre-filled with the default path.
func NewDirectoryPage() *DirectoryPage {
	ti := textinput.New()
	ti.CharLimit = 512
	ti.Placeholder = "~/BubbleFish/Nexus"
	return &DirectoryPage{input: ti}
}

func (p *DirectoryPage) Name() string { return "Install Directory" }

func (p *DirectoryPage) Init(state *WizardState) tea.Cmd {
	if state.InstallDir == "" {
		state.InstallDir = defaultInstallDir()
	}
	p.input.SetValue(state.InstallDir)
	p.presets = buildPresets()
	p.useCustom = false
	return nil
}

func (p *DirectoryPage) Update(msg tea.Msg, state *WizardState) (Page, tea.Cmd) {
	if p.useCustom {
		if k, ok := msg.(tea.KeyMsg); ok && (k.String() == "esc" || k.String() == "shift+tab") {
			p.useCustom = false
			p.input.Blur()
			return p, nil
		}
		var cmd tea.Cmd
		p.input, cmd = p.input.Update(msg)
		state.InstallDir = p.input.Value()
		return p, cmd
	}

	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "up", "k":
			if p.presetCursor > 0 {
				p.presetCursor--
			}
		case "down", "j":
			if p.presetCursor < len(p.presets) {
				p.presetCursor++
			}
		case " ", "enter":
			if p.presetCursor < len(p.presets) {
				state.InstallDir = p.presets[p.presetCursor]
				p.input.SetValue(state.InstallDir)
			} else {
				p.useCustom = true
				p.input.Focus()
				return p, textinput.Blink
			}
		case "tab":
			p.useCustom = true
			p.input.Focus()
			return p, textinput.Blink
		}
	}
	return p, nil
}

func (p *DirectoryPage) CanAdvance(state *WizardState) bool {
	return strings.TrimSpace(state.InstallDir) != ""
}

func (p *DirectoryPage) View(width, height int) string {
	var b strings.Builder
	title := lipgloss.NewStyle().Foreground(styles.ColorTeal).Bold(true).
		Render("Install Directory")
	b.WriteString(title + "\n\n")
	b.WriteString(lipgloss.NewStyle().Foreground(styles.TextMuted).
		Render("Nexus will store its config, WAL, and database at this location.") + "\n\n")

	b.WriteString(lipgloss.NewStyle().Foreground(styles.TextSecondary).
		Render("Quick Select:") + "\n")
	for i, preset := range p.presets {
		cursor := "  "
		rowStyle := lipgloss.NewStyle().Foreground(styles.TextSecondary)
		if !p.useCustom && i == p.presetCursor {
			cursor = lipgloss.NewStyle().Foreground(styles.ColorTeal).Render("▶ ")
			rowStyle = lipgloss.NewStyle().Foreground(styles.TextPrimary).Bold(true)
		}
		b.WriteString(fmt.Sprintf("%s[%d] %s\n", cursor, i+1, rowStyle.Render(preset)))
	}
	{
		cursor := "  "
		rowStyle := lipgloss.NewStyle().Foreground(styles.TextSecondary)
		if !p.useCustom && p.presetCursor == len(p.presets) {
			cursor = lipgloss.NewStyle().Foreground(styles.ColorTeal).Render("▶ ")
			rowStyle = lipgloss.NewStyle().Foreground(styles.TextPrimary).Bold(true)
		}
		b.WriteString(fmt.Sprintf("%s[%d] %s\n", cursor, len(p.presets)+1, rowStyle.Render("Custom path...")))
	}

	b.WriteString("\n")
	if p.useCustom {
		b.WriteString(lipgloss.NewStyle().Foreground(styles.TextSecondary).Render("Directory: ") +
			p.input.View() + "\n")
		b.WriteString(lipgloss.NewStyle().Foreground(styles.TextMuted).
			Render("  Esc to return to presets") + "\n")
	} else {
		b.WriteString(lipgloss.NewStyle().Foreground(styles.TextMuted).
			Render("  Tab for custom path  ·  Ctrl+N to continue") + "\n")
	}
	return lipgloss.NewStyle().Width(width).Render(b.String())
}

func defaultInstallDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", "BubbleFish", "Nexus")
	}
	return filepath.Join(home, "BubbleFish", "Nexus")
}

func buildPresets() []string {
	home, _ := os.UserHomeDir()
	if home == "" {
		home = "."
	}
	presets := []string{filepath.Join(home, "BubbleFish", "Nexus")}
	if runtime.GOOS == "windows" {
		if _, err := os.Stat("C:\\"); err == nil {
			presets = append(presets, "C:\\BubbleFish\\Nexus")
		}
		if _, err := os.Stat("D:\\"); err == nil {
			presets = append(presets, "D:\\BubbleFish\\Nexus")
		}
	} else {
		presets = append(presets, "/opt/bubblefish/nexus")
	}
	return presets
}
