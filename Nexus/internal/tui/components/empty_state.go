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
	"time"

	"github.com/charmbracelet/lipgloss"
)

// EmptyStateKind enumerates the four accepted user-facing empty states.
type EmptyStateKind int

const (
	EmptyStateLoading      EmptyStateKind = iota
	EmptyStateNoData
	EmptyStateDisconnected
	EmptyStateFeatureGated
)

// EmptyStateOptions configures the render.
type EmptyStateOptions struct {
	Kind        EmptyStateKind
	Width       int
	Height      int
	BorderStyle lipgloss.Style // style of enclosing box
	MutedColor  lipgloss.Color // Theme.Muted
	WhiteDim    lipgloss.Color // Theme.WhiteDim
	Amber       lipgloss.Color // Theme.Amber
	Teal        lipgloss.Color // Theme.Teal
	Hint        string         // optional custom hint (NoData/Gated)
	RetryInSecs int            // Disconnected only
	Frame       int            // animation frame counter (Loading)
}

// Render produces the empty-state panel.
func Render(o EmptyStateOptions) string {
	innerW := o.Width - 2
	innerH := o.Height - 2
	if innerW < 20 || innerH < 4 {
		return o.BorderStyle.Width(o.Width).Height(o.Height).Render("…")
	}

	var lines []string
	switch o.Kind {
	case EmptyStateLoading:
		lines = renderLoading(o, innerW)
	case EmptyStateNoData:
		lines = renderNoData(o, innerW)
	case EmptyStateDisconnected:
		lines = renderDisconnected(o, innerW)
	case EmptyStateFeatureGated:
		lines = renderGated(o, innerW)
	}

	content := strings.Join(lines, "\n")
	placed := lipgloss.Place(innerW, innerH, lipgloss.Center, lipgloss.Center, content)
	return o.BorderStyle.Width(o.Width).Height(o.Height).Render(placed)
}

func renderLoading(o EmptyStateOptions, _ int) []string {
	frames := []string{"⠁", "⠉", "⠋", "⠛", "⠟", "⠿", "⡿", "⣿"}
	dot := frames[o.Frame%len(frames)]
	label := lipgloss.NewStyle().Foreground(o.Teal).Render(fmt.Sprintf("%s Loading… %s", dot, dot))
	return []string{label}
}

func renderNoData(o EmptyStateOptions, _ int) []string {
	icon := lipgloss.NewStyle().Foreground(o.MutedColor).Render("◌")
	title := lipgloss.NewStyle().Foreground(o.WhiteDim).Bold(true).Render("No data yet.")
	hint := o.Hint
	if hint == "" {
		hint = "Data will appear here as it becomes available."
	}
	hintStyled := lipgloss.NewStyle().Foreground(o.MutedColor).Render(hint)
	return []string{icon, "", title, hintStyled}
}

func renderDisconnected(o EmptyStateOptions, _ int) []string {
	icon := lipgloss.NewStyle().Foreground(o.Amber).Render("⚠")
	title := lipgloss.NewStyle().Foreground(o.Amber).Bold(true).Render("Daemon unreachable")
	retry := "Retrying…"
	if o.RetryInSecs > 0 {
		retry = fmt.Sprintf("Retrying in %ds…", o.RetryInSecs)
	}
	hint := lipgloss.NewStyle().Foreground(o.WhiteDim).Render(retry)
	return []string{icon, "", title, hint}
}

func renderGated(o EmptyStateOptions, _ int) []string {
	icon := lipgloss.NewStyle().Foreground(o.MutedColor).Render("⊘")
	title := lipgloss.NewStyle().Foreground(o.WhiteDim).Bold(true).Render("Feature gated")
	hint := o.Hint
	if hint == "" {
		hint = "Daemon configuration required to enable this view."
	}
	hintStyled := lipgloss.NewStyle().Foreground(o.MutedColor).Render(hint)
	return []string{icon, "", title, hintStyled}
}

// LoadingTick is emitted by a tea.Tick to advance the Loading animation.
type LoadingTick time.Time

// TickLoading returns a tea.Cmd that fires LoadingTick once after 150ms.
// Screens re-schedule it in their Update handler to maintain the animation loop.
func TickLoading() func() LoadingTick {
	return func() LoadingTick { return LoadingTick(time.Now()) }
}
