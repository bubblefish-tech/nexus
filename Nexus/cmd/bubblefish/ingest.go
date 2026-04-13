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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"text/tabwriter"
	"time"
)

// runIngest executes `bubblefish ingest <subcommand>`.
//
// Subcommands:
//
//	status   — list all watchers with state, path, ingest count
//	pause    — pause a named watcher
//	resume   — resume a paused watcher
//	reset    — forget file state for a watcher (triggers re-parse)
func runIngest(args []string) {
	if len(args) == 0 {
		printIngestUsage()
		os.Exit(1)
	}

	url := "http://127.0.0.1:8080"
	adminKey := os.Getenv("NEXUS_ADMIN_KEY")

	switch args[0] {
	case "status":
		ingestStatus(url, adminKey)
	case "pause":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: bubblefish ingest pause <watcher>")
			os.Exit(1)
		}
		ingestControl(url, adminKey, args[1], "pause")
	case "resume":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: bubblefish ingest resume <watcher>")
			os.Exit(1)
		}
		ingestControl(url, adminKey, args[1], "resume")
	case "reset":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: bubblefish ingest reset <watcher> [<path>]")
			os.Exit(1)
		}
		path := ""
		if len(args) > 2 {
			path = args[2]
		}
		ingestReset(url, adminKey, args[1], path)
	default:
		fmt.Fprintf(os.Stderr, "bubblefish ingest: unknown subcommand %q\n", args[0])
		printIngestUsage()
		os.Exit(1)
	}
}

func printIngestUsage() {
	fmt.Fprintln(os.Stderr, "usage: bubblefish ingest <subcommand>")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "subcommands:")
	fmt.Fprintln(os.Stderr, "  status             list all watchers with state and ingest counts")
	fmt.Fprintln(os.Stderr, "  pause <watcher>    pause a named watcher")
	fmt.Fprintln(os.Stderr, "  resume <watcher>   resume a paused watcher")
	fmt.Fprintln(os.Stderr, "  reset <watcher>    forget file state (triggers re-parse from offset 0)")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "environment:")
	fmt.Fprintln(os.Stderr, "  NEXUS_ADMIN_KEY    admin token for daemon API")
}

func ingestStatus(baseURL, adminKey string) {
	body, err := ingestAPIGet(baseURL+"/api/ingest/status", adminKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish ingest status: %v\n", err)
		os.Exit(1)
	}

	var resp struct {
		Watchers []struct {
			Name        string `json:"name"`
			SourceName  string `json:"source_name"`
			State       string `json:"state"`
			Path        string `json:"path"`
			IngestCount int64  `json:"ingest_count"`
			LastIngest  int64  `json:"last_ingest_at"`
		} `json:"watchers"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish ingest status: parse response: %v\n", err)
		os.Exit(1)
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "WATCHER\tSTATE\tPATH\tINGESTED\tLAST INGEST")
	for _, w := range resp.Watchers {
		lastIngest := "-"
		if w.LastIngest > 0 {
			lastIngest = time.Unix(w.LastIngest, 0).Format("2006-01-02 15:04:05")
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\n",
			w.Name, w.State, w.Path, w.IngestCount, lastIngest)
	}
	tw.Flush()
}

func ingestControl(baseURL, adminKey, watcher, action string) {
	url := fmt.Sprintf("%s/api/ingest/%s?watcher=%s", baseURL, action, watcher)
	body, err := ingestAPIPost(url, adminKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish ingest %s: %v\n", action, err)
		os.Exit(1)
	}
	fmt.Println(string(body))
}

func ingestReset(baseURL, adminKey, watcher, path string) {
	url := fmt.Sprintf("%s/api/ingest/reset?watcher=%s", baseURL, watcher)
	if path != "" {
		url += "&path=" + path
	}
	body, err := ingestAPIPost(url, adminKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish ingest reset: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(body))
}

func ingestAPIGet(url, adminKey string) ([]byte, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if adminKey != "" {
		req.Header.Set("Authorization", "Bearer "+adminKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed (is the daemon running?): %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, body)
	}
	return body, nil
}

func ingestAPIPost(url, adminKey string) ([]byte, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return nil, err
	}
	if adminKey != "" {
		req.Header.Set("Authorization", "Bearer "+adminKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed (is the daemon running?): %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, body)
	}
	return body, nil
}
