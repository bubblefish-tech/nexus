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
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/bubblefish-tech/nexus/internal/config"
	"github.com/bubblefish-tech/nexus/internal/maintain"
)

// runMaintain executes the `nexus maintain` command family.
//
// Subcommands:
//
//	status    — print a one-shot snapshot of all tracked tools
//	fix       — run a targeted convergence attempt for one tool
//	watch     — stream live scan + reconcile results (Ctrl-C to stop)
//	registry  — list all connectors in the embedded registry
func runMaintain(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: nexus maintain <subcommand>")
		fmt.Fprintln(os.Stderr, "subcommands:")
		fmt.Fprintln(os.Stderr, "  status    show tracked tools, drift, and health")
		fmt.Fprintln(os.Stderr, "  fix       run a convergence attempt for a specific tool")
		fmt.Fprintln(os.Stderr, "  watch     stream live scan and reconcile results")
		fmt.Fprintln(os.Stderr, "  registry  list all connectors in the embedded registry")
		os.Exit(1)
	}
	switch args[0] {
	case "status":
		runMaintainStatus()
	case "fix":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: nexus maintain fix <tool-name>")
			os.Exit(1)
		}
		runMaintainFix(args[1])
	case "watch":
		runMaintainWatch()
	case "registry":
		runMaintainRegistry()
	default:
		fmt.Fprintf(os.Stderr, "nexus maintain: unknown subcommand %q\n", args[0])
		os.Exit(1)
	}
}

func newMaintainer() (*maintain.Maintainer, error) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	}))
	configDir, err := config.ConfigDir()
	if err != nil {
		return nil, fmt.Errorf("config dir: %w", err)
	}
	return maintain.New(maintain.Config{ConfigDir: configDir}, logger)
}

func runMaintainStatus() {
	m, err := newMaintainer()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus maintain status: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := m.Scan(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "nexus maintain status: scan: %v\n", err)
		os.Exit(1)
	}

	s := m.Status()
	fmt.Printf("platform:  %s\n", s.Platform)
	fmt.Printf("topology:  %v\n", s.TopologySet)
	fmt.Printf("learned:   %d fix records\n", s.LearnedCount)
	fmt.Printf("last scan: %s\n", s.LastScan.Local().Format(time.RFC3339))
	fmt.Println()

	if len(s.Tools) == 0 {
		fmt.Println("no tools tracked (run `nexus maintain watch` to start live monitoring)")
		return
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "TOOL\tSTATUS\tDRIFT\tHEALTH\tPROTOCOL")
	for _, row := range s.Tools {
		health := "✗"
		if row.Health {
			health = "✓"
		}
		proto := row.Protocol
		if proto == "" {
			proto = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\n",
			row.Name, row.Status, row.Drift, health, proto)
	}
	tw.Flush()
}

func runMaintainFix(toolName string) {
	m, err := newMaintainer()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus maintain fix: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Scan first so the twin has current state.
	if err := m.Scan(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "nexus maintain fix: scan: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("fixing %s...\n", toolName)
	if err := m.FixTool(ctx, toolName); err != nil {
		fmt.Fprintf(os.Stderr, "nexus maintain fix: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ %s converged\n", toolName)
}

func runMaintainWatch() {
	m, err := newMaintainer()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus maintain watch: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		cancel()
	}()

	if err := m.Start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "nexus maintain watch: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("watching... (Ctrl-C to stop)")
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			fmt.Println("stopped.")
			return
		case <-ticker.C:
			s := m.Status()
			fixed := 0
			for _, row := range s.Tools {
				if row.Status == "running" && row.Drift == 0 {
					fixed++
				}
			}
			fmt.Printf("[%s] tools=%d converged=%d learned=%d\n",
				time.Now().Local().Format("15:04:05"),
				len(s.Tools), fixed, s.LearnedCount)
		}
	}
}

func runMaintainRegistry() {
	m, err := newMaintainer()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus maintain registry: %v\n", err)
		os.Exit(1)
	}

	reg := m.Registry()
	connectors := reg.AllConnectors()

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tDISPLAY NAME\tKNOWN ISSUES")
	for _, c := range connectors {
		fmt.Fprintf(tw, "%s\t%s\t%d\n", c.Name, c.DisplayName, len(c.KnownIssues))
	}
	tw.Flush()
	merkle := reg.Merkle()
	fmt.Printf("\ntotal: %d connectors (merkle: %x)\n", reg.Len(), merkle[:])
}
