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

// Package immune provides Tier-0 content scanning for prompt-injection and
// abuse patterns. All 12 heuristic rules run in pure Go with no model
// dependencies, targeting sub-millisecond evaluation per check.
package immune

import (
	"github.com/bubblefish-tech/nexus/internal/immune/rules"
)

// ScanResult is the outcome of an immune scanner check.
type ScanResult struct {
	Action            string // "accept", "quarantine", "reject", "flag"
	Rule              string // rule ID that triggered; empty when Action is "accept"
	Details           string // human-readable description of the match
	NormalizedContent string // homoglyph-normalized content; non-empty only for T0-006
}

// Config holds tunable parameters for the Scanner.
type Config struct {
	// EmbeddingDim is the expected embedding vector length. When non-zero,
	// writes whose embedding length differs are rejected (T0-008).
	EmbeddingDim int
}

// Scanner runs heuristic Tier-0 rules against content and orchestration results.
type Scanner struct {
	cfg Config
}

// New returns a ready-to-use Scanner with default configuration.
func New() *Scanner { return &Scanner{} }

// NewWithConfig returns a Scanner configured with cfg.
func NewWithConfig(cfg Config) *Scanner { return &Scanner{cfg: cfg} }

// ScanWrite checks content and its embedding before commitment to the memory
// store. Returns a non-accept result on the first rule violation.
func (s *Scanner) ScanWrite(content string, metadata map[string]any, embedding []float32) ScanResult {
	r := rules.ScanContent(content, metadata, embedding, s.cfg.EmbeddingDim)
	return ScanResult{
		Action:            r.Action,
		Rule:              r.Rule,
		Details:           r.Details,
		NormalizedContent: r.NormalizedContent,
	}
}

// ScanOrchestrationResult checks a result returned by a remote agent before
// it is delivered to the calling agent. Applies all content-level Tier-0
// rules; embedding checks (T0-008, T0-009) are skipped since results carry
// no embedding vector.
func (s *Scanner) ScanOrchestrationResult(agentID, result string) ScanResult {
	r := rules.ScanContent(result, nil, nil, 0)
	return ScanResult{
		Action:  r.Action,
		Rule:    r.Rule,
		Details: r.Details,
	}
}
