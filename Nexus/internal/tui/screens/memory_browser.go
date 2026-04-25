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
	"time"

	"github.com/bubblefish-tech/nexus/internal/tui/api"
	"github.com/bubblefish-tech/nexus/internal/tui/components"
	"github.com/bubblefish-tech/nexus/internal/tui/styles"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type memorySearchMsg struct {
	memories []api.Memory
	errKind  api.ErrorKind
	hint     string
}

var memoryKeys = struct {
	Search key.Binding
	Edit   key.Binding
	Delete key.Binding
	Proof  key.Binding
}{
	Search: key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
	Edit:   key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit")),
	Delete: key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
	Proof:  key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "proof")),
}

// MemoryBrowserScreen is Page 2 — search + inspect memories.
type MemoryBrowserScreen struct {
	width, height int
	searchInput   textinput.Model
	searching     bool
	records       []api.Memory
	selectedIdx   int
	errKind       api.ErrorKind
	errHint       string
	loading       bool
	lastQuery     string
	client        *api.Client
}

// NewMemoryBrowserScreen creates the memory browser.
func NewMemoryBrowserScreen() *MemoryBrowserScreen {
	ti := textinput.New()
	ti.Placeholder = "search memories..."
	ti.CharLimit = 256
	ti.Width = 60
	ti.PromptStyle = lipgloss.NewStyle().Foreground(styles.ColorTeal)
	ti.TextStyle = lipgloss.NewStyle().Foreground(styles.TextPrimary)
	return &MemoryBrowserScreen{
		searchInput: ti,
		loading:     true,
	}
}

func (m *MemoryBrowserScreen) Name() string     { return "Memory" }
func (m *MemoryBrowserScreen) Init() tea.Cmd    { return nil }
func (m *MemoryBrowserScreen) SetSize(w, h int) { m.width = w; m.height = h }

func (m *MemoryBrowserScreen) ShortHelp() []key.Binding {
	return []key.Binding{memoryKeys.Search, memoryKeys.Edit, memoryKeys.Delete, memoryKeys.Proof}
}

func (m *MemoryBrowserScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case memorySearchMsg:
		m.loading = false
		m.errKind = msg.errKind
		m.errHint = msg.hint
		if msg.errKind == api.ErrKindUnknown {
			m.records = msg.memories
			m.selectedIdx = 0
		}
		return m, nil

	case tea.KeyMsg:
		if m.searching {
			switch msg.String() {
			case "esc":
				m.searching = false
				m.searchInput.Blur()
				return m, nil
			case "enter":
				m.searching = false
				m.lastQuery = m.searchInput.Value()
				m.searchInput.Blur()
				if m.client != nil && m.lastQuery != "" {
					return m, m.searchCmd()
				}
				return m, nil
			default:
				var cmd tea.Cmd
				m.searchInput, cmd = m.searchInput.Update(msg)
				return m, cmd
			}
		}

		switch {
		case key.Matches(msg, memoryKeys.Search):
			m.searching = true
			m.searchInput.Focus()
			return m, textinput.Blink
		case msg.String() == "j" || msg.String() == "down":
			if m.selectedIdx < len(m.records)-1 {
				m.selectedIdx++
			}
			return m, nil
		case msg.String() == "k" || msg.String() == "up":
			if m.selectedIdx > 0 {
				m.selectedIdx--
			}
			return m, nil
		}
	}

	if m.searching {
		var cmd tea.Cmd
		m.searchInput, cmd = m.searchInput.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m *MemoryBrowserScreen) FireRefresh(client *api.Client) tea.Cmd {
	m.client = client
	return func() tea.Msg {
		resp, err := client.ListMemories(50, 0)
		if err != nil {
			kind := api.Classify(err)
			sdbg("ListMemories failed kind=%d err=%v", kind, err)
			return memorySearchMsg{errKind: kind, hint: api.HintForEndpoint("/api/memories", kind)}
		}
		return memorySearchMsg{memories: resp.Memories}
	}
}

func (m *MemoryBrowserScreen) searchCmd() tea.Cmd {
	query := m.lastQuery
	client := m.client
	return func() tea.Msg {
		resp, err := client.SearchMemories(query, 50)
		if err != nil {
			kind := api.Classify(err)
			sdbg("SearchMemories failed kind=%d err=%v", kind, err)
			return memorySearchMsg{errKind: kind, hint: api.HintForEndpoint("/api/memories", kind)}
		}
		return memorySearchMsg{memories: resp.Memories}
	}
}

func (m *MemoryBrowserScreen) View() string {
	if m.width < 40 || m.height < 10 {
		return ""
	}

	if m.loading {
		frame := int(time.Now().UnixMilli()/150) % 8
		return components.Render(loadingOpts(m.width, m.height, frame))
	}
	if m.errKind != api.ErrKindUnknown {
		return components.Render(emptyStateOpts(m.errKind, m.errHint, m.width, m.height))
	}

	var lines []string

	lines = append(lines, sectionHeader("SEARCH", m.width))
	if m.searching {
		lines = append(lines, "  "+m.searchInput.View())
	} else {
		query := m.searchInput.Value()
		if query == "" {
			query = "(all memories)"
		}
		lines = append(lines, fmt.Sprintf("  [/] %s",
			lipgloss.NewStyle().Foreground(styles.TextWhiteDim).Render(query)))
	}
	lines = append(lines, "")

	listW := m.width * 35 / 100
	if listW < 25 {
		listW = 25
	}
	detailW := m.width - listW - 1

	left := m.viewResultsList(listW)
	right := m.viewDetail(detailW)

	body := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(listW).Render(left),
		lipgloss.NewStyle().Width(detailW).Render(right),
	)
	lines = append(lines, body)

	return lipgloss.NewStyle().Width(m.width).Height(m.height).
		Render(strings.Join(lines, "\n"))
}

