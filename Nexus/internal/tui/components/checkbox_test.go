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
	view := c.View()
	if view == "" {
		t.Fatal("expected non-empty view after move down")
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

func TestCheckboxList_Toggle_ViewReflectsState(t *testing.T) {
	t.Helper()
	c := CheckboxList{
		Items: []CheckboxItem{
			{Label: "Alpha", Checked: false},
			{Label: "Bravo", Checked: false},
		},
		Cursor: 0,
		Width:  60,
	}

	viewBefore := c.View()

	c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	if !c.Items[0].Checked {
		t.Fatal("expected item 0 to be checked after space")
	}

	viewAfter := c.View()
	if viewBefore == viewAfter {
		t.Fatal("expected view to change after toggling checkbox")
	}

	c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	if c.Items[0].Checked {
		t.Fatal("expected item 0 to be unchecked after second toggle")
	}
}

func TestCheckboxList_DisabledNotToggled(t *testing.T) {
	t.Helper()
	c := CheckboxList{
		Items: []CheckboxItem{{Label: "x", Disabled: true}},
		Width: 60,
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

func TestCheckboxList_ViewContainsLabels(t *testing.T) {
	t.Helper()
	c := CheckboxList{
		Items: []CheckboxItem{
			{Label: "Feature_A"},
			{Label: "Feature_B"},
		},
		Width: 80,
	}
	view := c.View()
	if !strings.Contains(view, "Feature_A") {
		t.Fatalf("expected 'Feature_A' in view, got:\n%s", view)
	}
	if !strings.Contains(view, "Feature_B") {
		t.Fatalf("expected 'Feature_B' in view, got:\n%s", view)
	}
}

func TestCheckboxList_CursorHighlightsDifferentItem(t *testing.T) {
	t.Helper()
	c := CheckboxList{
		Items: []CheckboxItem{
			{Label: "First"},
			{Label: "Second"},
			{Label: "Third"},
		},
		Cursor: 0,
		Width:  60,
	}

	view0 := c.View()
	c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	view1 := c.View()

	if view0 == view1 {
		t.Fatal("expected cursor movement to change the rendered view")
	}
}
