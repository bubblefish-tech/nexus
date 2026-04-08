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
	"github.com/BubbleFish-Nexus/internal/tui/styles"
	"github.com/charmbracelet/lipgloss"
)

// ProvBadge renders a colored pill for actor type.
func ProvBadge(actorType string) string {
	label := actorType
	if label == "" {
		label = "sys"
	}
	if len(label) > 5 {
		label = label[:5]
	}

	var fg, bg lipgloss.Color
	switch actorType {
	case "user":
		fg = styles.TextContrast
		bg = styles.ColorPurple
	case "agent":
		fg = styles.TextContrast
		bg = styles.ColorTeal
	default:
		fg = styles.TextContrast
		bg = styles.ColorGray
	}

	return lipgloss.NewStyle().
		Foreground(fg).
		Background(bg).
		Padding(0, 1).
		Render(label)
}
