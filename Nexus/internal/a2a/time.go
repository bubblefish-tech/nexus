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

package a2a

import "time"

// TimeFormat is the canonical NA2A timestamp format: RFC 3339 with
// millisecond precision in UTC. All timestamps in NA2A messages and
// tasks MUST use this format.
const TimeFormat = "2006-01-02T15:04:05.000Z"

// FormatTime formats a time.Time as an NA2A timestamp string.
func FormatTime(t time.Time) string {
	return t.UTC().Format(TimeFormat)
}

// ParseTime parses an NA2A timestamp string into a time.Time.
func ParseTime(s string) (time.Time, error) {
	t, err := time.Parse(TimeFormat, s)
	if err != nil {
		// Try standard RFC 3339 as a fallback for interop with
		// non-Nexus agents that may omit milliseconds.
		t, err = time.Parse(time.RFC3339, s)
		if err != nil {
			return time.Time{}, NewErrorf(CodeInvalidParams, "invalid timestamp %q: expected RFC 3339 with ms precision", s)
		}
	}
	return t.UTC(), nil
}

// Now returns the current time formatted as an NA2A timestamp.
func Now() string {
	return FormatTime(time.Now())
}
