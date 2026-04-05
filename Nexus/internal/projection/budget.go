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
	"bytes"
	"encoding/json"
	"sync"
)

// bufPool recycles byte buffers used by serialization to measure response size.
var bufPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

// EncodedSize returns the byte length of the JSON encoding of v.
// It uses a pooled buffer to avoid allocations on the hot path.
func EncodedSize(v any) int {
	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufPool.Put(buf)

	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		// Encoding failure is not expected for well-formed projection output;
		// returning 0 causes the budget check to pass, which is safe: the
		// caller will catch the encode error at serialization time.
		return 0
	}
	return buf.Len()
}

// FitBudget applies the byte-budget constraint to a slice of projected record
// maps. It measures the serialized JSON size of the full records slice; if
// that size exceeds maxBytes it truncates the "content" field of each record
// on word boundaries (last record first) until the response fits.
//
// maxBytes <= 0 disables the budget check (no truncation).
//
// Returns the (possibly mutated) records slice and a boolean that is true
// when any truncation occurred. The input slice is modified in place; callers
// that need the originals must copy before calling.
//
// Reference: Tech Spec Section 9.3 (max_response_bytes), Phase 2 Behavioral
// Contract item 2.
func FitBudget(records []map[string]any, maxBytes int) ([]map[string]any, bool) {
	if maxBytes <= 0 || len(records) == 0 {
		return records, false
	}

	if EncodedSize(records) <= maxBytes {
		return records, false
	}

	truncated := false

	// Work from the last record backward. For each record, repeatedly halve
	// the content field (on a word boundary) until the total fits or the
	// content is exhausted.
	for i := len(records) - 1; i >= 0; i-- {
		content, ok := records[i]["content"].(string)
		if !ok || content == "" {
			continue
		}

		// Binary-search-style reduction: keep halving the allowed content
		// length until the serialized response fits within the budget.
		allowedBytes := len(content)
		for EncodedSize(records) > maxBytes && allowedBytes > 0 {
			allowedBytes /= 2
			short, didTrunc := TruncateOnWordBoundary(content, allowedBytes)
			if didTrunc || short != content {
				records[i]["content"] = short
				content = short
				truncated = true
			}
		}

		// If we've already shrunk the response enough, stop.
		if EncodedSize(records) <= maxBytes {
			break
		}
	}

	return records, truncated
}
