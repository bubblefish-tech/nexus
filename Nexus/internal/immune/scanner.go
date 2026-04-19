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

// Package immune provides content scanning for prompt-injection and abuse
// patterns. Tier-0 rules (12 heuristic checks) are added in DEF.1; this
// stub always accepts so the orchestration engine can reference the interface.
package immune

// ScanResult is the outcome of an immune scanner check.
type ScanResult struct {
	Action  string // "accept", "quarantine", "reject", "flag"
	Rule    string // rule ID that triggered; empty when Action is "accept"
	Details string // human-readable description of the match
}

// Scanner runs heuristic rules against content and orchestration results.
// The full 12-rule Tier-0 implementation is added in DEF.1.
type Scanner struct{}

// New returns a ready-to-use Scanner.
func New() *Scanner { return &Scanner{} }

// ScanOrchestrationResult checks a result returned by a remote agent before
// it is delivered to the calling agent. Stub: always accepts until DEF.1.
func (s *Scanner) ScanOrchestrationResult(agentID, result string) ScanResult {
	return ScanResult{Action: "accept"}
}

// ScanWrite checks content and its embedding before commitment to the memory
// store. Stub: always accepts until DEF.1.
func (s *Scanner) ScanWrite(content string, metadata map[string]any, embedding []float32) ScanResult {
	return ScanResult{Action: "accept"}
}
