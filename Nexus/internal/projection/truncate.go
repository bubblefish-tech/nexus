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

import "unicode/utf8"

// TruncateOnWordBoundary shortens s so that its UTF-8 byte length does not
// exceed maxBytes. Truncation always ends on a word boundary:
//
//   - If the byte at position maxBytes is a space (i.e. the cut already falls
//     between two words), the preceding word is returned intact.
//   - Otherwise the last space before maxBytes is found and the string is
//     truncated there.
//   - If no space exists before the cut, the string is truncated at the
//     rune-safe cut point (never inside a multi-byte sequence).
//
// Returns the (possibly shortened) string and a boolean that is true when
// truncation occurred. If maxBytes <= 0, the empty string is returned with
// truncated = (len(s) > 0).
//
// Reference: Tech Spec Section 9.3, Phase 2 Behavioral Contract item 2.
func TruncateOnWordBoundary(s string, maxBytes int) (string, bool) {
	if maxBytes <= 0 {
		return "", len(s) > 0
	}
	if len(s) <= maxBytes {
		return s, false
	}

	// Walk back from maxBytes to the start of a valid UTF-8 rune boundary so
	// we never split inside a multi-byte sequence.
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}

	// If the character exactly at the cut point is a space, the text before
	// it is already a complete word — return it with trailing spaces stripped.
	if cut < len(s) && s[cut] == ' ' {
		end := cut
		for end > 0 && s[end-1] == ' ' {
			end--
		}
		return s[:end], end < len(s)
	}

	// The cut lands inside a word. Find the last space strictly before cut.
	idx := -1
	for i := cut - 1; i >= 0; i-- {
		if s[i] == ' ' {
			idx = i
			break
		}
	}

	if idx >= 0 {
		// Trim any trailing spaces that precede the boundary.
		end := idx
		for end > 0 && s[end-1] == ' ' {
			end--
		}
		return s[:end], true
	}

	// No space found anywhere before the cut — the content is one long
	// unbreakable token. Return at the rune-safe cut point.
	if cut == 0 {
		return "", true
	}
	return s[:cut], true
}
