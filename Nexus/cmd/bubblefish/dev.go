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
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/BubbleFish-Nexus/internal/config"
	"github.com/BubbleFish-Nexus/internal/daemon"
	"github.com/BubbleFish-Nexus/internal/tray"
	"github.com/BubbleFish-Nexus/internal/version"
	"github.com/BubbleFish-Nexus/internal/web"
)

// runDev executes the `bubblefish dev` command.
//
// It starts the same daemon as `bubblefish start` with dev-friendly defaults:
// log_level=debug, auto-reload enabled, and all effective config paths printed
// at startup. Does NOT change pipeline semantics — no dev-only code paths.
//
// Reference: Tech Spec Section 13.1.
func runDev() {
	// Resolve config directory — os.UserHomeDir failure is fatal.
	configDir, err := config.ConfigDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish dev: %v\n", err)
		os.Exit(1)
	}

	// Set up structured logger based on config (pre-load default).
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	// Load config.
	cfg, err := config.Load(configDir, logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish dev: config load: %v\n", err)
		os.Exit(1)
	}

	// Override log_level to debug for dev mode.
	cfg.Daemon.LogLevel = "debug"

	// Re-create logger with debug level.
	logger = buildLogger(cfg)

	logger.Info("bubblefish dev",
		"component", "main",
		"version", version.Version,
		"config_dir", configDir,
		"mode", cfg.Daemon.Mode,
	)

	// Print all effective config paths for developer convenience.
	printConfigPaths(logger, configDir)

	// Create daemon — same pipeline as start, no dev-only code paths.
	d := daemon.New(cfg, logger)

	// Start web dashboard in background (non-fatal on failure).
	var dashboard *web.Dashboard
	if cfg.Daemon.Web.Port > 0 {
		dashboard = web.New(web.Config{
			Port:        cfg.Daemon.Web.Port,
			RequireAuth: cfg.Daemon.Web.RequireAuth,
			AdminKey:    cfg.ResolvedAdminKey,
			Logger:      logger,
		})
		go func() {
			if err := dashboard.Start(); err != nil {
				logger.Warn("bubblefish dev: web dashboard failed — continuing without dashboard",
					"component", "main",
					"error", err,
				)
			}
		}()
	}

	// Start system tray in background (non-fatal; headless: skipped with INFO).
	trayDone := make(chan struct{})
	go func() {
		defer close(trayDone)
		tray.Run(tray.Config{
			DaemonPort:    cfg.Daemon.Port,
			DashboardPort: cfg.Daemon.Web.Port,
			Logger:        logger,
			OnStop: func() {
				d.Stop()
			},
		})
	}()

	// Signal handling — SIGINT/SIGTERM triggers graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Start daemon in background.
	daemonErr := make(chan error, 1)
	go func() {
		daemonErr <- d.Start()
	}()

	// Wait for signal or daemon error.
	select {
	case sig := <-sigCh:
		logger.Info("bubblefish dev: received signal, shutting down",
			"component", "main",
			"signal", sig.String(),
		)
	case err := <-daemonErr:
		if err != nil {
			logger.Error("bubblefish dev: daemon exited with error",
				"component", "main",
				"error", err,
			)
		}
	}

	// Graceful shutdown.
	if dashboard != nil {
		dashboard.Stop()
	}
	tray.Quit()

	if err := d.Stop(); err != nil {
		logger.Error("bubblefish dev: shutdown error",
			"component", "main",
			"error", err,
		)
		os.Exit(1)
	}

	logger.Info("bubblefish dev: shutdown complete",
		"component", "main",
	)
}

// printConfigPaths logs all effective config paths at INFO level so the
// developer can see exactly which files are in play.
func printConfigPaths(logger *slog.Logger, configDir string) {
	logger.Info("effective config paths",
		"component", "main",
		"daemon_toml", filepath.Join(configDir, "daemon.toml"),
		"sources_dir", filepath.Join(configDir, "sources"),
		"destinations_dir", filepath.Join(configDir, "destinations"),
		"compiled_dir", filepath.Join(configDir, "compiled"),
	)
}
