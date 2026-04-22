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

package query

import "strings"

// ExtractTemporalHint detects temporal language in a query and returns
// the corresponding bin, or -1 if no temporal hint is found.
func ExtractTemporalHint(query string) int {
	q := strings.ToLower(query)

	patterns := []struct {
		keywords []string
		bin      int
	}{
		{[]string{"just now", "a moment ago", "seconds ago"}, 0},
		{[]string{"today", "this morning", "this afternoon", "this evening", "earlier today"}, 1},
		{[]string{"yesterday"}, 2},
		{[]string{"this week", "few days ago", "other day"}, 3},
		{[]string{"last week"}, 4},
		{[]string{"this month", "few weeks ago", "recently"}, 5},
		{[]string{"last month"}, 6},
		{[]string{"this quarter", "few months ago"}, 7},
		{[]string{"this year", "earlier this year"}, 8},
		{[]string{"last year"}, 9},
		{[]string{"years ago", "long ago", "a while back"}, 10},
	}

	for _, p := range patterns {
		for _, kw := range p.keywords {
			if strings.Contains(q, kw) {
				return p.bin
			}
		}
	}
	return -1
}
