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

	"github.com/bubblefish-tech/nexus/internal/simulate"
)

// runSimulate executes `bubblefish simulate`.
//
// Reference: v0.1.3 Build Plan Section 6.2.
func runSimulate(args []string) {
	fs := flag.NewFlagSet("bubblefish simulate", flag.ExitOnError)
	seed := fs.Int64("seed", 0, "random seed (0 = random)")
	duration := fs.Duration("duration", 30*time.Second, "simulation duration")
	concurrency := fs.Int("concurrency", 5, "concurrent writer goroutines")
	faultRate := fs.Float64("fault-rate", 0.05, "probability of fault per write cycle (0.0-1.0)")
	report := fs.String("report", "", "output file for JSON report (default: stdout)")

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	result, err := simulate.Run(simulate.Options{
		Seed:        *seed,
		Duration:    *duration,
		Concurrency: *concurrency,
		FaultRate:   *faultRate,
		Logger:      logger,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish simulate: %v\n", err)
		os.Exit(1)
	}

	out, _ := json.MarshalIndent(result, "", "  ")

	if *report != "" {
		if err := os.WriteFile(*report, out, 0600); err != nil {
			fmt.Fprintf(os.Stderr, "bubblefish simulate: write report: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "bubblefish simulate: report written to %s\n", *report)
	} else {
		fmt.Println(string(out))
	}

	fmt.Fprintln(os.Stderr, result.Verdict)
	if !result.Pass {
		os.Exit(1)
	}
}
