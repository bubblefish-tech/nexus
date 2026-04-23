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
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bubblefish-tech/nexus/internal/config"
)

// runLogs executes the `nexus logs` command.
//
// Reads <configDir>/logs/nexus.log (JSONL written by buildLogger) and prints
// matching entries. Supports tail, level filter, since filter, and raw JSON
// output. Non-fatal if the log file does not exist (daemon has never run).
//
// Reference: Tech Spec WIRE.5.
func runLogs(args []string) {
	fs := flag.NewFlagSet("logs", flag.ExitOnError)
	tail := fs.Int("tail", 50, "show last N log lines (0 = all)")
	level := fs.String("level", "", "filter by minimum level: debug, info, warn, error")
	since := fs.String("since", "", "only show entries after this time (RFC3339 or duration like 1h, 30m)")
	asJSON := fs.Bool("json", false, "output raw JSON lines")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "nexus logs: %v\n", err)
		os.Exit(1)
	}

	configDir, err := config.ConfigDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus logs: %v\n", err)
		os.Exit(1)
	}

	logPath := filepath.Join(configDir, "logs", "nexus.log")
	f, err := os.Open(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("nexus logs: no log file at %s (daemon has not run yet)\n", logPath)
			return
		}
		fmt.Fprintf(os.Stderr, "nexus logs: open %s: %v\n", logPath, err)
		os.Exit(1)
	}
	defer func() { _ = f.Close() }()

	// Resolve "since" filter.
	var sinceTime time.Time
	if *since != "" {
		sinceTime = parseSince(*since)
		if sinceTime.IsZero() {
			fmt.Fprintf(os.Stderr, "nexus logs: invalid --since value %q (use RFC3339 or duration like 1h, 30m)\n", *since)
			os.Exit(1)
		}
	}

	minLevel := parseLevelFilter(*level)

	// Collect all matching lines.
	var lines []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)
	for scanner.Scan() {
		raw := scanner.Text()
		if raw == "" {
			continue
		}

		var entry map[string]any
		if err := json.Unmarshal([]byte(raw), &entry); err != nil {
			continue
		}

		if !matchLevel(entry, minLevel) {
			continue
		}
		if !sinceTime.IsZero() && !matchSince(entry, sinceTime) {
			continue
		}

		lines = append(lines, raw)
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "nexus logs: read: %v\n", err)
		os.Exit(1)
	}

	// Apply tail.
	if *tail > 0 && len(lines) > *tail {
		lines = lines[len(lines)-*tail:]
	}

	for _, raw := range lines {
		if *asJSON {
			fmt.Println(raw)
			continue
		}
		fmt.Println(formatLogLine(raw))
	}
}

// parseSince parses a time value from --since: RFC3339 or Go duration (1h, 30m, etc.).
func parseSince(s string) time.Time {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	if d, err := time.ParseDuration(s); err == nil {
		return time.Now().Add(-d)
	}
	return time.Time{}
}

// levelInt converts a slog level string to an integer for comparison.
// debug=-4, info=0, warn=4, error=8.
func levelInt(s string) int {
	switch strings.ToLower(s) {
	case "debug":
		return -4
	case "warn", "warning":
		return 4
	case "error":
		return 8
	default:
		return 0
	}
}

func parseLevelFilter(s string) int {
	if s == "" {
		return -4 // accept all
	}
	return levelInt(s)
}

func matchLevel(entry map[string]any, minLevel int) bool {
	lv, _ := entry["level"].(string)
	return levelInt(lv) >= minLevel
}

func matchSince(entry map[string]any, since time.Time) bool {
	ts, _ := entry["time"].(string)
	if ts == "" {
		return true
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return true
	}
	return t.After(since)
}

// formatLogLine renders a JSON log line as a human-readable string.
func formatLogLine(raw string) string {
	var entry map[string]any
	if err := json.Unmarshal([]byte(raw), &entry); err != nil {
		return raw
	}

	ts, _ := entry["time"].(string)
	lv, _ := entry["level"].(string)
	msg, _ := entry["msg"].(string)

	// Parse timestamp to compact form.
	displayTime := ts
	if t, err := time.Parse(time.RFC3339, ts); err == nil {
		displayTime = t.Format("15:04:05")
	}

	// Build extra key=value pairs, skipping known fields.
	skip := map[string]bool{"time": true, "level": true, "msg": true}
	var extras []string
	for k, v := range entry {
		if skip[k] {
			continue
		}
		extras = append(extras, fmt.Sprintf("%s=%v", k, v))
	}

	line := fmt.Sprintf("%s %-5s %s", displayTime, strings.ToUpper(lv), msg)
	if len(extras) > 0 {
		line += "  " + strings.Join(extras, " ")
	}
	return line
}
