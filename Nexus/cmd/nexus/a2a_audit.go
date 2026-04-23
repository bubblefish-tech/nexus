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
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/bubblefish-tech/nexus/internal/a2a"
	"github.com/bubblefish-tech/nexus/internal/a2a/server"
)

// runA2AAudit dispatches audit subcommands for A2A.
func runA2AAudit(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: nexus a2a audit <subcommand>")
		fmt.Fprintln(os.Stderr, "subcommands:")
		fmt.Fprintln(os.Stderr, "  tail   show recent tasks (audit tail)")
		os.Exit(1)
	}

	switch args[0] {
	case "tail":
		runA2AAuditTail(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "nexus a2a audit: unknown subcommand %q\n", args[0])
		os.Exit(1)
	}
}

func runA2AAuditTail(args []string) {
	var since string

	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--since" && i+1 < len(args):
			since = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--since="):
			since = strings.TrimPrefix(args[i], "--since=")
		default:
			fmt.Fprintf(os.Stderr, "nexus a2a audit tail: unknown flag %q\n", args[i])
			os.Exit(1)
		}
	}

	filter := server.TaskFilter{
		Limit: 50,
	}

	if since != "" {
		dur, err := time.ParseDuration(since)
		if err != nil {
			fmt.Fprintf(os.Stderr, "nexus a2a audit tail: invalid duration %q: %v\n", since, err)
			os.Exit(1)
		}
		filter.Since = time.Now().Add(-dur)
	} else {
		// Default: last 24 hours.
		filter.Since = time.Now().Add(-24 * time.Hour)
	}

	ts := openA2ATaskStore()
	defer ts.Close()

	ctx := context.Background()
	tasks, err := ts.ListTasks(ctx, filter)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus a2a audit tail: %v\n", err)
		os.Exit(1)
	}

	type auditLine struct {
		TaskID    string `json:"taskId"`
		State     string `json:"state"`
		Timestamp string `json:"timestamp"`
	}

	lines := make([]auditLine, len(tasks))
	for i, t := range tasks {
		lines[i] = auditLine{
			TaskID:    t.TaskID,
			State:     string(t.Status.State),
			Timestamp: a2a.FormatTime(time.Now()), // best-effort; real timestamp is in the task
		}
		if t.Status.Timestamp != "" {
			lines[i].Timestamp = t.Status.Timestamp
		}
	}

	out, _ := json.MarshalIndent(lines, "", "  ")
	fmt.Println(string(out))
}
