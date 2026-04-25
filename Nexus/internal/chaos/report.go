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

package chaos

import "time"

// DrillReport is the machine-readable output of a chaos drill.
type DrillReport struct {
	StartedAt     time.Time       `json:"started_at"`
	FinishedAt    time.Time       `json:"finished_at"`
	Duration      time.Duration   `json:"duration_ns"`
	DurationHuman string          `json:"duration"`
	Timeout       time.Duration   `json:"timeout_ns"`
	ScenariosRun  int             `json:"scenarios_run"`
	Results       []ScenarioResult `json:"results"`
	Pass          bool            `json:"pass"`
	Verdict       string          `json:"verdict"`
}

// ScenarioResult records the outcome of a single chaos scenario.
type ScenarioResult struct {
	Name     string        `json:"name"`
	Pass     bool          `json:"pass"`
	Duration time.Duration `json:"duration_ns"`
	Error    string        `json:"error,omitempty"`
	Details  string        `json:"details,omitempty"`
}
