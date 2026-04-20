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

	"github.com/bubblefish-tech/nexus/internal/demo"
)

// runDemo executes `nexus demo`.
//
// Usage:
//
//	nexus demo [--url URL] [--api-key KEY] [--admin-key KEY] [--source S] [--destination D] [--keep]
//
// When --url is omitted the demo starts its own daemon process, performs the
// SIGKILL crash-recovery cycle, and verifies 50/50 recovery with 0 duplicates.
//
// Reference: Tech Spec Section 13.3, Phase R-26.
func runDemo(args []string) {
	fs := flag.NewFlagSet("nexus demo", flag.ExitOnError)
	url := fs.String("url", "", "base URL of a running Nexus daemon (omit to auto-start)")
	source := fs.String("source", "default", "source name for demo writes")
	destination := fs.String("destination", "sqlite", "destination name for demo queries")
	apiKey := fs.String("api-key", "", "data-plane API key (or set NEXUS_API_KEY)")
	adminKey := fs.String("admin-key", "", "admin token (or set NEXUS_ADMIN_KEY)")
	keep := fs.Bool("keep", false, "keep demo data after the run (do not clean up)")

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	// Resolve keys from env if not provided via flags.
	if *apiKey == "" {
		*apiKey = os.Getenv("NEXUS_API_KEY")
	}
	if *adminKey == "" {
		*adminKey = os.Getenv("NEXUS_ADMIN_KEY")
	}

	if *apiKey == "" {
		fmt.Fprintln(os.Stderr, "nexus demo: --api-key or NEXUS_API_KEY is required")
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	opts := demo.Options{
		URL:         *url,
		Source:      *source,
		Destination: *destination,
		APIKey:      *apiKey,
		AdminKey:    *adminKey,
		Keep:        *keep,
		Logger:      logger,
	}

	result, err := demo.Run(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus demo: %v\n", err)
		os.Exit(1)
	}

	// Print result JSON to stdout.
	out, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(out))

	if result.Pass {
		fmt.Fprintln(os.Stderr, "nexus demo: PASS — 50 present, 0 duplicates")
	} else {
		fmt.Fprintf(os.Stderr, "nexus demo: FAIL — recovered=%d, duplicates=%d, missing=%d\n",
			result.TotalRecovered, result.Duplicates, len(result.MissingKeys))
		os.Exit(1)
	}
}
