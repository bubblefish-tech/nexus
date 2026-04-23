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
	"fmt"
	"strings"
	"time"

	"github.com/bubblefish-tech/nexus/internal/tui/components"
	"github.com/bubblefish-tech/nexus/internal/tui/styles"
	"github.com/charmbracelet/harmonica"
	"github.com/charmbracelet/lipgloss"

	tea "github.com/charmbracelet/bubbletea"
)

const splashDuration = 13500 * time.Millisecond
const splashTickInterval = 50 * time.Millisecond
const splashCrossFade = 500 * time.Millisecond

type splashTickMsg time.Time

// anim wraps a Harmonica spring for animating a single 0→1 property.
type anim struct {
	spring harmonica.Spring
	pos    float64
	vel    float64
	target float64
}

func newAnim(freq, damp float64) anim {
	return anim{spring: harmonica.NewSpring(harmonica.FPS(20), freq, damp)}
}

func (a *anim) activate() { a.target = 1.0 }

func (a *anim) tick() {
	a.pos, a.vel = a.spring.Update(a.pos, a.vel, a.target)
}

func (a *anim) p() float64 {
	if a.pos < 0 {
		return 0
	}
	if a.pos > 1 {
		return 1
	}
	return a.pos
}

// SplashModel renders the 3.5-second grandiose boot animation.
//
// Sequence: bubbles (0.0s) → fish emblem (0.4s) → tool dots (1.0s) →
// provenance chain (1.4s) → N E X U S letters (1.8s) → tagline (2.3s) →
// footer (2.7s) → ripple (3.0s) → auto-transition (3.5s).
//
// Every transition uses Harmonica springs. Skippable with any key;
// on skip, remaining elements cross-fade into place over 500ms.
type SplashModel struct {
	width, height int
	startTime     time.Time
	elapsed       time.Duration
	skipping      bool
	skipAt        time.Duration

	bubbleField *components.BubbleField
	bubbleFade  anim
	fishFade    anim
	dotsOnline  int
	chainFades  [5]anim
	letterFades [5]anim
	taglineFade anim
	footerFade  anim
	rippleFades [3]anim
}

// NewSplashModel creates the splash screen with Harmonica springs for each element.
func NewSplashModel() SplashModel {
	s := SplashModel{
		startTime:   time.Now(),
		bubbleField: components.NewBubbleField(120, 40, 50),
		bubbleFade:  newAnim(3.0, 0.8),
		fishFade:    newAnim(4.0, 0.7),
		taglineFade: newAnim(3.5, 0.8),
		footerFade:  newAnim(3.5, 0.8),
	}
	for i := range s.chainFades {
		s.chainFades[i] = newAnim(5.0, 0.7)
	}
	for i := range s.letterFades {
		s.letterFades[i] = newAnim(5.0, 0.7)
	}
	for i := range s.rippleFades {
		s.rippleFades[i] = newAnim(4.0, 0.8)
	}
	return s
}

// Init starts the animation tick loop.
func (s SplashModel) Init() tea.Cmd {
	return tea.Tick(splashTickInterval, func(t time.Time) tea.Msg {
		return splashTickMsg(t)
	})
}

