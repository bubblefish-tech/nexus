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
	"path/filepath"
	"strings"

	"github.com/bubblefish-tech/nexus/internal/config"
	"github.com/bubblefish-tech/nexus/internal/destination"
)

// runDestination dispatches to destination subcommands.
//
// Usage:
//
//	nexus destination rebuild [--name NAME] [--no-backup]
//
// Reference: v0.1.3 Build Plan Section 6.6.
func runDestination(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: nexus destination <subcommand>")
		fmt.Fprintln(os.Stderr, "subcommands:")
		fmt.Fprintln(os.Stderr, "  rebuild  replay WAL into a fresh destination database")
		os.Exit(1)
	}

	switch args[0] {
	case "rebuild":
		runDestinationRebuild(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "nexus destination: unknown subcommand %q\n", args[0])
		os.Exit(1)
	}
}

// runDestinationRebuild implements `nexus destination rebuild`.
func runDestinationRebuild(args []string) {
	fs := flag.NewFlagSet("nexus destination rebuild", flag.ExitOnError)
	name := fs.String("name", "", "destination name filter (optional — empty rebuilds all)")
	walDir := fs.String("wal-dir", "", "WAL directory (default: from config)")
	dbPath := fs.String("db-path", "", "SQLite database path (default: from config)")
	noBackup := fs.Bool("no-backup", false, "skip backing up the old database file")

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	// Resolve defaults from config if not provided.
	if *walDir == "" || *dbPath == "" {
		configDir, err := config.ConfigDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "nexus destination rebuild: %v\n", err)
			os.Exit(1)
		}
		cfg, err := config.Load(configDir, logger)
		if err != nil {
			fmt.Fprintf(os.Stderr, "nexus destination rebuild: config load: %v\n", err)
			os.Exit(1)
		}

		if *walDir == "" {
			wp := cfg.Daemon.WAL.Path
			if wp == "" {
				wp = filepath.Join(configDir, "wal")
			}
			if strings.HasPrefix(wp, "~/") || strings.HasPrefix(wp, "~\\") {
				home, _ := os.UserHomeDir()
				wp = filepath.Join(home, wp[2:])
			}
			*walDir = wp
		}

		if *dbPath == "" {
			// Default SQLite path is ~/.nexus/Nexus/nexus.db
			home, _ := os.UserHomeDir()
			*dbPath = filepath.Join(home, ".nexus", "Nexus", "nexus.db")
		}
	}

	fmt.Fprintf(os.Stderr, "nexus destination rebuild: WAL=%s DB=%s\n", *walDir, *dbPath)

	result, err := destination.Rebuild(destination.RebuildOptions{
		WALDir:    *walDir,
		DestPath:  *dbPath,
		DestName:  *name,
		BackupOld: !*noBackup,
		Logger:    logger,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus destination rebuild: %v\n", err)
		os.Exit(1)
	}

	out, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(out))
	fmt.Fprintf(os.Stderr, "nexus destination rebuild: ok — %d entries written (%s)\n",
		result.EntriesWritten, result.Duration)
}
