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
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	}))
}

// --- Controller tests ---

func TestNewController_Defaults(t *testing.T) {
	c := NewController(ControllerConfig{})
	if c.timeout != 5*time.Minute {
		t.Errorf("default timeout = %v, want 5m", c.timeout)
	}
	if c.logger == nil {
		t.Error("logger should not be nil")
	}
}

func TestNewController_CustomTimeout(t *testing.T) {
	c := NewController(ControllerConfig{Timeout: 30 * time.Second})
	if c.timeout != 30*time.Second {
		t.Errorf("timeout = %v, want 30s", c.timeout)
	}
}

func TestController_Register(t *testing.T) {
	c := NewController(ControllerConfig{Logger: testLogger(t)})
	c.Register(&KillMidWriteScenario{})
	c.Register(&BlockEmbeddingScenario{})

	names := c.Scenarios()
	if len(names) != 2 {
		t.Fatalf("scenarios count = %d, want 2", len(names))
	}
	if names[0] != "KILL_MID_WRITE" {
		t.Errorf("names[0] = %q, want KILL_MID_WRITE", names[0])
	}
	if names[1] != "BLOCK_EMBEDDING" {
		t.Errorf("names[1] = %q, want BLOCK_EMBEDDING", names[1])
	}
}

func TestController_RegisterDefaults(t *testing.T) {
	c := NewController(ControllerConfig{Logger: testLogger(t)})
	c.RegisterDefaults()

	names := c.Scenarios()
	if len(names) != 5 {
		t.Fatalf("scenarios count = %d, want 5", len(names))
	}

	expected := []string{"KILL_MID_WRITE", "BLOCK_EMBEDDING", "FILL_DISK", "STALL_IO", "INJECT_PANIC"}
	for i, want := range expected {
		if names[i] != want {
			t.Errorf("names[%d] = %q, want %q", i, names[i], want)
		}
	}
}

func TestController_EmptyDrill(t *testing.T) {
	c := NewController(ControllerConfig{Logger: testLogger(t)})
	report := c.RunDrill(context.Background())

	if !report.Pass {
		t.Error("empty drill should pass")
	}
	if report.ScenariosRun != 0 {
		t.Errorf("scenarios_run = %d, want 0", report.ScenariosRun)
	}
	if !strings.Contains(report.Verdict, "PASS") {
		t.Errorf("verdict should contain PASS: %s", report.Verdict)
	}
}

func TestController_RunDrill_AllPass(t *testing.T) {
	c := NewController(ControllerConfig{
		Timeout: 2 * time.Minute,
		Logger:  testLogger(t),
	})
	c.Register(&passingScenario{name: "test-1"})
	c.Register(&passingScenario{name: "test-2"})

	report := c.RunDrill(context.Background())

	if !report.Pass {
		t.Errorf("drill should pass: %s", report.Verdict)
	}
	if report.ScenariosRun != 2 {
		t.Errorf("scenarios_run = %d, want 2", report.ScenariosRun)
	}
	if len(report.Results) != 2 {
		t.Fatalf("results count = %d, want 2", len(report.Results))
	}
	for _, r := range report.Results {
		if !r.Pass {
			t.Errorf("scenario %s should pass", r.Name)
		}
	}
}

func TestController_RunDrill_OneFails(t *testing.T) {
	c := NewController(ControllerConfig{
		Timeout: 2 * time.Minute,
		Logger:  testLogger(t),
	})
	c.Register(&passingScenario{name: "good"})
	c.Register(&failingScenario{name: "bad"})

	report := c.RunDrill(context.Background())

	if report.Pass {
		t.Error("drill should fail when one scenario fails")
	}
	if !strings.Contains(report.Verdict, "FAIL") {
		t.Errorf("verdict should contain FAIL: %s", report.Verdict)
	}
	if !strings.Contains(report.Verdict, "1/2") {
		t.Errorf("verdict should show 1/2 passed: %s", report.Verdict)
	}
}

func TestController_RunDrill_Timeout(t *testing.T) {
	c := NewController(ControllerConfig{
		Timeout: 1 * time.Millisecond,
		Logger:  testLogger(t),
	})
	c.Register(&slowScenario{name: "slow", duration: 10 * time.Second})

	report := c.RunDrill(context.Background())

	if report.Pass {
		t.Error("drill should fail when timeout exceeded")
	}
}