func (m *MemoryBrowserScreen) viewResultsList(w int) string {
	var lines []string
	lines = append(lines, sectionHeader(fmt.Sprintf("RESULTS (%d)", len(m.records)), w))
	lines = append(lines, "")

	if len(m.records) == 0 {
		lines = append(lines, styles.MutedStyle.Render("  No results"))
		return strings.Join(lines, "\n")
	}

	visible := m.height - 8
	if visible < 5 {
		visible = 5
	}
	start := 0
	if m.selectedIdx >= visible {
		start = m.selectedIdx - visible + 1
	}
	end := start + visible
	if end > len(m.records) {
		end = len(m.records)
	}

	for i := start; i < end; i++ {
		rec := m.records[i]
		prefix := "  "
		if i == m.selectedIdx {
			prefix = lipgloss.NewStyle().Foreground(styles.ColorTeal).Render("▸ ")
		}

		content := rec.Content
		if content == "" {
			content = rec.ID
		}
		maxLen := w - 6
		if maxLen < 10 {
			maxLen = 10
		}
		if len(content) > maxLen {
			content = content[:maxLen-3] + "..."
		}

		nameStyle := styles.TextPrimary
		if i == m.selectedIdx {
			nameStyle = styles.ColorTeal
		}
		line := prefix + lipgloss.NewStyle().Foreground(nameStyle).Render(content)
		lines = append(lines, line)

		meta := fmt.Sprintf("    %s · %s", rec.Source, rec.CreatedAt)
		lines = append(lines, lipgloss.NewStyle().Foreground(styles.TextMuted).Render(meta))
	}

	return strings.Join(lines, "\n")
}

func (m *MemoryBrowserScreen) viewDetail(w int) string {
	var lines []string
	lines = append(lines, sectionHeader("SELECTED MEMORY", w))
	lines = append(lines, "")

	if len(m.records) == 0 || m.selectedIdx >= len(m.records) {
		lines = append(lines, styles.MutedStyle.Render("  Select a memory to view details"))
		return strings.Join(lines, "\n")
	}

	rec := m.records[m.selectedIdx]

	idStyle := lipgloss.NewStyle().Foreground(styles.ColorTealDim)
	lines = append(lines, "  "+idStyle.Render(rec.ID))
	lines = append(lines, fmt.Sprintf("  %s", rec.CreatedAt))
	actor := rec.ActorType
	if actor == "" {
		actor = rec.Actor
	}
	if actor == "" {
		actor = "—"
	}
	lines = append(lines, fmt.Sprintf("  source: %s  ·  actor: %s", rec.Source, actor))
	if rec.Namespace != "" {
		lines = append(lines, fmt.Sprintf("  namespace: %s", rec.Namespace))
	}
	lines = append(lines, "")

	sep := lipgloss.NewStyle().Foreground(styles.BorderBase).
		Render(strings.Repeat("─", w-4))
	lines = append(lines, "  "+sep)
	lines = append(lines, "")

	content := rec.Content
	contentW := w - 4
	if contentW < 20 {
		contentW = 20
	}
	wrapped := wrapText(content, contentW)
	for _, line := range wrapped {
		lines = append(lines, "  "+lipgloss.NewStyle().Foreground(styles.TextPrimary).Render(line))
	}
	lines = append(lines, "")
	lines = append(lines, "  "+sep)
	lines = append(lines, "")

	// Provenance section (populated when daemon returns hash/sig fields).
	if rec.Score > 0 {
		lines = append(lines, "  "+sep)
		lines = append(lines, "")
		lines = append(lines, lipgloss.NewStyle().Foreground(styles.ColorTeal).Bold(true).
			Render("  ◈ RETRIEVAL SCORE"))
		lines = append(lines, fmt.Sprintf("    rrf:   %.2f", rec.Score))
	}

	lines = append(lines, "")
	actions := lipgloss.NewStyle().Foreground(styles.TextMuted).
		Render("  [e] edit  [d] delete  [p] proof")
	lines = append(lines, actions)

	return strings.Join(lines, "\n")
}

func wrapText(s string, width int) []string {
	if width < 1 {
		return []string{s}
	}
	var lines []string
	for len(s) > width {
		cut := width
		if space := strings.LastIndex(s[:cut], " "); space > width/2 {
			cut = space
		}
		lines = append(lines, s[:cut])
		s = s[cut:]
		if len(s) > 0 && s[0] == ' ' {
			s = s[1:]
		}
	}
	if len(s) > 0 {
		lines = append(lines, s)
	}
	return lines
}

var _ Screen = (*MemoryBrowserScreen)(nil)
