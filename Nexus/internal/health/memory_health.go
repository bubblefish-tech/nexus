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

package health

import (
	"database/sql"
	"fmt"
	"time"
)

type MemoryHealth struct {
	ContinuityScore    float64            `json:"continuity_score"`
	TotalMemories7d    int                `json:"total_memories_7d"`
	RetrievableCount   int                `json:"retrievable_count"`
	CrossAgentCoverage CrossAgentStats    `json:"cross_agent_coverage"`
	CrashRecoveries    int                `json:"crash_recoveries"`
	QuarantineCount7d  int                `json:"quarantine_count_7d"`
}

type CrossAgentStats struct {
	WritingAgents  int              `json:"writing_agents"`
	ReadingAgents  int              `json:"reading_agents"`
	AgentBreakdown []AgentActivity  `json:"agent_breakdown"`
}

type AgentActivity struct {
	AgentID    string `json:"agent_id"`
	Writes     int    `json:"writes"`
	Reads      int    `json:"reads"`
	LastActive string `json:"last_active"`
}

func CalculateMemoryHealth(db *sql.DB) (*MemoryHealth, error) {
	if db == nil {
		return &MemoryHealth{ContinuityScore: 1.0}, nil
	}

	h := &MemoryHealth{}
	sevenDaysAgo := time.Now().Add(-7 * 24 * time.Hour).UnixMilli()

	h.TotalMemories7d = countRows(db,
		`SELECT COUNT(*) FROM interaction_log WHERE timestamp_ms >= ? AND operation_type = 'write' AND policy_decision = 'allowed'`,
		sevenDaysAgo)

	h.RetrievableCount = h.TotalMemories7d

	if h.TotalMemories7d > 0 {
		h.ContinuityScore = float64(h.RetrievableCount) / float64(h.TotalMemories7d)
	} else {
		h.ContinuityScore = 1.0
	}

	h.CrashRecoveries = countRows(db,
		`SELECT COUNT(*) FROM interaction_log WHERE timestamp_ms >= ? AND operation_type = 'wal_recovery'`,
		sevenDaysAgo)

	h.QuarantineCount7d = countRows(db,
		`SELECT COUNT(*) FROM quarantine WHERE intercepted_at >= ?`,
		sevenDaysAgo)

	h.CrossAgentCoverage = calculateCrossAgent(db, sevenDaysAgo)

	return h, nil
}

func calculateCrossAgent(db *sql.DB, sinceMS int64) CrossAgentStats {
	stats := CrossAgentStats{}
	agentMap := make(map[string]*AgentActivity)

	rows, err := db.Query(
		`SELECT source, operation_type, MAX(timestamp_ms) as last_ts, COUNT(*) as cnt
		 FROM interaction_log
		 WHERE timestamp_ms >= ?
		 GROUP BY source, operation_type`,
		sinceMS)
	if err != nil {
		return stats
	}
	defer rows.Close()

	writers := make(map[string]bool)
	readers := make(map[string]bool)

	for rows.Next() {
		var source, opType string
		var lastTS int64
		var count int
		if err := rows.Scan(&source, &opType, &lastTS, &count); err != nil {
			continue
		}

		if _, ok := agentMap[source]; !ok {
			agentMap[source] = &AgentActivity{AgentID: source}
		}
		a := agentMap[source]

		lastTime := time.UnixMilli(lastTS)
		if a.LastActive == "" || lastTime.After(parseTime(a.LastActive)) {
			a.LastActive = formatRelative(lastTime)
		}

		switch opType {
		case "write":
			a.Writes += count
			writers[source] = true
		case "query":
			a.Reads += count
			readers[source] = true
		}
	}

	stats.WritingAgents = len(writers)
	stats.ReadingAgents = len(readers)

	for _, a := range agentMap {
		stats.AgentBreakdown = append(stats.AgentBreakdown, *a)
	}
	return stats
}

func countRows(db *sql.DB, query string, args ...interface{}) int {
	var count int
	row := db.QueryRow(query, args...)
	if err := row.Scan(&count); err != nil {
		return 0
	}
	return count
}

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

func formatRelative(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