// Update handles tick messages and skip-on-keypress with cross-fade.
func (s SplashModel) Update(msg tea.Msg) (SplashModel, tea.Cmd) {
	switch msg.(type) {
	case tea.KeyMsg:
		if !s.skipping {
			s.skipping = true
			s.skipAt = s.elapsed
			s.bubbleFade.activate()
			s.fishFade.activate()
			for i := range s.chainFades {
				s.chainFades[i].activate()
			}
			for i := range s.letterFades {
				s.letterFades[i].activate()
			}
			s.taglineFade.activate()
			s.footerFade.activate()
			s.dotsOnline = 12
		}
		return s, tea.Tick(splashTickInterval, func(t time.Time) tea.Msg {
			return splashTickMsg(t)
		})

	case splashTickMsg:
		s.elapsed = time.Since(s.startTime)

		if s.skipping && s.elapsed-s.skipAt >= splashCrossFade {
			return s, func() tea.Msg { return SplashDoneMsg{} }
		}
		if !s.skipping && s.elapsed >= splashDuration {
			return s, func() tea.Msg { return SplashDoneMsg{} }
		}

		s.bubbleField.Tick(splashTickInterval)
		ms := s.elapsed.Milliseconds()

		// Phase activations based on elapsed time.
		if ms >= 0 {
			s.bubbleFade.activate()
		}
		if ms >= 400 {
			s.fishFade.activate()
		}
		if ms >= 1000 {
			online := int(ms-1000) / 80
			if online > 12 {
				online = 12
			}
			s.dotsOnline = online
		}
		for i := range s.chainFades {
			if ms >= int64(1400+i*80) {
				s.chainFades[i].activate()
			}
		}
		for i := range s.letterFades {
			if ms >= int64(1800+i*40) {
				s.letterFades[i].activate()
			}
		}
		if ms >= 2300 {
			s.taglineFade.activate()
		}
		if ms >= 2700 {
			s.footerFade.activate()
		}
		for i := range s.rippleFades {
			if ms >= int64(3000+i*80) {
				s.rippleFades[i].activate()
			}
		}

		// Advance all springs.
		s.bubbleFade.tick()
		s.fishFade.tick()
		for i := range s.chainFades {
			s.chainFades[i].tick()
		}
		for i := range s.letterFades {
			s.letterFades[i].tick()
		}
		s.taglineFade.tick()
		s.footerFade.tick()
		for i := range s.rippleFades {
			s.rippleFades[i].tick()
		}

		return s, tea.Tick(splashTickInterval, func(t time.Time) tea.Msg {
			return splashTickMsg(t)
		})

	case tea.WindowSizeMsg:
		ws := msg.(tea.WindowSizeMsg)
		s.width = ws.Width
		s.height = ws.Height
		s.bubbleField.SetSize(s.width, s.height)
	}
	return s, nil
}

// View renders the splash with progressive reveal driven by Harmonica springs.
func (s SplashModel) View() string {
	if s.width < 40 || s.height < 10 {
		return ""
	}

	var content []string

	// Fish/shield emblem centered above block-letter banners.
	if fp := s.fishFade.p(); fp > 0.1 {
		content = append(content, components.RenderSplashEmblem())
		content = append(content, "")
		content = append(content, components.RenderSplashBanners(s.width))
	}

	content = append(content, "")

	// Tool dots — ring of 12 dots, each pulses teal when its tool comes online.
	if s.dotsOnline > 0 {
		content = append(content, s.viewToolDots())
	}

	content = append(content, "")

	// Provenance chain — 5 hash boxes, 80ms stagger left-to-right.
	if chain := s.viewChain(); chain != "" {
		content = append(content, chain)
		content = append(content, "")
	}

	// Tagline — 2.3s onset.
	if tp := s.taglineFade.p(); tp > 0.1 {
		c := splashFadeColor(tp, styles.ColorTealDim)
		tag := lipgloss.NewStyle().Foreground(c).
			Render("The Governed AI Cryptographic Substrate Control Plane")
		content = append(content, lipgloss.PlaceHorizontal(s.width, lipgloss.Center, tag))
		content = append(content, "")
	}

	// Footer — 2.7s onset.
	if fp := s.footerFade.p(); fp > 0.1 {
		c := splashFadeColor(fp, styles.TextMuted)
		footer := lipgloss.NewStyle().Foreground(c).
			Render("v0.1.3-public · bubblefish.sh · Copyright © 2026 Shawn Sammartano")
		content = append(content, lipgloss.PlaceHorizontal(s.width, lipgloss.Center, footer))
	}

	// Ripple — 3.0s onset, 3 expanding concentric rings over 500ms.
	if ripple := s.viewRipple(); ripple != "" {
		content = append(content, "", ripple)
	}

	body := strings.Join(content, "\n")

	// Composite: bubble field background with centered content overlay.
	if s.bubbleFade.p() > 0.05 {
		bg := s.bubbleField.Render()
		return s.compositeView(bg, body)
	}

	return lipgloss.Place(s.width, s.height, lipgloss.Center, lipgloss.Center, body)
}

