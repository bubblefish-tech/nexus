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

package tui

import (
	"strings"
	"time"

	"github.com/bubblefish-tech/nexus/internal/tui/components"
	"github.com/bubblefish-tech/nexus/internal/tui/styles"
	"github.com/charmbracelet/lipgloss"

	tea "github.com/charmbracelet/bubbletea"
)

const splashDuration = 1200 * time.Millisecond
const splashTickInterval = 50 * time.Millisecond

type splashTickMsg time.Time

// SplashModel renders the 1.2-second boot animation.
// Sequence: fish emblem fade-in → N E X U S lettermark → tagline → footer.
type SplashModel struct {
	width, height int
	startTime     time.Time
	elapsed       time.Duration
}

// NewSplashModel creates the splash screen model.
func NewSplashModel() SplashModel {
	return SplashModel{
		startTime: time.Now(),
	}
}

// Init starts the animation tick loop.
func (s SplashModel) Init() tea.Cmd {
	return tea.Tick(splashTickInterval, func(t time.Time) tea.Msg {
		return splashTickMsg(t)
	})
}

// Update handles tick messages and skip-on-keypress.
func (s SplashModel) Update(msg tea.Msg) (SplashModel, tea.Cmd) {
	switch msg.(type) {
	case tea.KeyMsg:
		return s, func() tea.Msg { return SplashDoneMsg{} }
	case splashTickMsg:
		s.elapsed = time.Since(s.startTime)
		if s.elapsed >= splashDuration {
			return s, func() tea.Msg { return SplashDoneMsg{} }
		}
		return s, tea.Tick(splashTickInterval, func(t time.Time) tea.Msg {
			return splashTickMsg(t)
		})
	case tea.WindowSizeMsg:
		ws := msg.(tea.WindowSizeMsg)
		s.width = ws.Width
		s.height = ws.Height
	}
	return s, nil
}

// View renders the splash with progressive reveal based on elapsed time.
func (s SplashModel) View() string {
	if s.width < 40 || s.height < 10 {
		return ""
	}

	progress := float64(s.elapsed) / float64(splashDuration)
	if progress > 1 {
		progress = 1
	}

	var content []string

	// Phase 1 (0.0-0.4s): Fish emblem appears
	if progress > 0.08 {
		fish := components.Logo{Width: s.width}
		content = append(content, fish.View())
	}

	// Phase 2 (0.4-0.7s): N E X U S wordmark
	if progress > 0.33 {
		letters := "N E X U S"
		wordmark := lipgloss.NewStyle().
			Foreground(styles.ColorCyan).
			Bold(true).
			Render(letters)
		content = append(content, lipgloss.PlaceHorizontal(s.width, lipgloss.Center, wordmark))
		content = append(content, "")
	}

	// Phase 3 (0.7-1.0s): Tagline
	if progress > 0.58 {
		tagline := lipgloss.NewStyle().
			Foreground(styles.ColorTealDim).
			Render("The Governed AI Cryptographic Substrate Control Plane")
		content = append(content, lipgloss.PlaceHorizontal(s.width, lipgloss.Center, tagline))
		content = append(content, "")
	}

	// Phase 4 (1.0-1.2s): Footer
	if progress > 0.83 {
		footer := lipgloss.NewStyle().
			Foreground(styles.TextMuted).
			Render("v0.1.3-public · bubblefish.sh · Copyright © 2026 Shawn Sammartano")
		content = append(content, lipgloss.PlaceHorizontal(s.width, lipgloss.Center, footer))
	}

	body := strings.Join(content, "\n")
	return lipgloss.Place(s.width, s.height, lipgloss.Center, lipgloss.Center, body)
}
