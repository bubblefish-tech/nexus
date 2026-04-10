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
	"fmt"
	"strings"

	"github.com/bubblefish-tech/nexus/internal/tui/styles"
	"github.com/charmbracelet/lipgloss"
)

// StatusbarData holds the key-value pairs for the status bar.
type StatusbarData struct {
	Status  string
	Version string
	Queue   int
	Paused  bool
}

// Statusbar renders the bottom status strip.
type Statusbar struct {
	Data  StatusbarData
	Width int
}

// View renders the statusbar.
func (s Statusbar) View() string {
	pairs := []string{
		kv("status", s.Data.Status),
		kv("ver", s.Data.Version),
		kv("queue", fmt.Sprintf("%d", s.Data.Queue)),
	}

	if s.Data.Paused {
		pairs = append(pairs, lipgloss.NewStyle().Foreground(styles.ColorAmber).Bold(true).Render("[PAUSED]"))
	}

	left := strings.Join(pairs, "  ")
	hints := lipgloss.NewStyle().Foreground(styles.TextDim).Render("1-7:tabs  ?:help  q:quit  H:sidebar  ctrl+r:pause  r:refresh")

	gap := s.Width - lipgloss.Width(left) - lipgloss.Width(hints)
	if gap < 1 {
		gap = 1
	}

	return lipgloss.NewStyle().
		Background(styles.BgRow).
		Width(s.Width).
		Render(left + strings.Repeat(" ", gap) + hints)
}

func kv(k, v string) string {
	key := lipgloss.NewStyle().Foreground(styles.TextMuted).Render(k + ":")
	val := lipgloss.NewStyle().Foreground(styles.TextPrimary).Render(v)
	return key + val
}
