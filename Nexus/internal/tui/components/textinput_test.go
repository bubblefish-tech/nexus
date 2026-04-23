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
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestConfirmInput_InitialNotValid(t *testing.T) {
	t.Helper()
	ci := NewConfirmInput("Password", "enter password", true)
	// Empty primary is valid (optional field by design).
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
	// Simulate typing in primary field.
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
	// Tab should advance to confirm phase.
	_ = ci.Update(tea.KeyMsg{Type: tea.KeyTab})
	if ci.phase != confirmPhaseConfirm {
		t.Fatal("expected confirmPhaseConfirm after Tab")
	}
}
