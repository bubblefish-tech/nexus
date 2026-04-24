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
	"github.com/bubblefish-tech/nexus/internal/tui/api"
	"github.com/bubblefish-tech/nexus/internal/tui/components"
	"github.com/bubblefish-tech/nexus/internal/tui/styles"
	"github.com/charmbracelet/lipgloss"
)

// translateKindToEmpty maps an api.ErrorKind to the appropriate EmptyStateKind
// for display in a TUI panel.
func translateKindToEmpty(kind api.ErrorKind) components.EmptyStateKind {
	switch kind {
	case api.ErrKindConnection, api.ErrKindServer:
		return components.EmptyStateDisconnected
	case api.ErrKindForbidden, api.ErrKindNotFound, api.ErrKindClient:
		return components.EmptyStateFeatureGated
	default:
		return components.EmptyStateNoData
	}
}

// emptyStateOpts builds the EmptyStateOptions for a panel with the given
// error kind and hint, sized to fill width×height.
func emptyStateOpts(kind api.ErrorKind, hint string, width, height int) components.EmptyStateOptions {
	return components.EmptyStateOptions{
		Kind:   translateKindToEmpty(kind),
		Width:  width,
		Height: height,
		BorderStyle: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(styles.BorderBase),
		MutedColor: styles.ColorGray,
		WhiteDim:   styles.TextWhiteDim,
		Amber:      styles.ColorAmber,
		Teal:       styles.ColorTeal,
		Hint:       hint,
	}
}

// loadingOpts builds the EmptyStateOptions for the loading state.
func loadingOpts(width, height, frame int) components.EmptyStateOptions {
	return components.EmptyStateOptions{
		Kind:   components.EmptyStateLoading,
		Width:  width,
		Height: height,
		BorderStyle: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(styles.BorderBase),
		MutedColor: styles.ColorGray,
		WhiteDim:   styles.TextWhiteDim,
		Amber:      styles.ColorAmber,
		Teal:       styles.ColorTeal,
		Frame:      frame,
	}
}
