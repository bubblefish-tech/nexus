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

package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/BubbleFish-Nexus/internal/audit"
)

// runTimeline walks the audit log and displays the full history of a memory.
// It collects all audit records that reference the given memory_id and
// displays them chronologically — write, query, cluster assignment, and
// policy decisions.
//
// Usage: bubblefish timeline <memory_id>
//
// No running daemon required.
//
// Reference: v0.1.3 Build Plan Phase 4 Subtask 4.10.
func runTimeline(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: bubblefish timeline <memory_id>")
		os.Exit(1)
	}

	memoryID := args[0]

	reader, err := buildReaderFromConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish timeline: %v\n", err)
		os.Exit(1)
	}

	// Query all audit records referencing this memory_id.
	// We use a broad query and filter client-side since the audit reader
	// doesn't support payload_id filtering directly.
	filter := audit.AuditFilter{
		Limit: 100000,
	}
	result, err := reader.Query(filter)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish timeline: read audit log: %v\n", err)
		os.Exit(1)
	}

	// Filter records that reference the memory_id.
	var matching []audit.InteractionRecord
	for _, rec := range result.Records {
		if rec.PayloadID == memoryID || rec.Subject == memoryID {
			matching = append(matching, rec)
		}
	}

	if len(matching) == 0 {
		fmt.Printf("bubblefish timeline: no audit records found for %s\n", memoryID)
		return
	}

	// Sort by timestamp.
	sort.Slice(matching, func(i, j int) bool {
		return matching[i].Timestamp.Before(matching[j].Timestamp)
	})

	// Print timeline.
	fmt.Printf("Timeline for %s (%d events)\n", memoryID, len(matching))
	fmt.Println(strings.Repeat("-", 80))
	fmt.Printf("%-30s %-10s %-12s %-10s %s\n", "TIMESTAMP", "OPERATION", "SOURCE", "DECISION", "DETAILS")
	fmt.Println(strings.Repeat("-", 80))

	for _, rec := range matching {
		details := ""
		switch rec.OperationType {
		case "write":
			details = fmt.Sprintf("dest=%s", rec.Destination)
			if rec.IsDuplicate {
				details += " (duplicate)"
			}
		case "query":
			details = fmt.Sprintf("profile=%s results=%d", rec.RetrievalProfile, rec.ResultCount)
			if rec.CacheHit {
				details += " (cache)"
			}
		case "admin":
			details = rec.Endpoint
		}

		if rec.PolicyReason != "" {
			details += " reason=" + rec.PolicyReason
		}

		ts := rec.Timestamp.Format("2006-01-02T15:04:05Z")
		fmt.Printf("%-30s %-10s %-12s %-10s %s\n",
			ts, rec.OperationType, rec.Source, rec.PolicyDecision, details)
	}
}
