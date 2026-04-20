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

package commands

import (
	"github.com/bubblefish-tech/nexus/internal/tui/api"
	tea "github.com/charmbracelet/bubbletea"
)

// TestResultMsg carries the outcome of a test run.
// Full implementation added in TUI.5.
type TestResultMsg struct {
	Category string
	Passed   int
	Failed   int
	Err      error
}

// TestCommand opens the test runner category selector.
// Real implementation added in TUI.5.
type TestCommand struct{}

var _ Command = TestCommand{}

func (TestCommand) Name() string        { return "test" }
func (TestCommand) Description() string { return "Run test suite (select category)" }

func (TestCommand) Execute(client *api.Client) tea.Cmd {
	return func() tea.Msg {
		// Quick health check — full category UI added in TUI.5.
		ok, err := client.Health()
		if err != nil {
			return TestResultMsg{Category: "health", Failed: 1, Err: err}
		}
		passed := 0
		if ok {
			passed = 1
		}
		return TestResultMsg{Category: "health", Passed: passed}
	}
}
