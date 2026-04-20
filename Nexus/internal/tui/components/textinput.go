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
	"strings"

	"github.com/bubblefish-tech/nexus/internal/tui/styles"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// confirmPhase tracks which field is active in a ConfirmInput.
type confirmPhase int

const (
	confirmPhaseEnter   confirmPhase = iota
	confirmPhaseConfirm confirmPhase = iota
)

// ConfirmInput is a two-field text/password input that validates that both
// entries match. Set Password=true for masked entry.
type ConfirmInput struct {
	Label       string
	Placeholder string
	Password    bool
	input       textinput.Model
	confirm     textinput.Model
	phase       confirmPhase
	Err         string
	Width       int
}

// NewConfirmInput creates a ConfirmInput with the given label.
func NewConfirmInput(label, placeholder string, password bool) ConfirmInput {
	primary := textinput.New()
	primary.Placeholder = placeholder
	primary.CharLimit = 256
	if password {
		primary.EchoMode = textinput.EchoPassword
		primary.EchoCharacter = '•'
	}

	conf := textinput.New()
	conf.Placeholder = "re-enter " + strings.ToLower(label)
	conf.CharLimit = 256
	if password {
		conf.EchoMode = textinput.EchoPassword
		conf.EchoCharacter = '•'
	}

	return ConfirmInput{
		Label:       label,
		Placeholder: placeholder,
		Password:    password,
		input:       primary,
		confirm:     conf,
	}
}

// Focus activates the primary input field.
func (c *ConfirmInput) Focus() tea.Cmd {
	c.phase = confirmPhaseEnter
	c.confirm.Blur()
	return c.input.Focus()
}

// Value returns the primary input value (or "" if confirmation failed).
func (c ConfirmInput) Value() string { return c.input.Value() }

// Valid returns true when the field is empty (optional) or both entries match.
func (c ConfirmInput) Valid() bool {
	if c.input.Value() == "" {
		return true
	}
	return c.input.Value() == c.confirm.Value()
}

// Update routes keyboard input to the active field.
func (c *ConfirmInput) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "tab", "enter":
			if c.phase == confirmPhaseEnter {
				c.phase = confirmPhaseConfirm
				c.input.Blur()
				return c.confirm.Focus()
			}
			// Validate.
			if c.input.Value() != c.confirm.Value() {
				c.Err = "entries do not match"
				c.confirm.SetValue("")
				c.phase = confirmPhaseEnter
				c.input.SetValue("")
				return c.input.Focus()
			}
			c.Err = ""
			return nil
		case "shift+tab":
			if c.phase == confirmPhaseConfirm {
				c.phase = confirmPhaseEnter
				c.confirm.Blur()
				return c.input.Focus()
			}
		}
	}
	if c.phase == confirmPhaseEnter {
		c.input, cmd = c.input.Update(msg)
	} else {
		c.confirm, cmd = c.confirm.Update(msg)
	}
	return cmd
}

// View renders the two input fields.
func (c ConfirmInput) View() string {
	labelStyle := lipgloss.NewStyle().Foreground(styles.TextSecondary).Width(22)
	var b strings.Builder
	b.WriteString(labelStyle.Render(c.Label+":") + " " + c.input.View() + "\n")
	b.WriteString(labelStyle.Render("Confirm "+c.Label+":") + " " + c.confirm.View() + "\n")
	if c.Err != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(styles.ColorRed).Render("✗  "+c.Err) + "\n")
	}
	return lipgloss.NewStyle().Width(c.Width).Render(b.String())
}
