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
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/bubblefish-tech/nexus/internal/config"
)

// stopOptions collects resolved parameters so the core logic can be tested
// without os.Exit or real network calls.
type stopOptions struct {
	configDir string
	timeout   int
	stdout    io.Writer
	stderr    io.Writer
	client    *http.Client
}

// runStop executes the `bubblefish stop` command.
func runStop(args []string) {
	fs := flag.NewFlagSet("stop", flag.ExitOnError)
	timeout := fs.Int("timeout", 30, "seconds to wait for daemon to exit")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish stop: %v\n", err)
		os.Exit(1)
	}

	configDir, err := config.ConfigDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish stop: %v\n", err)
		os.Exit(1)
	}

	opts := stopOptions{
		configDir: configDir,
		timeout:   *timeout,
		stdout:    os.Stdout,
		stderr:    os.Stderr,
		client:    &http.Client{},
	}

	code := doStop(opts)
	os.Exit(code)
}

// doStop is the testable core of runStop. Returns the exit code.
func doStop(opts stopOptions) int {
	logger := slog.New(slog.NewTextHandler(opts.stderr, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	}))

	cfg, err := config.Load(opts.configDir, logger)
	if err != nil {
		fmt.Fprintf(opts.stderr, "bubblefish stop: no config found at %s. Run 'bubblefish install' first.\n", opts.configDir)
		return 1
	}

	bind := fmt.Sprintf("%s:%d", cfg.Daemon.Bind, cfg.Daemon.Port)
	healthURL := fmt.Sprintf("http://%s/health", bind)
	shutdownURL := fmt.Sprintf("http://%s/api/shutdown", bind)

	// Check if daemon is running.
	healthClient := &http.Client{Timeout: 1 * time.Second}
	_, err = healthClient.Get(healthURL)
	if err != nil {
		fmt.Fprintf(opts.stdout, "bubblefish stop: no daemon running at %s\n", bind)
		return 0
	}

	// Daemon is running — send shutdown request.
	client := opts.client
	client.Timeout = 5 * time.Second

	req, err := http.NewRequest(http.MethodPost, shutdownURL, nil)
	if err != nil {
		fmt.Fprintf(opts.stderr, "bubblefish stop: %v\n", err)
		return 1
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Daemon.AdminToken)

	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(opts.stderr, "bubblefish stop: failed to send shutdown request: %v\n", err)
		return 1
	}
	_ = resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		fmt.Fprintf(opts.stderr, "bubblefish stop: admin token rejected. Check that the daemon was started from the same config directory.\n")
		return 1
	}

	if resp.StatusCode != http.StatusAccepted {
		fmt.Fprintf(opts.stderr, "bubblefish stop: unexpected response %d from /api/shutdown\n", resp.StatusCode)
		return 1
	}

	fmt.Fprintf(opts.stdout, "bubblefish stop: shutdown requested, waiting for daemon to exit...\n")

	// Poll /health once per second until the daemon stops responding.
	pollClient := &http.Client{Timeout: 1 * time.Second}
	deadline := time.Now().Add(time.Duration(opts.timeout) * time.Second)

	for time.Now().Before(deadline) {
		time.Sleep(1 * time.Second)
		_, err := pollClient.Get(healthURL)
		if err != nil {
			// Connection refused — daemon has exited.
			fmt.Fprintf(opts.stdout, "bubblefish stop: daemon stopped cleanly\n")
			return 0
		}
	}

	fmt.Fprintf(opts.stderr, "bubblefish stop: daemon did not exit within %d seconds. You may need to kill it manually with: taskkill /F /IM bubblefish.exe (Windows) or kill -9 <pid> (POSIX).\n", opts.timeout)
	return 1
}
