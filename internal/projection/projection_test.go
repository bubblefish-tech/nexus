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

package projection_test

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/bubblefish-tech/nexus/internal/destination"
	"github.com/bubblefish-tech/nexus/internal/policy"
	"github.com/bubblefish-tech/nexus/internal/projection"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func makePayload(content string) destination.TranslatedPayload {
	return destination.TranslatedPayload{
		PayloadID:   "pid-1",
		RequestID:   "rid-1",
		Source:      "claude",
		Subject:     "user:shawn",
		Namespace:   "default",
		Destination: "sqlite",
		Content:     content,
		Model:       "claude-3",
		Role:        "user",
		Timestamp:   time.Date(2026, 4, 5, 0, 0, 0, 0, time.UTC),
		ActorType:   "user",
		ActorID:     "shawn",
	}
}

func openPolicy() policy.PolicyEntry {
	return policy.PolicyEntry{
		Source:           "claude",
		MaxResponseBytes: 0, // unlimited
		FieldVisibility: policy.FieldVisibilityEntry{
			IncludeFields: nil, // all fields
			StripMetadata: false,
		},
	}
}

// ---------------------------------------------------------------------------
// TruncateOnWordBoundary
// ---------------------------------------------------------------------------

func TestTruncateOnWordBoundary(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		input     string
		maxBytes  int
		wantOut   string
		wantTrunc bool
	}{
		{
			name:      "under budget — no truncation",
			input:     "hello world",
			maxBytes:  100,
			wantOut:   "hello world",
			wantTrunc: false,
		},
		{
			name:      "exact fit — no truncation",
			input:     "hello",
			maxBytes:  5,
			wantOut:   "hello",
			wantTrunc: false,
		},
		{
			name:      "truncate on word boundary",
			input:     "hello world foo",
			maxBytes:  11,
			wantOut:   "hello world",
			wantTrunc: true,
		},
		{
			name:      "truncate mid-word falls back to previous space",
			input:     "hello world foo bar",
			maxBytes:  14,
			wantOut:   "hello world",
			wantTrunc: true,
		},
		{
			name:      "single long token no space",
			input:     "superlongtoken",
			maxBytes:  5,
			wantOut:   "super",
			wantTrunc: true,
		},
		{
			name:      "maxBytes zero returns empty",
			input:     "hello",
			maxBytes:  0,
			wantOut:   "",
			wantTrunc: true,
		},
		{
			name:      "empty string no truncation",
			input:     "",
			maxBytes:  10,
			wantOut:   "",
			wantTrunc: false,
		},
		{
			name:      "multibyte rune boundary respected",
			input:     "café world",
			maxBytes:  6, // "café" is 5 bytes (UTF-8: c=1,a=1,f=1,é=2) → 5 bytes
			wantOut:   "café",
			wantTrunc: true,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, didTrunc := projection.TruncateOnWordBoundary(tc.input, tc.maxBytes)
			if got != tc.wantOut {
				t.Errorf("TruncateOnWordBoundary(%q, %d) = %q; want %q",
					tc.input, tc.maxBytes, got, tc.wantOut)
			}
			if didTrunc != tc.wantTrunc {
				t.Errorf("TruncateOnWordBoundary(%q, %d) truncated=%v; want %v",
					tc.input, tc.maxBytes, didTrunc, tc.wantTrunc)
			}
			// Result must always be valid UTF-8.
			if !utf8.ValidString(got) {
				t.Errorf("TruncateOnWordBoundary returned invalid UTF-8: %q", got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Apply — field allowlist
// ---------------------------------------------------------------------------

func TestApply_FieldAllowlist(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		includeFields []string
		wantPresent   []string
		wantAbsent    []string
	}{
		{
			name:          "empty allowlist retains all fields",
			includeFields: nil,
			wantPresent:   []string{"content", "source", "role", "payload_id"},
			wantAbsent:    nil,
		},
		{
			name:          "allowlist with content and source only",
			includeFields: []string{"content", "source"},
			wantPresent:   []string{"content", "source"},
			wantAbsent:    []string{"role", "payload_id", "model", "actor_type"},
		},
		{
			name:          "allowlist with all spec-documented fields",
			includeFields: []string{"content", "source", "role", "timestamp", "model", "actor_type", "actor_id"},
			wantPresent:   []string{"content", "source", "role", "timestamp", "model", "actor_type", "actor_id"},
			wantAbsent:    []string{"payload_id", "request_id", "namespace", "destination"},
		},
		{
			name:          "allowlist with single field",
			includeFields: []string{"content"},
			wantPresent:   []string{"content"},
			wantAbsent:    []string{"source", "role", "model"},
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pol := openPolicy()
			pol.FieldVisibility.IncludeFields = tc.includeFields

			resp := projection.Apply(
				[]destination.TranslatedPayload{makePayload("hello world")},
				pol,
				projection.NexusMetadata{Stage: "structured", Profile: "balanced"},
			)

			if len(resp.Records) != 1 {
				t.Fatalf("expected 1 record, got %d", len(resp.Records))
			}
			rec := resp.Records[0]
			for _, field := range tc.wantPresent {
				if _, ok := rec[field]; !ok {
					t.Errorf("expected field %q to be present in record", field)
				}
			}
			for _, field := range tc.wantAbsent {
				if _, ok := rec[field]; ok {
					t.Errorf("expected field %q to be absent from record, but it is present", field)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Apply — byte budget truncation
// ---------------------------------------------------------------------------

func TestApply_ByteBudget(t *testing.T) {
	t.Parallel()

	// Build a content string well over 100 bytes with word boundaries.
	longContent := strings.Repeat("word ", 50) // 250 bytes

	pol := openPolicy()
	pol.MaxResponseBytes = 100 // tight budget

	resp := projection.Apply(
		[]destination.TranslatedPayload{makePayload(longContent)},
		pol,
		projection.NexusMetadata{Stage: "structured", Profile: "balanced"},
	)

	if len(resp.Records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(resp.Records))
	}

	// _nexus.truncated must be true.
	if resp.Nexus == nil {
		t.Fatal("_nexus block missing")
	}
	if !resp.Nexus.Truncated {
		t.Error("_nexus.truncated should be true when byte budget exceeded")
	}

	// The returned content must be shorter than the original.
	gotContent, ok := resp.Records[0]["content"].(string)
	if !ok {
		t.Fatal("content field missing or wrong type")
	}
	if len(gotContent) >= len(longContent) {
		t.Errorf("content was not truncated: len=%d", len(gotContent))
	}

	// Truncated content must be valid UTF-8.
	if !utf8.ValidString(gotContent) {
		t.Error("truncated content is invalid UTF-8")
	}
}

func TestApply_NoBudget_NoTruncation(t *testing.T) {
	t.Parallel()
	content := strings.Repeat("word ", 100)
	pol := openPolicy()
	pol.MaxResponseBytes = 0 // unlimited

	resp := projection.Apply(
		[]destination.TranslatedPayload{makePayload(content)},
		pol,
		projection.NexusMetadata{Stage: "structured"},
	)
	if resp.Nexus != nil && resp.Nexus.Truncated {
		t.Error("_nexus.truncated should be false when no budget is set")
	}
	got := resp.Records[0]["content"].(string)
	if got != content {
		t.Error("content should be unchanged when no budget is set")
	}
}

// ---------------------------------------------------------------------------
// Apply — _nexus metadata injection and strip_metadata
// ---------------------------------------------------------------------------

func TestApply_NexusPresent_WhenStripFalse(t *testing.T) {
	t.Parallel()
	pol := openPolicy()
	pol.FieldVisibility.StripMetadata = false

	meta := projection.NexusMetadata{
		Stage:            "structured",
		ResultCount:      0, // overwritten by Apply
		SemanticUnavailable: true,
		SemanticUnavailableReason: "no embedding provider configured",
		HasMore:          true,
		NextCursor:       "dGVzdA==",
		TemporalDecayApplied: false,
		ConsistencyScore: 0.95,
		Profile:          "balanced",
	}

	resp := projection.Apply(
		[]destination.TranslatedPayload{makePayload("hello")},
		pol,
		meta,
	)

	if resp.Nexus == nil {
		t.Fatal("_nexus block should be present when strip_metadata = false")
	}
	n := resp.Nexus
	if n.Stage != "structured" {
		t.Errorf("_nexus.stage = %q; want %q", n.Stage, "structured")
	}
	if !n.SemanticUnavailable {
		t.Error("_nexus.semantic_unavailable should be true")
	}
	if n.SemanticUnavailableReason != "no embedding provider configured" {
		t.Errorf("_nexus.semantic_unavailable_reason = %q", n.SemanticUnavailableReason)
	}
	if n.ResultCount != 1 {
		t.Errorf("_nexus.result_count = %d; want 1", n.ResultCount)
	}
	if !n.HasMore {
		t.Error("_nexus.has_more should be true")
	}
	if n.NextCursor != "dGVzdA==" {
		t.Errorf("_nexus.next_cursor = %q", n.NextCursor)
	}
	if n.Profile != "balanced" {
		t.Errorf("_nexus.profile = %q; want balanced", n.Profile)
	}
	if n.ConsistencyScore != 0.95 {
		t.Errorf("_nexus.consistency_score = %f; want 0.95", n.ConsistencyScore)
	}
}

func TestApply_NexusAbsent_WhenStripTrue(t *testing.T) {
	t.Parallel()
	pol := openPolicy()
	pol.FieldVisibility.StripMetadata = true

	resp := projection.Apply(
		[]destination.TranslatedPayload{makePayload("hello")},
		pol,
		projection.NexusMetadata{Stage: "structured", Profile: "fast"},
	)

	if resp.Nexus != nil {
		t.Error("_nexus block should be absent when strip_metadata = true")
	}
}

// ---------------------------------------------------------------------------
// Apply — result_count set correctly
// ---------------------------------------------------------------------------

func TestApply_ResultCount(t *testing.T) {
	t.Parallel()
	pol := openPolicy()

	payloads := []destination.TranslatedPayload{
		makePayload("one"),
		makePayload("two"),
		makePayload("three"),
	}
	// Assign distinct PayloadIDs.
	payloads[1].PayloadID = "pid-2"
	payloads[2].PayloadID = "pid-3"

	resp := projection.Apply(payloads, pol, projection.NexusMetadata{})
	if resp.Nexus == nil {
		t.Fatal("_nexus missing")
	}
	if resp.Nexus.ResultCount != 3 {
		t.Errorf("_nexus.result_count = %d; want 3", resp.Nexus.ResultCount)
	}
	if len(resp.Records) != 3 {
		t.Errorf("records count = %d; want 3", len(resp.Records))
	}
}

// ---------------------------------------------------------------------------
// Apply — empty records
// ---------------------------------------------------------------------------

func TestApply_EmptyRecords(t *testing.T) {
	t.Parallel()
	pol := openPolicy()
	resp := projection.Apply(nil, pol, projection.NexusMetadata{Stage: "structured"})
	if len(resp.Records) != 0 {
		t.Errorf("expected 0 records, got %d", len(resp.Records))
	}
	if resp.Nexus != nil && resp.Nexus.ResultCount != 0 {
		t.Errorf("result_count should be 0 for empty input, got %d", resp.Nexus.ResultCount)
	}
}

// ---------------------------------------------------------------------------
// FitBudget — concurrency safety
// ---------------------------------------------------------------------------

func TestFitBudget_Concurrency(t *testing.T) {
	t.Parallel()
	content := strings.Repeat("hello world ", 200)

	// 50 goroutines each calling FitBudget with independent copies.
	done := make(chan struct{})
	for i := 0; i < 50; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			records := []map[string]any{{"content": content}}
			projection.FitBudget(records, 200)
		}()
	}
	for i := 0; i < 50; i++ {
		<-done
	}
}
