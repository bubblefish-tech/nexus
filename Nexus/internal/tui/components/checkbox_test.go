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

func makeCheckboxList(n int) CheckboxList {
	items := make([]CheckboxItem, n)
	for i := range items {
		items[i] = CheckboxItem{Label: "item", Checked: false}
	}
	return CheckboxList{Items: items, Cursor: 0, Width: 60}
}

func TestCheckboxList_MoveDown(t *testing.T) {
	t.Helper()
	c := makeCheckboxList(3)
	c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if c.Cursor != 1 {
		t.Fatalf("expected cursor 1, got %d", c.Cursor)
	}
}

func TestCheckboxList_MoveUp(t *testing.T) {
	t.Helper()
	c := makeCheckboxList(3)
	c.Cursor = 2
	c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if c.Cursor != 1 {
		t.Fatalf("expected cursor 1, got %d", c.Cursor)
	}
}

func TestCheckboxList_NoMoveAboveZero(t *testing.T) {
	t.Helper()
	c := makeCheckboxList(3)
	c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if c.Cursor != 0 {
		t.Fatalf("expected cursor 0, got %d", c.Cursor)
	}
}

func TestCheckboxList_NoBelowLast(t *testing.T) {
	t.Helper()
	c := makeCheckboxList(2)
	c.Cursor = 1
	c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if c.Cursor != 1 {
		t.Fatalf("expected cursor 1, got %d", c.Cursor)
	}
}

func TestCheckboxList_Toggle(t *testing.T) {
	t.Helper()
	c := makeCheckboxList(3)
	c.Cursor = 1
	c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	if !c.Items[1].Checked {
		t.Fatal("expected item 1 to be checked")
	}
	c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	if c.Items[1].Checked {
		t.Fatal("expected item 1 to be unchecked after second toggle")
	}
}

func TestCheckboxList_DisabledNotToggled(t *testing.T) {
	t.Helper()
	c := CheckboxList{
		Items:  []CheckboxItem{{Label: "x", Disabled: true}},
		Width:  60,
	}
	c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	if c.Items[0].Checked {
		t.Fatal("disabled item should not be toggled")
	}
}

func TestCheckboxList_Selected(t *testing.T) {
	t.Helper()
	c := makeCheckboxList(4)
	c.Items[0].Checked = true
	c.Items[2].Checked = true
	sel := c.Selected()
	if len(sel) != 2 || sel[0] != 0 || sel[1] != 2 {
		t.Fatalf("expected [0, 2], got %v", sel)
	}
}

func TestCheckboxList_ViewNonEmpty(t *testing.T) {
	t.Helper()
	c := makeCheckboxList(2)
	v := c.View()
	if v == "" {
		t.Fatal("expected non-empty view")
	}
}
