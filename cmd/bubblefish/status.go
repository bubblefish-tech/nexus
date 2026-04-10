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
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bubblefish-tech/nexus/internal/config"
	"github.com/bubblefish-tech/nexus/internal/version"
)

// statusOptions collects resolved parameters so the core logic can be tested
// without os.Exit or real network calls.
type statusOptions struct {
	configDir string
	stdout    io.Writer
	stderr    io.Writer
}

// runStatus executes the `bubblefish status` command.
func runStatus(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	paths := fs.Bool("paths", false, "print all resolved config and data file paths with existence checks, then exit")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish status: %v\n", err)
		os.Exit(1)
	}

	configDir, err := config.ConfigDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish status: %v\n", err)
		os.Exit(1)
	}

	opts := statusOptions{
		configDir: configDir,
		stdout:    os.Stdout,
		stderr:    os.Stderr,
	}

	if *paths {
		if err := doStatusPaths(opts); err != nil {
			fmt.Fprintf(os.Stderr, "bubblefish status --paths: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if err := doStatusDefault(opts); err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish status: %v\n", err)
		os.Exit(1)
	}
}

// doStatusDefault prints a brief daemon health summary.
func doStatusDefault(opts statusOptions) error {
	logger := slog.New(slog.NewTextHandler(opts.stderr, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	}))

	cfg, err := config.Load(opts.configDir, logger)
	if err != nil {
		return fmt.Errorf("no config found at %s. Run 'bubblefish install' first", opts.configDir)
	}

	bind := fmt.Sprintf("%s:%d", cfg.Daemon.Bind, cfg.Daemon.Port)

	fmt.Fprintf(opts.stdout, "bubblefish status (v%s)\n\n", version.Version)

	// Try /health with 2-second timeout.
	client := &http.Client{Timeout: 2 * time.Second}
	healthURL := fmt.Sprintf("http://%s/health", bind)
	resp, err := client.Get(healthURL)
	if err != nil {
		// Daemon is not running — graceful degradation.
		fmt.Fprintf(opts.stdout, "  config dir:      %s\n", opts.configDir)
		fmt.Fprintf(opts.stdout, "  mode:            %s\n", cfg.Daemon.Mode)
		fmt.Fprintf(opts.stdout, "  bind:            %s\n", bind)
		fmt.Fprintf(opts.stdout, "\n  daemon:          not running\n")
		fmt.Fprintf(opts.stdout, "\n  Start with: bubblefish start\n")
		fmt.Fprintf(opts.stdout, "  For path information, run: bubblefish status --paths\n")
		return nil
	}
	_ = resp.Body.Close()

	healthOK := resp.StatusCode == http.StatusOK

	// Try /ready.
	readyURL := fmt.Sprintf("http://%s/ready", bind)
	readyResp, err := client.Get(readyURL)
	readyOK := err == nil && readyResp != nil && readyResp.StatusCode == http.StatusOK
	if readyResp != nil {
		_ = readyResp.Body.Close()
	}

	fmt.Fprintf(opts.stdout, "  daemon:          running at %s\n", bind)
	fmt.Fprintf(opts.stdout, "  health:          %s\n", boolStatus(healthOK))
	fmt.Fprintf(opts.stdout, "  ready:           %s\n", boolStatus(readyOK))

	// Try /api/status with admin token, 5-second timeout.
	apiClient := &http.Client{Timeout: 5 * time.Second}
	statusURL := fmt.Sprintf("http://%s/api/status", bind)
	req, err := http.NewRequest(http.MethodGet, statusURL, nil)
	if err != nil {
		fmt.Fprintf(opts.stdout, "\n  api:             unreachable (request error)\n")
		fmt.Fprintf(opts.stdout, "\n  For path information, run: bubblefish status --paths\n")
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Daemon.AdminToken)

	apiResp, err := apiClient.Do(req)
	if err != nil || apiResp.StatusCode != http.StatusOK {
		if apiResp != nil {
			_ = apiResp.Body.Close()
		}
		fmt.Fprintf(opts.stdout, "\n  api:             unreachable (check admin token)\n")
		fmt.Fprintf(opts.stdout, "\n  For path information, run: bubblefish status --paths\n")
		return nil
	}
	defer func() { _ = apiResp.Body.Close() }()

	var status struct {
		QueueDepth  int     `json:"queue_depth"`
		WALPending  int     `json:"wal_pending"`
		Consistency float64 `json:"consistency"`
		Destination struct {
			Type      string `json:"type"`
			Reachable bool   `json:"reachable"`
		} `json:"destination"`
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(apiResp.Body).Decode(&status); err != nil {
		fmt.Fprintf(opts.stdout, "\n  api:             invalid response\n")
		fmt.Fprintf(opts.stdout, "\n  For path information, run: bubblefish status --paths\n")
		return nil
	}

	destStatus := fmt.Sprintf("%s (reachable)", status.Destination.Type)
	if !status.Destination.Reachable {
		destStatus = fmt.Sprintf("%s (unreachable)", status.Destination.Type)
	}

	fmt.Fprintf(opts.stdout, "\n  queue depth:     %d\n", status.QueueDepth)
	fmt.Fprintf(opts.stdout, "  wal pending:     %d\n", status.WALPending)
	fmt.Fprintf(opts.stdout, "  consistency:     %.2f\n", status.Consistency)
	fmt.Fprintf(opts.stdout, "\n  destination:     %s\n", destStatus)
	fmt.Fprintf(opts.stdout, "  mode:            %s\n", status.Mode)
	fmt.Fprintf(opts.stdout, "\n  For path information, run: bubblefish status --paths\n")

	return nil
}

