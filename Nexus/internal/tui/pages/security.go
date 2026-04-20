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
	"strings"

	"github.com/bubblefish-tech/nexus/internal/tui/styles"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// securityPhase tracks which input field is active.
type securityPhase int

const (
	phasePassword securityPhase = iota
	phaseConfirm
)

// SecurityPage collects an optional encryption password with confirmation.
type SecurityPage struct {
	phase    securityPhase
	password textinput.Model
	confirm  textinput.Model
	err      string
}

var _ Page = (*SecurityPage)(nil)

// NewSecurityPage returns a SecurityPage with masked inputs.
func NewSecurityPage() *SecurityPage {
	pw := textinput.New()
	pw.Placeholder = "leave blank to skip encryption"
	pw.EchoMode = textinput.EchoPassword
	pw.EchoCharacter = '•'
	pw.CharLimit = 128

	cf := textinput.New()
	cf.Placeholder = "re-enter password"
	cf.EchoMode = textinput.EchoPassword
	cf.EchoCharacter = '•'
	cf.CharLimit = 128

	return &SecurityPage{password: pw, confirm: cf}
}

func (p *SecurityPage) Name() string { return "Encryption" }

func (p *SecurityPage) Init(state *WizardState) tea.Cmd {
	p.phase = phasePassword
	p.password.Focus()
	p.confirm.Blur()
	p.err = ""
	return textinput.Blink
}

func (p *SecurityPage) Update(msg tea.Msg, state *WizardState) (Page, tea.Cmd) {
	var cmd tea.Cmd
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "tab", "enter":
			if p.phase == phasePassword {
				p.phase = phaseConfirm
				p.password.Blur()
				p.confirm.Focus()
				return p, textinput.Blink
			}
			// On confirm phase: validate and commit.
			pw := p.password.Value()
			cf := p.confirm.Value()
			if pw != cf {
				p.err = "Passwords do not match — please try again."
				p.confirm.SetValue("")
				p.phase = phasePassword
				p.password.SetValue("")
				p.password.Focus()
				p.confirm.Blur()
				return p, textinput.Blink
			}
			state.EncryptionPass = pw
			p.err = ""
			return p, nil
		case "shift+tab":
			if p.phase == phaseConfirm {
				p.phase = phasePassword
				p.confirm.Blur()
				p.password.Focus()
				return p, textinput.Blink
			}
		}
	}

	if p.phase == phasePassword {
		p.password, cmd = p.password.Update(msg)
	} else {
		p.confirm, cmd = p.confirm.Update(msg)
	}
	return p, cmd
}

func (p *SecurityPage) CanAdvance(state *WizardState) bool {
	// Skip page allowed (blank password = no encryption).
	if p.password.Value() == "" {
		return true
	}
	return p.password.Value() == p.confirm.Value()
}

func (p *SecurityPage) View(width, height int) string {
	var b strings.Builder

	title := lipgloss.NewStyle().Foreground(styles.ColorTeal).Bold(true).
		Render("Encryption Password (optional)")
	b.WriteString(title + "\n\n")
	b.WriteString(lipgloss.NewStyle().Foreground(styles.TextMuted).
		Render("Setting a password encrypts memories, config secrets, and audit events at rest.\nLeave blank to skip encryption.") + "\n\n")

	labelStyle := lipgloss.NewStyle().Foreground(styles.TextSecondary).Width(20)
	b.WriteString(labelStyle.Render("Password:") + " " + p.password.View() + "\n\n")
	b.WriteString(labelStyle.Render("Confirm password:") + " " + p.confirm.View() + "\n\n")

	if p.err != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(styles.ColorRed).Bold(true).
			Render("✗  "+p.err) + "\n\n")
	}

	b.WriteString(lipgloss.NewStyle().Foreground(styles.TextMuted).
		Render("Tab to move between fields  ·  Ctrl+N to continue"))
	return lipgloss.NewStyle().Width(width).Render(b.String())
}
