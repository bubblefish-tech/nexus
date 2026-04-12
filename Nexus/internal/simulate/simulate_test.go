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

package simulate

import (
	"testing"
	"time"
)

func TestSimulateNoFaults(t *testing.T) {
	report, err := Run(Options{
		Seed:        42,
		Duration:    3 * time.Second,
		Concurrency: 2,
		FaultRate:   0, // no faults
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Pass {
		t.Errorf("expected pass with no faults: %s", report.Verdict)
	}
	if report.WritesDelivered == 0 {
		t.Error("expected some writes delivered")
	}
	if report.FaultsInjected != 0 {
		t.Errorf("expected 0 faults, got %d", report.FaultsInjected)
	}
}

func TestSimulateWithFaults(t *testing.T) {
	report, err := Run(Options{
		Seed:        123,
		Duration:    5 * time.Second,
		Concurrency: 3,
		FaultRate:   0.1, // 10% fault rate
	})
	if err != nil {
		t.Fatal(err)
	}
	// With direct writes (WAL + destination), all delivered entries should survive.
	if !report.Pass {
		t.Errorf("expected pass even with faults: %s", report.Verdict)
	}
	if report.WritesDelivered == 0 {
		t.Error("expected some writes delivered")
	}
	if report.FaultsInjected == 0 {
		t.Error("expected some faults injected at 10% rate")
	}
}

func TestSimulateSeedDeterminism(t *testing.T) {
	r1, err := Run(Options{
		Seed:        999,
		Duration:    3 * time.Second,
		Concurrency: 1,
		FaultRate:   0,
	})
	if err != nil {
		t.Fatal(err)
	}

	r2, err := Run(Options{
		Seed:        999,
		Duration:    3 * time.Second,
		Concurrency: 1,
		FaultRate:   0,
	})
	if err != nil {
		t.Fatal(err)
	}

	if r1.Seed != r2.Seed {
		t.Error("seeds should match")
	}
	// With no faults, all delivered writes must survive.
	if !r1.Pass {
		t.Errorf("run 1 should pass: %s (writes=%d, recovered=%d, missing=%d)",
			r1.Verdict, r1.WritesDelivered, r1.RecoveredCount, r1.MissingCount)
	}
	if !r2.Pass {
		t.Errorf("run 2 should pass: %s (writes=%d, recovered=%d, missing=%d)",
			r2.Verdict, r2.WritesDelivered, r2.RecoveredCount, r2.MissingCount)
	}
}
