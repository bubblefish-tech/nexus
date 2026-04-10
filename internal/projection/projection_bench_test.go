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

package projection

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bubblefish-tech/nexus/internal/destination"
	"github.com/bubblefish-tech/nexus/internal/policy"
)

func benchPayload(id, content string) destination.TranslatedPayload {
	return destination.TranslatedPayload{
		PayloadID:   id,
		RequestID:   "rid-" + id,
		Source:      "claude",
		Subject:     "user:bench",
		Namespace:   "default",
		Destination: "sqlite",
		Content:     content,
		Model:       "claude-3",
		Role:        "user",
		Timestamp:   time.Date(2026, 4, 5, 0, 0, 0, 0, time.UTC),
		ActorType:   "user",
		ActorID:     "bench",
	}
}

func benchPolicy() policy.PolicyEntry {
	return policy.PolicyEntry{
		Source:           "claude",
		MaxResponseBytes: 0, // unlimited
		FieldVisibility: policy.FieldVisibilityEntry{
			IncludeFields: nil, // all fields
			StripMetadata: false,
		},
	}
}

// BenchmarkProjection_Stage_End2End runs the full Apply projection against a
// fixture of ~100 memories and measures total time per query.
// Individual stages are not separately exposed through the public API —
// BenchmarkProjection_Stage_Individual is skipped (see below).
func BenchmarkProjection_Stage_End2End(b *testing.B) {
	b.ReportAllocs()

	// Build 100-record fixture.
	records := make([]destination.TranslatedPayload, 100)
	for i := range records {
		records[i] = benchPayload(
			fmt.Sprintf("pid-%d", i),
			fmt.Sprintf("Memory entry %d: %s", i, strings.Repeat("benchmark content ", 5)),
		)
	}

	pol := benchPolicy()
	meta := NexusMetadata{
		Stage:       "structured",
		ResultCount: len(records),
		Profile:     "balanced",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp := Apply(records, pol, meta)
		if len(resp.Records) != 100 {
			b.Fatalf("expected 100 records, got %d", len(resp.Records))
		}
	}
}

// BenchmarkProjection_Stage_Individual is skipped because individual retrieval
// stages are not exposed as separate functions through the public API.
// The 6-stage cascade runs internally within the daemon's Search path;
// projection.Apply is the only public entry point and it handles the
// post-retrieval formatting/filtering step, not the per-stage retrieval.
func BenchmarkProjection_Stage_Individual(b *testing.B) {
	b.Skip("individual retrieval stages are not exposed through the public API — only Apply is public")
}
