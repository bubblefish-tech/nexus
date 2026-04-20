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

package pages

import (
	"fmt"
	"strings"

	"github.com/bubblefish-tech/nexus/internal/tui/styles"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var dbChoices = []struct {
	key         string
	label       string
	desc        string
	needsDSN    bool
	dsnLabel    string
	dsnExample  string
}{
	{"sqlite", "SQLite (local)", "Zero-config embedded database — recommended for single-node installs", false, "", ""},
	{"postgres", "PostgreSQL", "Production-grade relational DB with pgvector support", true, "Connection string", "postgres://user:pass@localhost:5432/nexus"},
	{"mysql", "MySQL / MariaDB", "MySQL-compatible with app-level vector search", true, "Connection string", "user:pass@tcp(localhost:3306)/nexus"},
	{"cockroachdb", "CockroachDB", "Distributed SQL, PostgreSQL-compatible", true, "Connection string", "postgres://user:pass@localhost:26257/nexus"},
	{"mongodb", "MongoDB", "Document store with Atlas Vector Search", true, "MongoDB URI", "mongodb://user:pass@localhost:27017/nexus"},
	{"firestore", "Firebase / Firestore", "Google Firestore — schemaless, serverless", true, "Project ID", "my-firebase-project"},
	{"tidb", "TiDB", "MySQL-compatible distributed DB with native vector type", true, "Connection string", "user:pass@tcp(localhost:4000)/nexus"},
	{"turso", "Turso / libSQL", "SQLite-compatible with edge replication", true, "Turso URL", "libsql://database.turso.io?authToken=TOKEN"},
}

// DatabasePage handles database type selection and optional DSN input.
type DatabasePage struct {
	cursor   int
	dsnInput textinput.Model
}

var _ Page = (*DatabasePage)(nil)

// NewDatabasePage returns a DatabasePage defaulting to SQLite.
func NewDatabasePage() *DatabasePage {
	ti := textinput.New()
	ti.Placeholder = "connection string"
	ti.CharLimit = 512
	return &DatabasePage{dsnInput: ti}
}

func (p *DatabasePage) Name() string { return "Database Selection" }

func (p *DatabasePage) Init(state *WizardState) tea.Cmd {
	// Restore saved cursor position.
	for i, c := range dbChoices {
		if c.key == state.DatabaseType {
			p.cursor = i
			break
		}
	}
	if state.DatabaseType == "" {
		state.DatabaseType = "sqlite"
	}
	p.dsnInput.SetValue(state.DatabaseDSN)
	if dbChoices[p.cursor].needsDSN {
		p.dsnInput.Placeholder = dbChoices[p.cursor].dsnExample
		return textinput.Blink
	}
	return nil
}

func (p *DatabasePage) Update(msg tea.Msg, state *WizardState) (Page, tea.Cmd) {
	choice := dbChoices[p.cursor]
	if choice.needsDSN && p.dsnInput.Focused() {
		var cmd tea.Cmd
		p.dsnInput, cmd = p.dsnInput.Update(msg)
		state.DatabaseDSN = p.dsnInput.Value()
		if k, ok := msg.(tea.KeyMsg); ok && (k.String() == "esc" || k.String() == "shift+tab") {
			p.dsnInput.Blur()
		}
		return p, cmd
	}

	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "up", "k":
			if p.cursor > 0 {
				p.cursor--
				p.onCursorChange(state)
			}
		case "down", "j":
			if p.cursor < len(dbChoices)-1 {
				p.cursor++
				p.onCursorChange(state)
			}
		case " ", "enter":
			state.DatabaseType = dbChoices[p.cursor].key
			if dbChoices[p.cursor].needsDSN {
				p.dsnInput.Focus()
				return p, textinput.Blink
			}
		case "tab":
			if dbChoices[p.cursor].needsDSN {
				p.dsnInput.Focus()
				return p, textinput.Blink
			}
		}
	}
	return p, nil
}

func (p *DatabasePage) onCursorChange(state *WizardState) {
	state.DatabaseType = dbChoices[p.cursor].key
	p.dsnInput.Placeholder = dbChoices[p.cursor].dsnExample
	p.dsnInput.Blur()
	if dbChoices[p.cursor].key == "sqlite" {
		state.DatabaseDSN = ""
		p.dsnInput.SetValue("")
	}
}

func (p *DatabasePage) CanAdvance(state *WizardState) bool {
	if state.DatabaseType == "" || state.DatabaseType == "sqlite" {
		return true
	}
	for _, c := range dbChoices {
		if c.key == state.DatabaseType && c.needsDSN {
			return state.DatabaseDSN != ""
		}
	}
	return true
}

func (p *DatabasePage) View(width, height int) string {
	var b strings.Builder
	title := lipgloss.NewStyle().Foreground(styles.ColorTeal).Bold(true).
		Render("Choose a database backend")
	b.WriteString(title + "\n\n")
	b.WriteString(lipgloss.NewStyle().Foreground(styles.TextMuted).
		Render("↑/↓ or j/k to navigate  ·  Space or Enter to select  ·  Tab to enter DSN") + "\n\n")

	for i, c := range dbChoices {
		cursor := "  "
		rowStyle := lipgloss.NewStyle().Foreground(styles.TextSecondary)
		if i == p.cursor {
			cursor = lipgloss.NewStyle().Foreground(styles.ColorTeal).Render("▶ ")
			rowStyle = lipgloss.NewStyle().Foreground(styles.TextPrimary).Bold(true)
		}
		b.WriteString(fmt.Sprintf("%s%s\n", cursor, rowStyle.Render(c.label)))
		b.WriteString(fmt.Sprintf("    %s\n",
			lipgloss.NewStyle().Foreground(styles.TextMuted).Render(c.desc)))
		if i == p.cursor && c.needsDSN {
			b.WriteString(fmt.Sprintf("    %s: %s\n",
				lipgloss.NewStyle().Foreground(styles.ColorBlue).Render(c.dsnLabel),
				p.dsnInput.View()))
		}
		b.WriteString("\n")
	}
	return lipgloss.NewStyle().Width(width).Render(b.String())
}
