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

package immune_test

import (
	"testing"

	"github.com/bubblefish-tech/nexus/internal/immune"
)

func TestScanner_ScanOrchestrationResult_AlwaysAccepts(t *testing.T) {
	t.Helper()
	s := immune.New()
	r := s.ScanOrchestrationResult("agent-1", "some orchestration result")
	if r.Action != "accept" {
		t.Fatalf("expected Action=accept, got %q", r.Action)
	}
	if r.Rule != "" {
		t.Fatalf("expected empty Rule, got %q", r.Rule)
	}
}

func TestScanner_ScanOrchestrationResult_EmptyInputs(t *testing.T) {
	t.Helper()
	s := immune.New()
	r := s.ScanOrchestrationResult("", "")
	if r.Action != "accept" {
		t.Fatalf("empty inputs: expected accept, got %q", r.Action)
	}
}

func TestScanner_ScanWrite_AlwaysAccepts(t *testing.T) {
	t.Helper()
	s := immune.New()
	r := s.ScanWrite("hello world", nil, nil)
	if r.Action != "accept" {
		t.Fatalf("expected Action=accept, got %q", r.Action)
	}
}

func TestScanner_ScanWrite_WithEmbedding(t *testing.T) {
	t.Helper()
	s := immune.New()
	emb := []float32{0.1, 0.2, 0.3}
	r := s.ScanWrite("content", map[string]any{"lang": "en"}, emb)
	if r.Action != "accept" {
		t.Fatalf("expected accept with embedding, got %q", r.Action)
	}
}

func TestScanner_ZeroValue(t *testing.T) {
	t.Helper()
	var s immune.Scanner
	r := s.ScanOrchestrationResult("agent-x", "result")
	if r.Action != "accept" {
		t.Fatalf("zero-value Scanner: expected accept, got %q", r.Action)
	}
}

func TestScanResult_FieldsRoundTrip(t *testing.T) {
	t.Helper()
	r := immune.ScanResult{
		Action:  "quarantine",
		Rule:    "T0-001",
		Details: "prompt injection detected",
	}
	if r.Action != "quarantine" {
		t.Errorf("Action: got %q", r.Action)
	}
	if r.Rule != "T0-001" {
		t.Errorf("Rule: got %q", r.Rule)
	}
	if r.Details != "prompt injection detected" {
		t.Errorf("Details: got %q", r.Details)
	}
}
