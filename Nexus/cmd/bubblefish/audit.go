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
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BubbleFish-Nexus/internal/audit"
	"github.com/BubbleFish-Nexus/internal/config"
)

// runAudit dispatches `bubblefish audit <subcommand>`.
//
// Reference: Tech Spec Addendum Section A5.
func runAudit(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: bubblefish audit <subcommand>")
		fmt.Fprintln(os.Stderr, "subcommands:")
		fmt.Fprintln(os.Stderr, "  query   query interaction log with filters (no daemon required)")
		fmt.Fprintln(os.Stderr, "  stats   print summary statistics (no daemon required)")
		fmt.Fprintln(os.Stderr, "  export  export interaction log to JSON or CSV file (no daemon required)")
		fmt.Fprintln(os.Stderr, "  tail    stream interaction log entries in real time")
		os.Exit(1)
	}

	switch args[0] {
	case "query":
		runAuditQuery(args[1:])
	case "stats":
		runAuditStats(args[1:])
	case "export":
		runAuditExport(args[1:])
	case "tail":
		runAuditTail(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "bubblefish audit: unknown subcommand %q\n", args[0])
		os.Exit(1)
	}
}

// buildReaderFromConfig loads daemon.toml and creates an AuditReader with the
// correct integrity/encryption/dual-write settings. CLI commands read log files
// directly — no running daemon required.
func buildReaderFromConfig() (*audit.AuditReader, error) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	}))

	configDir, err := config.ConfigDir()
	if err != nil {
		return nil, fmt.Errorf("resolve config dir: %w", err)
	}

	cfg, err := config.Load(configDir, logger)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	logFile := cfg.Daemon.Audit.LogFile
	if logFile == "" {
		logFile = filepath.Join(configDir, "logs", "interactions.jsonl")
	} else if strings.HasPrefix(logFile, "~/") {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return nil, fmt.Errorf("resolve home dir: %w", herr)
		}
		logFile = filepath.Join(home, logFile[2:])
	}

	var opts []audit.ReaderOption
	opts = append(opts, audit.WithReaderLogger(logger))
	opts = append(opts, audit.WithReaderDualWrite(cfg.Daemon.Audit.AuditDualWriteEnabled()))

	if cfg.Daemon.Audit.Integrity.Mode == "mac" && cfg.Daemon.Audit.Integrity.MacKeyFile != "" {
		resolved, rerr := config.ResolveEnv(cfg.Daemon.Audit.Integrity.MacKeyFile, logger)
		if rerr != nil {
			return nil, fmt.Errorf("resolve audit integrity key: %w", rerr)
		}
		opts = append(opts, audit.WithReaderIntegrity("mac", []byte(resolved)))
	}

	if cfg.Daemon.Audit.Encryption.Enabled && cfg.Daemon.Audit.Encryption.KeyFile != "" {
		resolved, rerr := config.ResolveEnv(cfg.Daemon.Audit.Encryption.KeyFile, logger)
		if rerr != nil {
			return nil, fmt.Errorf("resolve audit encryption key: %w", rerr)
		}
		opts = append(opts, audit.WithReaderEncryption([]byte(resolved)))
	}

	return audit.NewAuditReader(logFile, opts...), nil
}

// ---------------------------------------------------------------------------
// bubblefish audit query
// ---------------------------------------------------------------------------

// runAuditQuery queries the interaction log with the same parameters as the
// GET /api/audit/log HTTP endpoint. Reads log files directly.
//
// Reference: Tech Spec Addendum Section A5.
func runAuditQuery(args []string) {
	fs := flag.NewFlagSet("audit query", flag.ExitOnError)
	source := fs.String("source", "", "filter by source name")
	actorType := fs.String("actor-type", "", "filter: user, agent, system")
	actorID := fs.String("actor-id", "", "filter by specific actor")
	operation := fs.String("operation", "", "filter: write, query, admin")
	decision := fs.String("decision", "", "filter: allowed, denied, filtered")
	subject := fs.String("subject", "", "filter by subject namespace")
	destination := fs.String("destination", "", "filter by destination")
	after := fs.String("after", "", "records after this timestamp (RFC3339)")
	before := fs.String("before", "", "records before this timestamp (RFC3339)")
	limit := fs.Int("limit", 100, "max records (1-1000)")
	offset := fs.Int("offset", 0, "pagination offset")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish audit query: parse flags: %v\n", err)
		os.Exit(1)
	}

	reader, err := buildReaderFromConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish audit query: %v\n", err)
		os.Exit(1)
	}

	filter := audit.AuditFilter{
		Source:         *source,
		ActorType:      *actorType,
		ActorID:        *actorID,
		Operation:      *operation,
		PolicyDecision: *decision,
		Subject:        *subject,
		Destination:    *destination,
		Limit:          *limit,
		Offset:         *offset,
	}

	if *after != "" {
		t, perr := time.Parse(time.RFC3339, *after)
		if perr != nil {
			fmt.Fprintf(os.Stderr, "bubblefish audit query: invalid --after: %v\n", perr)
			os.Exit(1)
		}
		filter.After = t
	}
	if *before != "" {
		t, perr := time.Parse(time.RFC3339, *before)
		if perr != nil {
			fmt.Fprintf(os.Stderr, "bubblefish audit query: invalid --before: %v\n", perr)
			os.Exit(1)
		}
		filter.Before = t
	}

	result, err := reader.Query(filter)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish audit query: %v\n", err)
		os.Exit(1)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(result); err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish audit query: encode: %v\n", err)
		os.Exit(1)
	}
}

