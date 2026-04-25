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

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Scenario is a single chaos fault injection test.
type Scenario interface {
	// Name returns a short identifier for this scenario (e.g. "KILL_MID_WRITE").
	Name() string

	// Description returns a human-readable explanation of what the scenario tests.
	Description() string

	// Run executes the scenario and returns a ScenarioResult.
	// The context carries the overall drill deadline.
	Run(ctx context.Context, logger *slog.Logger) ScenarioResult
}

// ChaosController orchestrates a sequence of chaos scenarios.
// It runs each registered scenario sequentially and produces a DrillReport.
type ChaosController struct {
	mu        sync.Mutex
	scenarios []Scenario
	logger    *slog.Logger
	timeout   time.Duration
}

// ControllerConfig configures the ChaosController.
type ControllerConfig struct {
	// Timeout is the maximum time for the entire drill. Default: 5 minutes.
	Timeout time.Duration

	// Logger for structured output.
	Logger *slog.Logger
}

// NewController creates a ChaosController with the given config.
func NewController(cfg ControllerConfig) *ChaosController {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Minute
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &ChaosController{
		logger:  cfg.Logger,
		timeout: cfg.Timeout,
	}
}

// Register adds a scenario to the drill. Scenarios execute in registration order.
func (c *ChaosController) Register(s Scenario) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.scenarios = append(c.scenarios, s)
}

// Scenarios returns the names of all registered scenarios.
func (c *ChaosController) Scenarios() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	names := make([]string, len(c.scenarios))
	for i, s := range c.scenarios {
		names[i] = s.Name()
	}
	return names
}

// RunDrill executes all registered scenarios sequentially within the timeout.
// Returns a DrillReport with per-scenario results.
func (c *ChaosController) RunDrill(ctx context.Context) *DrillReport {
	c.mu.Lock()
	scenarios := make([]Scenario, len(c.scenarios))
	copy(scenarios, c.scenarios)
	c.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	report := &DrillReport{
		StartedAt: time.Now().UTC(),
		Timeout:   c.timeout,
		Results:   make([]ScenarioResult, 0, len(scenarios)),
	}

	allPass := true
	for _, s := range scenarios {
		if ctx.Err() != nil {
			// Drill timed out — mark remaining scenarios as skipped.
			report.Results = append(report.Results, ScenarioResult{
				Name:     s.Name(),
				Pass:     false,
				Duration: 0,
				Error:    "skipped: drill timeout exceeded",
			})
			allPass = false
			continue
		}

		c.logger.Info("chaos drill: starting scenario",
			"component", "chaos",
			"scenario", s.Name(),
		)

		result := s.Run(ctx, c.logger)
		report.Results = append(report.Results, result)

		if !result.Pass {
			allPass = false
		}

		c.logger.Info("chaos drill: scenario complete",
			"component", "chaos",
			"scenario", s.Name(),
			"pass", result.Pass,
			"duration", result.Duration,
		)
	}

	report.FinishedAt = time.Now().UTC()
	report.Duration = report.FinishedAt.Sub(report.StartedAt)
	report.DurationHuman = report.Duration.Round(time.Millisecond).String()
	report.ScenariosRun = len(scenarios)
	report.Pass = allPass

	if allPass {
		report.Verdict = fmt.Sprintf("PASS — %d/%d scenarios passed in %s",
			len(scenarios), len(scenarios), report.DurationHuman)
	} else {
		passed := 0
		for _, r := range report.Results {
			if r.Pass {
				passed++
			}
		}
		report.Verdict = fmt.Sprintf("FAIL — %d/%d scenarios passed in %s",
			passed, len(scenarios), report.DurationHuman)
	}

	return report
}

// RunDrillJSON executes the drill and returns the report as indented JSON.
func (c *ChaosController) RunDrillJSON(ctx context.Context) ([]byte, error) {
	report := c.RunDrill(ctx)
	return json.MarshalIndent(report, "", "  ")
}

// RegisterDefaults registers all five built-in chaos scenarios.
func (c *ChaosController) RegisterDefaults() {
	c.Register(&KillMidWriteScenario{})
	c.Register(&BlockEmbeddingScenario{})
	c.Register(&FillDiskScenario{})
	c.Register(&StallIOScenario{})
	c.Register(&InjectPanicScenario{})
}