// compositeView overlays centered content on the bubble field background.
// Content lines replace background lines; gap lines (empty) let bubbles show through.
func (s SplashModel) compositeView(bg, content string) string {
	bgLines := strings.Split(bg, "\n")
	contentLines := strings.Split(content, "\n")

	for len(bgLines) < s.height {
		bgLines = append(bgLines, strings.Repeat(" ", s.width))
	}
	if len(bgLines) > s.height {
		bgLines = bgLines[:s.height]
	}

	startY := (s.height - len(contentLines)) / 2
	if startY < 0 {
		startY = 0
	}

	for i, cline := range contentLines {
		y := startY + i
		if y >= 0 && y < len(bgLines) && strings.TrimSpace(cline) != "" {
			bgLines[y] = lipgloss.PlaceHorizontal(s.width, lipgloss.Center, cline)
		}
	}

	return strings.Join(bgLines, "\n")
}

func (s SplashModel) viewToolDots() string {
	var dots []string
	for i := 0; i < 12; i++ {
		if i < s.dotsOnline {
			dots = append(dots, lipgloss.NewStyle().Foreground(styles.ColorTeal).Render("·"))
		} else {
			dots = append(dots, lipgloss.NewStyle().Foreground(styles.TextDim).Render("·"))
		}
	}
	ring := strings.Join(dots, "  ")
	return lipgloss.PlaceHorizontal(s.width, lipgloss.Center, ring)
}

var provenanceHashes = [5]string{"a3f9", "c8d3", "7e2b", "91d0", "f4a8"}

func (s SplashModel) viewChain() string {
	var boxes []string
	anyVisible := false
	arrow := lipgloss.NewStyle().Foreground(styles.TextMuted).Render(" → ")
	for i := range s.chainFades {
		p := s.chainFades[i].p()
		if p < 0.1 {
			continue
		}
		anyVisible = true
		c := splashFadeColor(p, styles.ColorTeal)
		box := lipgloss.NewStyle().Foreground(c).Render(fmt.Sprintf("[%s]", provenanceHashes[i]))
		boxes = append(boxes, box)
	}
	if !anyVisible {
		return ""
	}
	chain := strings.Join(boxes, arrow)
	return lipgloss.PlaceHorizontal(s.width, lipgloss.Center, chain)
}

var nexusLetters = [5]string{"N", "E", "X", "U", "S"}

func (s SplashModel) viewLetters() string {
	var letters []string
	anyVisible := false
	for i := range s.letterFades {
		p := s.letterFades[i].p()
		if p < 0.1 {
			continue
		}
		anyVisible = true
		c := splashFadeColor(p, styles.ColorCyan)
		letter := lipgloss.NewStyle().Foreground(c).Bold(true).Render(nexusLetters[i])
		letters = append(letters, letter)
	}
	if !anyVisible {
		return ""
	}
	word := strings.Join(letters, "   ")
	return lipgloss.PlaceHorizontal(s.width, lipgloss.Center, word)
}

func (s SplashModel) viewRipple() string {
	radii := [3]int{8, 14, 22}
	var lines []string
	anyVisible := false
	for i := range s.rippleFades {
		p := s.rippleFades[i].p()
		if p < 0.1 {
			continue
		}
		anyVisible = true
		r := int(float64(radii[i]) * p)
		if r < 2 {
			r = 2
		}
		brightness := 1.0 - p*0.5
		c := splashFadeColor(brightness, styles.ColorTealDim)
		ring := lipgloss.NewStyle().Foreground(c).Render(strings.Repeat("·", r*2+1))
		lines = append(lines, lipgloss.PlaceHorizontal(s.width, lipgloss.Center, ring))
	}
	if !anyVisible {
		return ""
	}
	return strings.Join(lines, "\n")
}

// splashFadeColor maps a 0→1 spring progress to a 3-step terminal color fade.
func splashFadeColor(progress float64, full lipgloss.Color) lipgloss.Color {
	if progress < 0.3 {
		return styles.TextDim
	}
	if progress < 0.6 {
		return styles.TextMuted
	}
	return full
}