// ---------------------------------------------------------------------------
// bubblefish audit stats
// ---------------------------------------------------------------------------

// auditCLIStats holds summary statistics for CLI display.
type auditCLIStats struct {
	TotalRecords      int            `json:"total_records"`
	InteractionsPerHr map[string]int `json:"interactions_per_hour"`
	DenialRate        float64        `json:"denial_rate"`
	TopSources        map[string]int `json:"top_sources"`
	TopActors         map[string]int `json:"top_actors"`
	ByOperation       map[string]int `json:"by_operation"`
	ByDecision        map[string]int `json:"by_decision"`
}

// computeStats calculates summary statistics from a set of interaction records.
func computeStats(records []audit.InteractionRecord, total int) auditCLIStats {
	stats := auditCLIStats{
		TotalRecords:      total,
		InteractionsPerHr: make(map[string]int),
		TopSources:        make(map[string]int),
		TopActors:         make(map[string]int),
		ByOperation:       make(map[string]int),
		ByDecision:        make(map[string]int),
	}

	oneHourAgo := time.Now().UTC().Add(-1 * time.Hour)
	var denied int

	for _, rec := range records {
		stats.ByOperation[rec.OperationType]++
		stats.ByDecision[rec.PolicyDecision]++
		stats.TopSources[rec.Source]++
		if rec.ActorID != "" {
			stats.TopActors[rec.ActorID]++
		}
		if rec.PolicyDecision == "denied" {
			denied++
		}
		if rec.Timestamp.After(oneHourAgo) {
			stats.InteractionsPerHr[rec.OperationType]++
		}
	}

	if stats.TotalRecords > 0 {
		stats.DenialRate = float64(denied) / float64(stats.TotalRecords)
	}

	return stats
}

// runAuditStats prints summary statistics for the interaction log.
//
// Reference: Tech Spec Addendum Section A5.
func runAuditStats(args []string) {
	fs := flag.NewFlagSet("audit stats", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish audit stats: parse flags: %v\n", err)
		os.Exit(1)
	}

	reader, err := buildReaderFromConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish audit stats: %v\n", err)
		os.Exit(1)
	}

	result, err := reader.Query(audit.AuditFilter{Limit: 1000})
	if err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish audit stats: %v\n", err)
		os.Exit(1)
	}

	stats := computeStats(result.Records, result.TotalMatching)

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(stats); err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish audit stats: encode: %v\n", err)
		os.Exit(1)
	}
}

// ---------------------------------------------------------------------------
// bubblefish audit export
// ---------------------------------------------------------------------------

// csvHeaders are the column headers for CSV export, matching the HTTP API.
var csvHeaders = []string{
	"record_id", "request_id", "timestamp", "source", "actor_type",
	"actor_id", "effective_ip", "operation_type", "endpoint",
	"http_method", "http_status_code", "payload_id", "destination",
	"subject", "policy_decision", "policy_reason", "latency_ms",
}

// writeCSV writes interaction records in CSV format to the given file.
func writeCSV(path string, records []audit.InteractionRecord) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			slog.Error("close export file", "err", err)
		}
	}()

	cw := csv.NewWriter(f)
	if err := cw.Write(csvHeaders); err != nil {
		return fmt.Errorf("write header: %w", err)
	}
	for _, rec := range records {
		if err := cw.Write(recordToCSVRow(rec)); err != nil {
			return fmt.Errorf("write row: %w", err)
		}
	}
	cw.Flush()
	return cw.Error()
}

// recordToCSVRow converts an InteractionRecord to a CSV row matching csvHeaders.
func recordToCSVRow(rec audit.InteractionRecord) []string {
	return []string{
		rec.RecordID,
		rec.RequestID,
		rec.Timestamp.Format(time.RFC3339Nano),
		rec.Source,
		rec.ActorType,
		rec.ActorID,
		rec.EffectiveIP,
		rec.OperationType,
		rec.Endpoint,
		rec.HTTPMethod,
		fmt.Sprintf("%d", rec.HTTPStatusCode),
		rec.PayloadID,
		rec.Destination,
		rec.Subject,
		rec.PolicyDecision,
		rec.PolicyReason,
		fmt.Sprintf("%.3f", rec.LatencyMs),
	}
}

