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
	"time"

	"github.com/bubblefish-tech/nexus/internal/tui/styles"
	"github.com/charmbracelet/lipgloss"
)

// TickerItem is a single event in the scrolling ticker.
type TickerItem struct {
	Time  time.Time
	Icon  rune
	Color lipgloss.Color
	Text  string
}

// EventTicker renders a Bloomberg-style scrolling event feed.
type EventTicker struct {
	items  []TickerItem
	offset int
	Width  int
}

// Push adds a new event to the ticker (max 30 items).
func (et *EventTicker) Push(item TickerItem) {
	et.items = append(et.items, item)
	if len(et.items) > 30 {
		et.items = et.items[len(et.items)-30:]
	}
}

// Tick advances the scroll offset.
func (et *EventTicker) Tick() {
	et.offset++
}

// View renders the ticker as a single scrolling line.
func (et *EventTicker) View() string {
	if len(et.items) == 0 {
		return lipgloss.NewStyle().Foreground(styles.TextMuted).
			Render(" ◈ LIVE  —")
	}

	var parts []string
	for _, item := range et.items {
		icon := lipgloss.NewStyle().Foreground(item.Color).Render(string(item.Icon))
		text := lipgloss.NewStyle().Foreground(styles.TextWhiteDim).Render(item.Text)
		parts = append(parts, icon+" "+text)
	}

	full := " ◈ LIVE  " + strings.Join(parts, "  ·  ")

	if et.Width > 0 && len(full) > et.Width {
		start := et.offset % len(full)
		doubled := full + "  ·  " + full
		if start+et.Width < len(doubled) {
			full = doubled[start : start+et.Width]
		}
	}

	return full
}
