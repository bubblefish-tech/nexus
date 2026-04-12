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
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/BubbleFish-Nexus/internal/config"
	"github.com/BubbleFish-Nexus/internal/doctor"
)

// runDoctor executes the `bubblefish doctor` command.
//
// It loads configuration and runs health checks, including OAuth-specific
// checks when [daemon.oauth] is enabled.
//
// Reference: Post-Build Add-On Update Technical Specification Section 6.4.
func runDoctor() {
	// Handle --fsync-test flag before loading config.
	for _, arg := range os.Args[2:] {
		if arg == "--fsync-test" {
			runFsyncTest()
			return
		}
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	}))

	configDir, err := config.ConfigDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish doctor: %v\n", err)
		os.Exit(1)
	}

	cfg, err := config.Load(configDir, logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish doctor: config load failed: %v\n", err)
		os.Exit(1)
	}

	hasErrors := false

	fmt.Println("bubblefish doctor: checking configuration...")

	// OAuth checks.
	if cfg.Daemon.OAuth.Enabled {
		fmt.Println("  [oauth] enabled = true")

		// issuer_url must not be empty.
		if cfg.Daemon.OAuth.IssuerURL == "" {
			fmt.Println("  [ERROR] oauth.issuer_url is empty")
			hasErrors = true
		} else {
			fmt.Printf("  [ok]    oauth.issuer_url = %s\n", cfg.Daemon.OAuth.IssuerURL)
		}

		// issuer_url should use HTTPS (except localhost).
		if cfg.Daemon.OAuth.IssuerURL != "" &&
			!strings.HasPrefix(cfg.Daemon.OAuth.IssuerURL, "https://") {
			if !strings.Contains(cfg.Daemon.OAuth.IssuerURL, "localhost") &&
				!strings.Contains(cfg.Daemon.OAuth.IssuerURL, "127.0.0.1") {
				fmt.Println("  [WARN]  oauth.issuer_url should use HTTPS")
			}
		}

		// private_key_file must be resolvable.
		pkf := cfg.Daemon.OAuth.PrivateKeyFile
		if pkf == "" {
			fmt.Println("  [WARN]  oauth.private_key_file is empty (will auto-generate on start)")
		} else if strings.HasPrefix(pkf, "file:") {
			path := strings.TrimPrefix(pkf, "file:")
			if _, statErr := os.Stat(path); statErr != nil {
				fmt.Printf("  [ERROR] oauth.private_key_file not found: %s\n", path)
				hasErrors = true
			} else {
				fmt.Printf("  [ok]    oauth.private_key_file exists: %s\n", path)
			}
		}

		// clients check.
		if len(cfg.Daemon.OAuth.Clients) == 0 {
			fmt.Println("  [WARN]  oauth: no clients registered")
		} else {
			fmt.Printf("  [ok]    oauth: %d client(s) registered\n", len(cfg.Daemon.OAuth.Clients))
		}
	} else {
		fmt.Println("  [ok]    oauth: disabled (no OAuth endpoints registered)")
	}

	if hasErrors {
		fmt.Println("\nbubblefish doctor: issues found")
		os.Exit(1)
	}
	fmt.Println("\nbubblefish doctor: ok")
}

// runFsyncTest executes `bubblefish doctor --fsync-test`.
// Writes data, fsyncs, reads back via fresh fd, and verifies the bytes match.
// Detects broken fsync on network storage and some consumer SSDs.
//
// Reference: v0.1.3 Build Plan Phase 1 Subtask 1.6.
func runFsyncTest() {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish doctor --fsync-test: %v\n", err)
		os.Exit(1)
	}
	walDir := filepath.Join(home, ".bubblefish", "Nexus", "wal")
	if err := os.MkdirAll(walDir, 0700); err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish doctor --fsync-test: create WAL dir: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("bubblefish doctor --fsync-test: testing fsync in %s\n", walDir)
	result := doctor.FsyncTest(walDir)
	if result.OK {
		fmt.Printf("bubblefish doctor --fsync-test: ok — fsync verified in %s\n", result.Duration)
	} else {
		fmt.Fprintf(os.Stderr, "bubblefish doctor --fsync-test: FAIL — %s\n", result.Error)
		fmt.Fprintln(os.Stderr, "  WARNING: fsync may not be flushing data to durable storage.")
		fmt.Fprintln(os.Stderr, "  This filesystem may silently lose data on power failure.")
		os.Exit(1)
	}
}
