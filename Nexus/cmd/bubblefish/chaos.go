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
	"time"

	"github.com/BubbleFish-Nexus/internal/chaos"
)

// runChaos executes `bubblefish chaos`.
//
// Usage:
//
//	bubblefish chaos --url URL --api-key KEY [flags]
//
// Reference: v0.1.3 Build Plan Section 6.1.
func runChaos(args []string) {
	fs := flag.NewFlagSet("bubblefish chaos", flag.ExitOnError)
	url := fs.String("url", "http://127.0.0.1:8000", "base URL of the running Nexus daemon")
	apiKey := fs.String("api-key", "", "data-plane API key (or set NEXUS_API_KEY)")
	adminKey := fs.String("admin-key", "", "admin token (or set NEXUS_ADMIN_KEY)")
	source := fs.String("source", "default", "source name for write requests")
	dbPath := fs.String("db", "", "path to memories.db for direct DB verification (required)")
	duration := fs.Duration("duration", 60*time.Second, "how long to run the chaos test")
	concurrency := fs.Int("concurrency", 5, "number of concurrent writer goroutines")
	faultInterval := fs.Duration("fault-interval", 10*time.Second, "time between fault injections")
	seed := fs.Int64("seed", 0, "random seed (0 = random)")
	report := fs.String("report", "", "output file for JSON report (default: stdout)")

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	if *apiKey == "" {
		*apiKey = os.Getenv("NEXUS_API_KEY")
	}
	if *adminKey == "" {
		*adminKey = os.Getenv("NEXUS_ADMIN_KEY")
	}
	if *apiKey == "" {
		fmt.Fprintln(os.Stderr, "bubblefish chaos: --api-key or NEXUS_API_KEY is required")
		os.Exit(1)
	}
	if *dbPath == "" {
		fmt.Fprintln(os.Stderr, "bubblefish chaos: --db is required (path to memories.db for direct DB verification)")
		os.Exit(1)
	}
	if *adminKey == "" {
		fmt.Fprintln(os.Stderr, "bubblefish chaos: --admin-key or NEXUS_ADMIN_KEY is required for /admin/memories verification")
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	result, err := chaos.Run(chaos.Options{
		URL:           *url,
		Source:        *source,
		DBPath:        *dbPath,
		APIKey:        *apiKey,
		AdminKey:      *adminKey,
		Duration:      *duration,
		Concurrency:   *concurrency,
		FaultInterval: *faultInterval,
		Seed:          *seed,
		ReportFile:    *report,
		Logger:        logger,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish chaos: %v\n", err)
		os.Exit(1)
	}

	out, _ := json.MarshalIndent(result, "", "  ")

	if *report != "" {
		if err := os.WriteFile(*report, out, 0600); err != nil {
			fmt.Fprintf(os.Stderr, "bubblefish chaos: write report: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "bubblefish chaos: report written to %s\n", *report)
	} else {
		fmt.Println(string(out))
	}

	fmt.Fprintln(os.Stderr, result.Verdict)
	if !result.Pass {
		os.Exit(1)
	}
}