// doStatusPaths prints all resolved config and data file paths with existence
// checks. This is the primary diagnostic output of the status command.
func doStatusPaths(opts statusOptions) error {
	logger := slog.New(slog.NewTextHandler(opts.stderr, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	}))

	cfg, err := config.Load(opts.configDir, logger)
	if err != nil {
		return fmt.Errorf("no config found at %s. Run 'bubblefish install' first", opts.configDir)
	}

	fmt.Fprintf(opts.stdout, "bubblefish paths (v%s)\n\n", version.Version)

	// Core paths.
	printPathLine(opts.stdout, "config directory:", opts.configDir)
	printPathLine(opts.stdout, "daemon config:", filepath.Join(opts.configDir, "daemon.toml"))
	printDirLine(opts.stdout, "sources directory:", filepath.Join(opts.configDir, "sources"))
	printDirLine(opts.stdout, "destinations dir:", filepath.Join(opts.configDir, "destinations"))

	fmt.Fprintln(opts.stdout)

	// WAL path.
	walPath, err := expandTilde(cfg.Daemon.WAL.Path)
	if err != nil {
		return fmt.Errorf("cannot resolve home directory (~): %w", err)
	}
	printPathLine(opts.stdout, "wal:", walPath)

	// Destination paths.
	for _, dst := range cfg.Destinations {
		switch dst.Type {
		case "sqlite":
			dbPath, err := expandTilde(dst.DBPath)
			if err != nil {
				return fmt.Errorf("cannot resolve home directory (~): %w", err)
			}
			printPathLine(opts.stdout, "sqlite database:", dbPath)
		case "postgres":
			redacted := redactDSN(dst.DSN)
			fmt.Fprintf(opts.stdout, "  %-20s %s  (remote)\n", "postgres:", redacted)
		case "openbrain":
			fmt.Fprintf(opts.stdout, "  %-20s %s  (remote)\n", "openbrain:", dst.URL)
		}
	}

	// Security log (only if configured).
	if cfg.SecurityEvents.LogFile != "" {
		secPath, err := expandTilde(cfg.SecurityEvents.LogFile)
		if err != nil {
			return fmt.Errorf("cannot resolve home directory (~): %w", err)
		}
		printPathLine(opts.stdout, "security log:", secPath)
	}

	// Audit log (only if configured).
	if cfg.Daemon.Audit.LogFile != "" {
		auditPath, err := expandTilde(cfg.Daemon.Audit.LogFile)
		if err != nil {
			return fmt.Errorf("cannot resolve home directory (~): %w", err)
		}
		printPathLine(opts.stdout, "audit log:", auditPath)
	}

	// BUBBLEFISH_HOME line.
	fmt.Fprintln(opts.stdout)
	if env := os.Getenv("BUBBLEFISH_HOME"); env != "" {
		fmt.Fprintf(opts.stdout, "BUBBLEFISH_HOME:       %s\n", env)
	} else {
		fmt.Fprintf(opts.stdout, "BUBBLEFISH_HOME:       (unset)\n")
	}

	return nil
}

// printPathLine prints a labeled path with an existence check.
func printPathLine(w io.Writer, label, path string) {
	info, err := os.Stat(path)
	_ = info
	if err != nil {
		fmt.Fprintf(w, "  %-20s %s  (does not exist)\n", label, path)
	} else {
		fmt.Fprintf(w, "  %-20s %s  (exists)\n", label, path)
	}
}

// printDirLine prints a labeled directory path with existence and file count.
func printDirLine(w io.Writer, label, path string) {
	entries, err := os.ReadDir(path)
	if err != nil {
		fmt.Fprintf(w, "  %-20s %s  (does not exist)\n", label, path)
		return
	}

	count := 0
	for _, e := range entries {
		if !e.IsDir() {
			count++
		}
	}

	noun := "files"
	if count == 1 {
		noun = "file"
	}
	fmt.Fprintf(w, "  %-20s %s  (exists, %d %s)\n", label, path, count, noun)
}

// expandTilde replaces a leading ~ with the user's home directory.
// Errors from os.UserHomeDir() must be propagated, not swallowed. See v0.1.0 audit finding M2.
func expandTilde(p string) (string, error) {
	if !strings.HasPrefix(p, "~") {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("expand tilde in %q: %w", p, err)
	}
	return filepath.Join(home, p[1:]), nil
}

// redactDSN extracts only the host:port from a PostgreSQL DSN, hiding
// credentials and database name.
func redactDSN(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return "(invalid DSN)"
	}
	return fmt.Sprintf("postgres://%s", u.Host)
}

// boolStatus returns "ok" or "error" for display.
func boolStatus(ok bool) string {
	if ok {
		return "ok"
	}
	return "error"
}
