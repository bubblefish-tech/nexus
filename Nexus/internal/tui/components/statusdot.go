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
	"github.com/bubblefish-tech/nexus/internal/tui/styles"
	"github.com/charmbracelet/lipgloss"
)

// DotStatus is the logical status of a service indicator.
type DotStatus int

const (
	DotOnline   DotStatus = iota // green pulse
	DotDegraded DotStatus = iota // amber slow pulse
	DotOffline  DotStatus = iota // red static (no pulse)
)

// StatusDot renders a colored "●" with optional pulse animation.
// Pulse is achieved by alternating bright/dim based on Frame % 2.
// Frame should be incremented by the caller on each tick.
type StatusDot struct {
	Status DotStatus
	Frame  int
}

// View renders the dot as a colored "● " string (dot + space).
func (d StatusDot) View() string {
	const glyph = "● "
	switch d.Status {
	case DotOnline:
		// Alternate bright/dim for pulse effect.
		if d.Frame%2 == 0 {
			return lipgloss.NewStyle().Foreground(styles.ColorGreen).Bold(true).Render(glyph)
		}
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#1a6b3a")).Render(glyph)

	case DotDegraded:
		// Slower pulse — dim every other frame.
		if d.Frame%4 < 2 {
			return lipgloss.NewStyle().Foreground(styles.ColorAmber).Bold(true).Render(glyph)
		}
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#7a5500")).Render(glyph)

	default: // DotOffline
		return lipgloss.NewStyle().Foreground(styles.ColorRed).Render(glyph)
	}
}

func dotStatusFromString(s string) DotStatus {
	switch s {
	case "green":
		return DotOnline
	case "amber":
		return DotDegraded
	default:
		return DotOffline
	}
}

