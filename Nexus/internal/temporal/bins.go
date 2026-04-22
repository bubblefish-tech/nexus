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

// Package temporal provides human-scale time binning for memories.
package temporal

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Bin boundaries in ascending duration order.
var binThresholds = [10]time.Duration{
	time.Hour,            // bin 0: last hour
	24 * time.Hour,       // bin 1: today
	48 * time.Hour,       // bin 2: yesterday
	7 * 24 * time.Hour,   // bin 3: this week
	14 * 24 * time.Hour,  // bin 4: last week
	30 * 24 * time.Hour,  // bin 5: this month
	60 * 24 * time.Hour,  // bin 6: last month
	90 * 24 * time.Hour,  // bin 7: this quarter
	365 * 24 * time.Hour, // bin 8: this year
	730 * 24 * time.Hour, // bin 9: last year
}

var binLabels = [11]string{
	"last hour", "today", "yesterday", "this week",
	"last week", "this month", "last month", "this quarter",
	"this year", "last year", "older",
}

// BinLabel returns the human-readable label for a temporal bin.
func BinLabel(bin int) string {
	if bin < 0 || bin > 10 {
		return "unknown"
	}
	return binLabels[bin]
}

// ComputeBin returns the temporal bin for a given timestamp relative to now.
func ComputeBin(t time.Time, now time.Time) int {
	diff := now.Sub(t)
	for i, threshold := range binThresholds {
		if diff < threshold {
			return i
		}
	}
	return 10
}

// HumanRelativeTime returns a natural language description of how long ago
// a timestamp was relative to now.
func HumanRelativeTime(t time.Time, now time.Time) string {
	diff := now.Sub(t)
	switch {
	case diff < time.Minute:
		return "just now"
	case diff < time.Hour:
		mins := int(diff.Minutes())
		if mins == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", mins)
	case diff < 2*time.Hour:
		return "about an hour ago"
	case diff < 24*time.Hour:
		return fmt.Sprintf("%d hours ago", int(diff.Hours()))
	case diff < 48*time.Hour:
		return "yesterday"
	case diff < 7*24*time.Hour:
		return fmt.Sprintf("%d days ago", int(diff.Hours()/24))
	case diff < 14*24*time.Hour:
		return "last week"
	case diff < 30*24*time.Hour:
		return fmt.Sprintf("%d weeks ago", int(diff.Hours()/(24*7)))
	case diff < 60*24*time.Hour:
		return "last month"
	case diff < 365*24*time.Hour:
		return fmt.Sprintf("%d months ago", int(diff.Hours()/(24*30)))
	case diff < 730*24*time.Hour:
		return "last year"
	default:
		return fmt.Sprintf("%d years ago", int(diff.Hours()/(24*365)))
	}
}

// RefreshBins updates all temporal bins whose value has become stale.
// Only rows whose bin would change are updated (WHERE filter).
func RefreshBins(ctx context.Context, db *sql.DB) error {
	now := time.Now().UTC()
	thresholds := make([]interface{}, 0, 20)
	for i := 0; i < 2; i++ {
		for _, d := range binThresholds {
			thresholds = append(thresholds, now.Add(-d).Format("2006-01-02T15:04:05.999999999Z"))
		}
	}
	_, err := db.ExecContext(ctx, `
		UPDATE memories SET temporal_bin = CASE
			WHEN timestamp > ? THEN 0
			WHEN timestamp > ? THEN 1
			WHEN timestamp > ? THEN 2
			WHEN timestamp > ? THEN 3
			WHEN timestamp > ? THEN 4
			WHEN timestamp > ? THEN 5
			WHEN timestamp > ? THEN 6
			WHEN timestamp > ? THEN 7
			WHEN timestamp > ? THEN 8
			WHEN timestamp > ? THEN 9
			ELSE 10
		END
		WHERE temporal_bin != CASE
			WHEN timestamp > ? THEN 0
			WHEN timestamp > ? THEN 1
			WHEN timestamp > ? THEN 2
			WHEN timestamp > ? THEN 3
			WHEN timestamp > ? THEN 4
			WHEN timestamp > ? THEN 5
			WHEN timestamp > ? THEN 6
			WHEN timestamp > ? THEN 7
			WHEN timestamp > ? THEN 8
			WHEN timestamp > ? THEN 9
			ELSE 10
		END`, thresholds...)
	return err
}
