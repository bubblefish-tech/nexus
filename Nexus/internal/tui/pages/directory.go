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
	"os"
	"path/filepath"
	"strings"

	"github.com/bubblefish-tech/nexus/internal/tui/styles"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// DirectoryPage lets the user choose the install directory.
type DirectoryPage struct {
	input textinput.Model
}

var _ Page = (*DirectoryPage)(nil)

// NewDirectoryPage returns a DirectoryPage pre-filled with the default path.
func NewDirectoryPage() *DirectoryPage {
	ti := textinput.New()
	ti.CharLimit = 512
	ti.Placeholder = "~/.bubblefish/Nexus"
	return &DirectoryPage{input: ti}
}

func (p *DirectoryPage) Name() string { return "Install Directory" }

func (p *DirectoryPage) Init(state *WizardState) tea.Cmd {
	if state.InstallDir == "" {
		state.InstallDir = defaultInstallDir()
	}
	p.input.SetValue(state.InstallDir)
	p.input.Focus()
	return textinput.Blink
}

func (p *DirectoryPage) Update(msg tea.Msg, state *WizardState) (Page, tea.Cmd) {
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	state.InstallDir = p.input.Value()
	return p, cmd
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
	b.WriteString(lipgloss.NewStyle().Foreground(styles.TextSecondary).Render("Directory: ") +
		p.input.View() + "\n\n")
	b.WriteString(lipgloss.NewStyle().Foreground(styles.TextMuted).
		Render("Press Ctrl+N to confirm and continue."))
	return lipgloss.NewStyle().Width(width).Render(b.String())
}

// defaultInstallDir returns the standard Nexus config directory.
func defaultInstallDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".bubblefish", "Nexus")
	}
	return filepath.Join(home, ".bubblefish", "Nexus")
}
