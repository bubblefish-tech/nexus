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
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/bubblefish-tech/nexus/internal/bench"
)

// runBench executes `bubblefish bench`.
//
// Usage:
//
//	bubblefish bench --mode throughput [--url URL] [--n N] [--concurrency C] [--output file.json]
//	bubblefish bench --mode latency   [--url URL] [--n N] [--query Q] [--output file.json]
//	bubblefish bench --mode eval      [--url URL] [--golden file.json] [--output file.json]
//
// Reference: Tech Spec Section 13.4, Phase R-25.
func runBench(args []string) {
	fs := flag.NewFlagSet("bubblefish bench", flag.ExitOnError)
	mode := fs.String("mode", "", "benchmark mode: throughput, latency, or eval (required)")
	url := fs.String("url", "http://127.0.0.1:8000", "base URL of the running Nexus daemon")
	n := fs.Int("n", 0, "number of requests (default: 100 for throughput, 50 for latency)")
	concurrency := fs.Int("concurrency", 10, "concurrent workers (throughput mode only)")
	source := fs.String("source", "default", "source name for write requests")
	destination := fs.String("destination", "sqlite", "destination name for read requests")
	apiKey := fs.String("api-key", "", "data-plane API key (or set NEXUS_API_KEY)")
	adminKey := fs.String("admin-key", "", "admin token for debug_stages (or set NEXUS_ADMIN_KEY)")
	golden := fs.String("golden", "", "path to known-good JSON file (eval mode)")
	query := fs.String("query", "", "search query for latency/eval modes")
	output := fs.String("output", "", "path for machine-readable JSON results")

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	if *mode == "" {
		fmt.Fprintln(os.Stderr, "bubblefish bench: --mode is required")
		fmt.Fprintln(os.Stderr, "usage: bubblefish bench --mode <throughput|latency|eval> [flags]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "modes:")
		fmt.Fprintln(os.Stderr, "  throughput  N concurrent writes — req/s, p50/p95/p99")
		fmt.Fprintln(os.Stderr, "  latency     N sequential reads — per-stage breakdown via _nexus.debug")
		fmt.Fprintln(os.Stderr, "  eval        compare retrieval vs known-good JSON — precision, recall, MRR, NDCG")
		os.Exit(1)
	}

	// Resolve keys from env if not provided via flags.
	if *apiKey == "" {
		*apiKey = os.Getenv("NEXUS_API_KEY")
	}
	if *adminKey == "" {
		*adminKey = os.Getenv("NEXUS_ADMIN_KEY")
	}

	if *apiKey == "" && *adminKey == "" {
		fmt.Fprintln(os.Stderr, "bubblefish bench: --api-key or NEXUS_API_KEY is required")
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	opts := bench.Options{
		Mode:        *mode,
		URL:         *url,
		N:           *n,
		Concurrency: *concurrency,
		Source:      *source,
		Destination: *destination,
		APIKey:      *apiKey,
		AdminKey:    *adminKey,
		GoldenFile:  *golden,
		Query:       *query,
		OutputFile:  *output,
		Logger:      logger,
	}

	result, err := bench.Run(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish bench: %v\n", err)
		os.Exit(1)
	}

	// Print summary to stdout.
	out, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(out))
}
