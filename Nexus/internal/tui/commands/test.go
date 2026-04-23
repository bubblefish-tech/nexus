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
	"fmt"
	"sync"

	"github.com/bubblefish-tech/nexus/internal/tui/api"
	tea "github.com/charmbracelet/bubbletea"
)

// TestCaseResult holds the pass/fail outcome of a single test case.
type TestCaseResult struct {
	Name    string
	Desc    string
	Passed  bool
	ErrMsg  string
}

// TestResultMsg carries the full outcome of a test category run.
type TestResultMsg struct {
	Category string
	Results  []TestCaseResult
	Passed   int
	Failed   int
	Err      error
}

// testCase is a single check with a name, description, and a run function.
type testCase struct {
	name string
	desc string
	run  func(client *api.Client) (bool, string)
}

// testCategory groups related tests.
type testCategory struct {
	name  string
	tests []testCase
}

// testCategories defines all available test categories.
var testCategories = []testCategory{
	{
		name: "Quick Health",
		tests: []testCase{
			{
				name: "daemon_alive",
				desc: "Daemon responds to health probe",
				run: func(c *api.Client) (bool, string) {
					ok, err := c.Health()
					if err != nil {
						return false, err.Error()
					}
					if !ok {
						return false, "health returned false"
					}
					return true, ""
				},
			},
			{
				name: "daemon_ready",
				desc: "Daemon reports ready state",
				run: func(c *api.Client) (bool, string) {
					ok, err := c.Ready()
					if err != nil {
						return false, err.Error()
					}
					if !ok {
						return false, "ready returned false"
					}
					return true, ""
				},
			},
			{
				name: "status_ok",
				desc: "Status API returns ok",
				run: func(c *api.Client) (bool, string) {
					st, err := c.Status()
					if err != nil {
						return false, err.Error()
					}
					if st.Status != "ok" {
						return false, fmt.Sprintf("status=%q", st.Status)
					}
					return true, ""
				},
			},
			{
				name: "config_readable",
				desc: "Config API accessible",
				run: func(c *api.Client) (bool, string) {
					_, err := c.Config()
					if err != nil {
						return false, err.Error()
					}
					return true, ""
				},
			},
			{
				name: "audit_accessible",
				desc: "Audit log API accessible",
				run: func(c *api.Client) (bool, string) {
					_, err := c.AuditLog(1)
					if err != nil {
						return false, err.Error()
					}
					return true, ""
				},
			},
		},
	},
	{
		name: "Core",
		tests: []testCase{
			{
				name: "lint_clean",
				desc: "No lint errors in config",
				run: func(c *api.Client) (bool, string) {
					resp, err := c.Lint()
					if err != nil {
						return false, err.Error()
					}
					for _, f := range resp.Findings {
						if f.Severity == "error" {
							return false, fmt.Sprintf("lint error: %s", f.Message)
						}
					}
					return true, ""
				},
			},
			{
				name: "security_summary",
				desc: "Security summary accessible",
				run: func(c *api.Client) (bool, string) {
					_, err := c.SecuritySummary()
					if err != nil {
						return false, err.Error()
					}
					return true, ""
				},
			},
		},
	},
	{
		name: "Full Suite",
		tests: nil, // populated dynamically (all categories combined)
	},
}

func init() {
	// Populate Full Suite with all tests from all non-full-suite categories.
	var all []testCase
	for _, cat := range testCategories {
		if cat.name != "Full Suite" {
			all = append(all, cat.tests...)
		}
	}
	for i := range testCategories {
		if testCategories[i].name == "Full Suite" {
			testCategories[i].tests = all
		}
	}
}

func categories() []string {
	out := make([]string, len(testCategories))
	for i, c := range testCategories {
		out[i] = c.name
	}
	return out
}

// TestCommand opens the test runner.
type TestCommand struct{}

var _ Command = TestCommand{}

func (TestCommand) Name() string        { return "test" }
func (TestCommand) Description() string { return "Run test suite (select category)" }

// Execute runs the Quick Health category by default. TUI wiring for category
// selection is handled by the running view in TUI.5.
func (TestCommand) Execute(client *api.Client) tea.Cmd {
	return runCategory(client, "Quick Health")
}

func runCategory(client *api.Client, categoryName string) tea.Cmd {
	return func() tea.Msg {
		for _, cat := range testCategories {
			if cat.name == categoryName {
				return executeCategory(client, cat)
			}
		}
		return TestResultMsg{
			Category: categoryName,
			Err:      fmt.Errorf("unknown category %q", categoryName),
		}
	}
}

func executeCategory(client *api.Client, cat testCategory) TestResultMsg {
	results := make([]TestCaseResult, len(cat.tests))
	var wg sync.WaitGroup
	wg.Add(len(cat.tests))
	for i, tc := range cat.tests {
		go func(idx int, t testCase) {
			defer wg.Done()
			ok, errMsg := t.run(client)
			results[idx] = TestCaseResult{
				Name:   t.name,
				Desc:   t.desc,
				Passed: ok,
				ErrMsg: errMsg,
			}
		}(i, tc)
	}
	wg.Wait()

	passed, failed := 0, 0
	for _, r := range results {
		if r.Passed {
			passed++
		} else {
			failed++
		}
	}
	return TestResultMsg{
		Category: cat.name,
		Results:  results,
		Passed:   passed,
		Failed:   failed,
	}
}
