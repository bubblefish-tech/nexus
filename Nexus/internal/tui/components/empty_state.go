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

// EmptyStateFeatureGated renders a centered empty-state panel for screens
// whose backing API endpoint is unavailable or access-controlled.
// title is the short reason (e.g. "Governance not enabled").
// hint is the actionable suggestion from api.HintForEndpoint.
// width and height are the available panel dimensions.
func EmptyStateFeatureGated(title, hint string, width, height int) string {
	titleStyle := lipgloss.NewStyle().
		Foreground(styles.TextSecondary).
		Bold(true)
	hintStyle := lipgloss.NewStyle().
		Foreground(styles.TextMuted)

	content := lipgloss.JoinVertical(lipgloss.Center,
		titleStyle.Render(title),
		hintStyle.Render(hint),
	)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, content)
}
