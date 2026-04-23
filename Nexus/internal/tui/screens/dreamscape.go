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
	"fmt"
	"strings"

	"github.com/bubblefish-tech/nexus/internal/tui/api"
	"github.com/bubblefish-tech/nexus/internal/tui/styles"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type dreamStatusMsg struct {
	data *api.StatusResponse
	err  error
}

// DreamscapeScreen is Page 8 — sleep consolidation mode.
type DreamscapeScreen struct {
	width, height int
	status        *api.StatusResponse
	err           error
}

// NewDreamscapeScreen creates the dreamscape.
func NewDreamscapeScreen() *DreamscapeScreen {
	return &DreamscapeScreen{}
}

func (d *DreamscapeScreen) Name() string            { return "Dream" }
func (d *DreamscapeScreen) Init() tea.Cmd            { return nil }
func (d *DreamscapeScreen) SetSize(w, h int)         { d.width = w; d.height = h }
func (d *DreamscapeScreen) ShortHelp() []key.Binding { return nil }

func (d *DreamscapeScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	if m, ok := msg.(dreamStatusMsg); ok {
		d.err = m.err
		if m.err == nil && m.data != nil {
			d.status = m.data
		}
	}
	return d, nil
}

func (d *DreamscapeScreen) FireRefresh(client *api.Client) tea.Cmd {
	return func() tea.Msg {
		data, err := client.Status()
		return dreamStatusMsg{data: data, err: err}
	}
}

func (d *DreamscapeScreen) View() string {
	if d.width < 40 || d.height < 10 {
		return ""
	}

	memories := 0
	if d.status != nil {
		memories = d.status.MemoriesTotal
	}

	var content []string
	content = append(content,
		lipgloss.NewStyle().Foreground(styles.ColorTealDim).
			Render(fmt.Sprintf("⋯ consolidating %d memories ⋯", memories)))
	content = append(content, "")
	content = append(content, "")

	// Consolidation pipeline.
	pipeline := lipgloss.NewStyle().Foreground(styles.TextWhiteDim).
		Render("   fresh  ───→  compressed  ───→  archived  ───→  archetypal")
	content = append(content, pipeline)
	content = append(content, "")
	content = append(content, "")

	// Dream events.
	dreamEvents := []string{
		"· ∘ · memory consolidation in progress...",
		"· ∘ · temporal patterns being compressed",
		"· ∘ · semantic clusters forming",
	}
	for _, ev := range dreamEvents {
		content = append(content,
			lipgloss.NewStyle().Foreground(styles.ColorTealDim).Render("  "+ev))
	}

	content = append(content, "")
	content = append(content, "")
	content = append(content, "")

	dreaming := lipgloss.NewStyle().Foreground(styles.ColorPurpleViv).Bold(true).
		Render("[NEXUS IS DREAMING]")
	hint := lipgloss.NewStyle().Foreground(styles.TextMuted).
		Render("    press any key to wake")
	content = append(content, dreaming+hint)

	body := strings.Join(content, "\n")
	return lipgloss.Place(d.width, d.height, lipgloss.Center, lipgloss.Center, body)
}

var _ Screen = (*DreamscapeScreen)(nil)