// runAuditExport exports interaction log records to a file.
// Supports --format json (default) or csv. --after and --before time filters.
//
// Reference: Tech Spec Addendum Section A5.
func runAuditExport(args []string) {
	fs := flag.NewFlagSet("audit export", flag.ExitOnError)
	format := fs.String("format", "json", "output format: json or csv")
	after := fs.String("after", "", "records after this timestamp (RFC3339)")
	before := fs.String("before", "", "records before this timestamp (RFC3339)")
	source := fs.String("source", "", "filter by source name")
	operation := fs.String("operation", "", "filter: write, query, admin")
	output := fs.String("output", "", "output file path (default: stdout)")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish audit export: parse flags: %v\n", err)
		os.Exit(1)
	}

	if *format != "json" && *format != "csv" {
		fmt.Fprintf(os.Stderr, "bubblefish audit export: unsupported format %q (use json or csv)\n", *format)
		os.Exit(1)
	}

	reader, err := buildReaderFromConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish audit export: %v\n", err)
		os.Exit(1)
	}

	filter := audit.AuditFilter{
		Source:    *source,
		Operation: *operation,
		Limit:    1000,
	}
	if *after != "" {
		t, perr := time.Parse(time.RFC3339, *after)
		if perr != nil {
			fmt.Fprintf(os.Stderr, "bubblefish audit export: invalid --after: %v\n", perr)
			os.Exit(1)
		}
		filter.After = t
	}
	if *before != "" {
		t, perr := time.Parse(time.RFC3339, *before)
		if perr != nil {
			fmt.Fprintf(os.Stderr, "bubblefish audit export: invalid --before: %v\n", perr)
			os.Exit(1)
		}
		filter.Before = t
	}

	result, err := reader.Query(filter)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish audit export: %v\n", err)
		os.Exit(1)
	}

	// Write to file or stdout.
	if *output != "" {
		if *format == "csv" {
			if err := writeCSV(*output, result.Records); err != nil {
				fmt.Fprintf(os.Stderr, "bubblefish audit export: %v\n", err)
				os.Exit(1)
			}
		} else {
			if err := writeJSONFile(*output, result.Records); err != nil {
				fmt.Fprintf(os.Stderr, "bubblefish audit export: %v\n", err)
				os.Exit(1)
			}
		}
		fmt.Fprintf(os.Stderr, "bubblefish audit export: %d records written to %s\n", len(result.Records), *output)
		return
	}

	// Stdout.
	if *format == "csv" {
		cw := csv.NewWriter(os.Stdout)
		_ = cw.Write(csvHeaders)
		for _, rec := range result.Records {
			_ = cw.Write(recordToCSVRow(rec))
		}
		cw.Flush()
	} else {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(result.Records)
	}
}

// writeJSONFile writes interaction records as a JSON array to the given file.
func writeJSONFile(path string, records []audit.InteractionRecord) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			slog.Error("close export file", "err", err)
		}
	}()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(records)
}

// ---------------------------------------------------------------------------
// bubblefish audit tail
// ---------------------------------------------------------------------------

// runAuditTail streams interaction log entries to stdout, watching for new
// records. Supports --source, --actor-type, and --operation filters.
//
// Reads the current log file and polls for new entries. No running daemon
// required for file-based tailing.
//
// Reference: Tech Spec Addendum Section A5.
func runAuditTail(args []string) {
	fs := flag.NewFlagSet("audit tail", flag.ExitOnError)
	source := fs.String("source", "", "filter by source name")
	actorType := fs.String("actor-type", "", "filter: user, agent, system")
	operation := fs.String("operation", "", "filter: write, query, admin")
	follow := fs.Bool("follow", true, "continue watching for new entries")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish audit tail: parse flags: %v\n", err)
		os.Exit(1)
	}

	reader, err := buildReaderFromConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish audit tail: %v\n", err)
		os.Exit(1)
	}

	filter := audit.AuditFilter{
		Source:    *source,
		ActorType: *actorType,
		Operation: *operation,
		Limit:    50,
	}

	// Print the last 50 records matching filters.
	result, err := reader.Query(filter)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish audit tail: %v\n", err)
		os.Exit(1)
	}

	enc := json.NewEncoder(os.Stdout)
	seen := make(map[string]struct{})
	for _, rec := range result.Records {
		seen[rec.RecordID] = struct{}{}
		_ = enc.Encode(rec)
	}

	if !*follow {
		return
	}

	// Poll for new entries every second.
	fmt.Fprintln(os.Stderr, "bubblefish audit tail: watching for new entries (ctrl+c to stop)")

	// Use a higher limit for polling to catch bursts.
	filter.Limit = 1000
	filter.Offset = 0

	for {
		time.Sleep(1 * time.Second)

		result, err := reader.Query(filter)
		if err != nil {
			fmt.Fprintf(os.Stderr, "bubblefish audit tail: poll error: %v\n", err)
			continue
		}

		for _, rec := range result.Records {
			if _, dup := seen[rec.RecordID]; dup {
				continue
			}
			seen[rec.RecordID] = struct{}{}
			_ = enc.Encode(rec)
		}
	}
}