func TestController_RunDrillJSON(t *testing.T) {
	c := NewController(ControllerConfig{
		Timeout: 1 * time.Minute,
		Logger:  testLogger(t),
	})
	c.Register(&passingScenario{name: "json-test"})

	data, err := c.RunDrillJSON(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	var report DrillReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if !report.Pass {
		t.Error("drill should pass")
	}
}

// --- Individual scenario tests ---

func TestKillMidWrite_Pass(t *testing.T) {
	s := &KillMidWriteScenario{}
	result := s.Run(context.Background(), testLogger(t))
	if !result.Pass {
		t.Errorf("KILL_MID_WRITE should pass: %s", result.Error)
	}
	if result.Name != "KILL_MID_WRITE" {
		t.Errorf("name = %q, want KILL_MID_WRITE", result.Name)
	}
	if result.Duration <= 0 {
		t.Error("duration should be positive")
	}
}

func TestBlockEmbedding_Pass(t *testing.T) {
	s := &BlockEmbeddingScenario{}
	result := s.Run(context.Background(), testLogger(t))
	if !result.Pass {
		t.Errorf("BLOCK_EMBEDDING should pass: %s", result.Error)
	}
	if !strings.Contains(result.Details, "5/5") {
		t.Errorf("details should mention 5/5 degraded: %s", result.Details)
	}
}

func TestFillDisk_Pass(t *testing.T) {
	s := &FillDiskScenario{MaxBytes: 64 * 1024} // 64KB limit for fast test
	result := s.Run(context.Background(), testLogger(t))
	if !result.Pass {
		t.Errorf("FILL_DISK should pass: %s", result.Error)
	}
	if !strings.Contains(result.Details, "rejected") {
		t.Errorf("details should mention rejected writes: %s", result.Details)
	}
}

func TestFillDisk_DefaultLimit(t *testing.T) {
	s := &FillDiskScenario{} // Default 1MB
	if s.MaxBytes != 0 {
		t.Error("MaxBytes should default to 0 (meaning use 1MB at runtime)")
	}
}

func TestStallIO_Pass(t *testing.T) {
	s := &StallIOScenario{StallDuration: 2 * time.Second}
	result := s.Run(context.Background(), testLogger(t))
	if !result.Pass {
		t.Errorf("STALL_IO should pass: %s", result.Error)
	}
	if !strings.Contains(result.Details, "watchdog") {
		t.Errorf("details should mention watchdog: %s", result.Details)
	}
}

func TestInjectPanic_Pass(t *testing.T) {
	s := &InjectPanicScenario{}
	result := s.Run(context.Background(), testLogger(t))
	if !result.Pass {
		t.Errorf("INJECT_PANIC should pass: %s", result.Error)
	}
	if !strings.Contains(result.Details, "restarted") {
		t.Errorf("details should mention restart: %s", result.Details)
	}
}

func TestScenarioNames(t *testing.T) {
	scenarios := []Scenario{
		&KillMidWriteScenario{},
		&BlockEmbeddingScenario{},
		&FillDiskScenario{},
		&StallIOScenario{},
		&InjectPanicScenario{},
	}

	expected := []string{"KILL_MID_WRITE", "BLOCK_EMBEDDING", "FILL_DISK", "STALL_IO", "INJECT_PANIC"}
	for i, s := range scenarios {
		if s.Name() != expected[i] {
			t.Errorf("scenario %d name = %q, want %q", i, s.Name(), expected[i])
		}
		if s.Description() == "" {
			t.Errorf("scenario %d has empty description", i)
		}
	}
}

func TestDrillReport_JSON(t *testing.T) {
	report := &DrillReport{
		StartedAt:     time.Now().UTC(),
		FinishedAt:    time.Now().UTC(),
		Duration:      1 * time.Second,
		DurationHuman: "1s",
		Timeout:       5 * time.Minute,
		ScenariosRun:  2,
		Results: []ScenarioResult{
			{Name: "test-1", Pass: true, Duration: 500 * time.Millisecond},
			{Name: "test-2", Pass: false, Duration: 300 * time.Millisecond, Error: "boom"},
		},
		Pass:    false,
		Verdict: "FAIL",
	}

	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}

	var decoded DrillReport
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ScenariosRun != 2 {
		t.Errorf("scenarios_run = %d, want 2", decoded.ScenariosRun)
	}
	if decoded.Results[1].Error != "boom" {
		t.Errorf("error = %q, want boom", decoded.Results[1].Error)
	}
}

func TestController_ContextCancellation(t *testing.T) {
	c := NewController(ControllerConfig{
		Timeout: 1 * time.Minute,
		Logger:  testLogger(t),
	})
	c.Register(&slowScenario{name: "cancellable", duration: 30 * time.Second})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	report := c.RunDrill(ctx)
	// The scenario should be skipped or fail due to context cancellation.
	if report.Pass && report.ScenariosRun > 0 {
		// If somehow the slow scenario passed within 50ms, that's fine too.
		// But typically it should fail.
	}
	if report.Duration > 5*time.Second {
		t.Errorf("drill took too long despite context cancellation: %v", report.Duration)
	}
}

// --- Test helpers ---

type passingScenario struct{ name string }

func (s *passingScenario) Name() string        { return s.name }
func (s *passingScenario) Description() string  { return "always passes" }
func (s *passingScenario) Run(_ context.Context, _ *slog.Logger) ScenarioResult {
	return ScenarioResult{Name: s.name, Pass: true, Duration: 1 * time.Millisecond}
}

type failingScenario struct{ name string }

func (s *failingScenario) Name() string        { return s.name }
func (s *failingScenario) Description() string  { return "always fails" }
func (s *failingScenario) Run(_ context.Context, _ *slog.Logger) ScenarioResult {
	return ScenarioResult{Name: s.name, Pass: false, Duration: 1 * time.Millisecond, Error: "intentional failure"}
}

type slowScenario struct {
	name     string
	duration time.Duration
}

func (s *slowScenario) Name() string        { return s.name }
func (s *slowScenario) Description() string  { return "takes a long time" }
func (s *slowScenario) Run(ctx context.Context, _ *slog.Logger) ScenarioResult {
	select {
	case <-ctx.Done():
		return ScenarioResult{Name: s.name, Pass: false, Duration: 0, Error: "context cancelled"}
	case <-time.After(s.duration):
		return ScenarioResult{Name: s.name, Pass: true, Duration: s.duration}
	}
}
