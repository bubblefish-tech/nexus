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
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestConfirmInput_InitialNotValid(t *testing.T) {
	t.Helper()
	ci := NewConfirmInput("Password", "enter password", true)
	if !ci.Valid() {
		t.Fatal("empty ConfirmInput should be valid (optional field)")
	}
}

func TestConfirmInput_ViewNonEmpty(t *testing.T) {
	t.Helper()
	ci := NewConfirmInput("Password", "enter password", true)
	v := ci.View()
	if v == "" {
		t.Fatal("expected non-empty view")
	}
	if !strings.Contains(v, "Password") {
		t.Fatalf("expected label 'Password' in view, got:\n%s", v)
	}
}

func TestConfirmInput_ValueReturnsInput(t *testing.T) {
	t.Helper()
	ci := NewConfirmInput("Username", "user", false)
	if ci.Value() != "" {
		t.Fatalf("expected empty initial value, got %q", ci.Value())
	}
}

func TestConfirmInput_MismatchInvalid(t *testing.T) {
	t.Helper()
	ci := NewConfirmInput("Password", "", true)
	ci.input.SetValue("secret123")
	ci.confirm.SetValue("different")
	if ci.Valid() {
		t.Fatal("mismatched entries should not be valid")
	}
}

func TestConfirmInput_MatchValid(t *testing.T) {
	t.Helper()
	ci := NewConfirmInput("Password", "", true)
	ci.input.SetValue("secret123")
	ci.confirm.SetValue("secret123")
	if !ci.Valid() {
		t.Fatal("matching entries should be valid")
	}
}

func TestConfirmInput_TabAdvancesPhase(t *testing.T) {
	t.Helper()
	ci := NewConfirmInput("Password", "", true)
	_ = ci.Focus()
	if ci.phase != confirmPhaseEnter {
		t.Fatal("expected confirmPhaseEnter after Focus")
	}
	_ = ci.Update(tea.KeyMsg{Type: tea.KeyTab})
	if ci.phase != confirmPhaseConfirm {
		t.Fatal("expected confirmPhaseConfirm after Tab")
	}

	viewConfirm := ci.View()
	if viewConfirm == "" {
		t.Fatal("expected non-empty view in confirm phase")
	}
}

func TestConfirmInput_ErrorShownOnMismatch(t *testing.T) {
	t.Helper()
	ci := NewConfirmInput("Token", "", true)
	_ = ci.Focus()
	ci.input.SetValue("abc")

	_ = ci.Update(tea.KeyMsg{Type: tea.KeyTab})
	ci.confirm.SetValue("xyz")

	_ = ci.Update(tea.KeyMsg{Type: tea.KeyEnter})

	view := ci.View()
	if !strings.Contains(view, "do not match") {
		t.Fatalf("expected mismatch error in view, got:\n%s", view)
	}
}

func TestConfirmInput_ViewContainsLabel(t *testing.T) {
	t.Helper()
	ci := NewConfirmInput("SecretKey", "", true)
	view := ci.View()
	if !strings.Contains(view, "SecretKey") {
		t.Fatalf("expected label in view, got:\n%s", view)
	}
	if !strings.Contains(view, "Confirm") {
		t.Fatalf("expected 'Confirm' label in view, got:\n%s", view)
	}
}
