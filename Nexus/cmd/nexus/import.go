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
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/bubblefish-tech/nexus/internal/ingest/importer"
)

// runImport executes `nexus import`.
//
// Usage:
//
//	nexus import <path> [--format auto|claude-zip|chatgpt-zip|claude-code-dir|cursor-dir|jsonl]
//	                         [--source-name <custom>]
//	                         [--dry-run]
//	                         [--url <daemon URL>]
//	                         [--api-key <key>]
func runImport(args []string) {
	fs := flag.NewFlagSet("nexus import", flag.ExitOnError)
	format := fs.String("format", "auto", "export format: auto, claude-zip, chatgpt-zip, claude-code-dir, cursor-dir, jsonl, markdown-diary")
	sourceName := fs.String("source-name", "", "custom source name (default: auto-detected)")
	dryRun := fs.Bool("dry-run", false, "count memories without writing")
	url := fs.String("url", "http://127.0.0.1:8080", "daemon base URL")
	apiKey := fs.String("api-key", "", "data-plane API key (or set NEXUS_API_KEY)")

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "nexus import: path argument is required")
		fmt.Fprintln(os.Stderr, "usage: nexus import <path> [--format auto] [--dry-run] [--source-name custom]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "supported formats:")
		fmt.Fprintln(os.Stderr, "  claude-zip       Claude data export ZIP (conversations.json + users.json)")
		fmt.Fprintln(os.Stderr, "  chatgpt-zip      ChatGPT data export ZIP (conversations.json)")
		fmt.Fprintln(os.Stderr, "  claude-code-dir  Claude Code project directory (*.jsonl)")
		fmt.Fprintln(os.Stderr, "  cursor-dir       Cursor editor directory (chat-history/*.json)")
		fmt.Fprintln(os.Stderr, "  jsonl            Generic JSONL file ({role, content, timestamp} per line)")
		fmt.Fprintln(os.Stderr, "  markdown-diary   Directory of dated markdown files (YYYY-MM-DD.md)")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "coming in v0.1.4: Slack exports, Codex CLI, LM Studio, Open WebUI")
		os.Exit(1)
	}

	path := fs.Arg(0)

	if *apiKey == "" {
		*apiKey = os.Getenv("NEXUS_API_KEY")
	}

	if !*dryRun && *apiKey == "" {
		fmt.Fprintln(os.Stderr, "nexus import: --api-key or NEXUS_API_KEY is required (or use --dry-run)")
		os.Exit(1)
	}

	var writer importer.Writer
	if !*dryRun {
		writer = &httpImportWriter{
			baseURL: *url,
			apiKey:  *apiKey,
			client:  &http.Client{Timeout: 10 * time.Second},
		}
	}

	fmt.Fprintf(os.Stderr, "nexus import: importing %s (format=%s)\n", path, *format)

	result, err := importer.Run(importer.Options{
		Path:       path,
		Format:     importer.Format(*format),
		SourceName: *sourceName,
		DryRun:     *dryRun,
		Writer:     writer,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus import: %v\n", err)
		os.Exit(1)
	}

	if result.Format == importer.FormatMarkdownDiary {
		printMarkdownDiarySummary(result, *dryRun, *sourceName)
	} else if *dryRun {
		fmt.Fprintf(os.Stderr, "nexus import: DRY RUN — %d memories found, 0 written\n", result.Total)
	} else {
		fmt.Fprintf(os.Stderr, "nexus import: %d written, %d skipped, %d errored (%.1fs)\n",
			result.Written, result.Skipped, result.Errored, result.Duration.Seconds())
	}
}

func printMarkdownDiarySummary(r *importer.Result, dryRun bool, sourceName string) {
	if sourceName == "" {
		sourceName = "markdown-import"
	}
	action := "imported"
	if dryRun {
		action = "found (dry run)"
	}
	fmt.Fprintf(os.Stderr, "\nMarkdown diary import complete:\n")
	fmt.Fprintf(os.Stderr, "  Files scanned:      %d\n", r.FilesScanned)
	fmt.Fprintf(os.Stderr, "  Memories %s: %d\n", action, r.Written)
	fmt.Fprintf(os.Stderr, "  Duplicates skipped: %d\n", r.Skipped)
	fmt.Fprintf(os.Stderr, "  Fragments skipped:  %d (too short)\n", r.FragmentsSkipped)
	fmt.Fprintf(os.Stderr, "  Source: %q\n", sourceName)
	if len(r.TypeBreakdown) > 0 {
		fmt.Fprintf(os.Stderr, "\n  Type breakdown:\n")
		for _, typ := range []string{"diary", "long-term", "personality", "preferences", "document"} {
			if count, ok := r.TypeBreakdown[typ]; ok {
				fmt.Fprintf(os.Stderr, "    %-14s %d\n", typ+":", count)
			}
		}
	}
	fmt.Fprintln(os.Stderr)
}

// httpImportWriter sends imported memories to the running daemon via HTTP.
type httpImportWriter struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

func (w *httpImportWriter) Write(source string, memory importer.Memory) error {
	body := map[string]interface{}{
		"content": memory.Content,
		"role":    memory.Role,
	}
	if memory.Model != "" {
		body["model"] = memory.Model
	}
	if memory.Meta != nil {
		body["metadata"] = memory.Meta
	}

	data, _ := json.Marshal(body)
	url := fmt.Sprintf("%s/inbound/%s", w.baseURL, source)

	req, err := http.NewRequest(http.MethodPost, url, io.NopCloser(bytes.NewReader(data)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+w.apiKey)

	resp, err := w.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("import write returned %d", resp.StatusCode)
	}
	return nil
}
