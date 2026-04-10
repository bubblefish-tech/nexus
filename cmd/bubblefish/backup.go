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
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/bubblefish-tech/nexus/internal/backup"
)

// runBackup dispatches to backup create or backup restore subcommands.
//
// Usage:
//
//	bubblefish backup create --dest /path [--include-db]
//	bubblefish backup restore --from /path [--force]
//
// Reference: Tech Spec Section 14.5, Phase R-24.
func runBackup(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: bubblefish backup <create|restore>")
		fmt.Fprintln(os.Stderr, "subcommands:")
		fmt.Fprintln(os.Stderr, "  create   create a backup of config, compiled, and WAL files")
		fmt.Fprintln(os.Stderr, "  restore  restore from a backup directory")
		os.Exit(1)
	}

	switch args[0] {
	case "create":
		runBackupCreate(args[1:])
	case "restore":
		runBackupRestore(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "bubblefish backup: unknown subcommand %q\n", args[0])
		os.Exit(1)
	}
}

// runBackupCreate implements `bubblefish backup create`.
func runBackupCreate(args []string) {
	fs := flag.NewFlagSet("bubblefish backup create", flag.ExitOnError)
	dest := fs.String("dest", "", "destination directory for the backup (required)")
	includeDB := fs.Bool("include-db", false, "include SQLite database snapshot via VACUUM INTO")

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	if *dest == "" {
		fmt.Fprintln(os.Stderr, "bubblefish backup create: --dest is required")
		fmt.Fprintln(os.Stderr, "usage: bubblefish backup create --dest /path [--include-db]")
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	if err := backup.Create(backup.CreateOptions{
		Dest:      *dest,
		IncludeDB: *includeDB,
		Logger:    logger,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish backup create: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("bubblefish backup create: ok")
}

// runBackupRestore implements `bubblefish backup restore`.
func runBackupRestore(args []string) {
	fs := flag.NewFlagSet("bubblefish backup restore", flag.ExitOnError)
	from := fs.String("from", "", "backup directory to restore from (required)")
	force := fs.Bool("force", false, "overwrite existing files")

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	if *from == "" {
		fmt.Fprintln(os.Stderr, "bubblefish backup restore: --from is required")
		fmt.Fprintln(os.Stderr, "usage: bubblefish backup restore --from /path [--force]")
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	if err := backup.Restore(backup.RestoreOptions{
		From:   *from,
		Force:  *force,
		Logger: logger,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish backup restore: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("bubblefish backup restore: ok")
}
