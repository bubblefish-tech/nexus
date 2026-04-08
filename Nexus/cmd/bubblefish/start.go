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
	"syscall"

	"github.com/BubbleFish-Nexus/internal/config"
	"github.com/BubbleFish-Nexus/internal/daemon"
	"github.com/BubbleFish-Nexus/internal/tray"
	"github.com/BubbleFish-Nexus/internal/version"
	"github.com/BubbleFish-Nexus/internal/web"
	dashboardui "github.com/BubbleFish-Nexus/web/dashboard"
)

// runStart executes the `bubblefish start` command.
//
// It loads configuration, starts the daemon (HTTP + MCP), launches the web
// dashboard, starts the system tray (Windows; headless: skipped with INFO),
// and blocks until SIGINT/SIGTERM.
//
// Reference: Tech Spec Section 13.1.
func runStart() {
	// Resolve config directory — os.UserHomeDir failure is fatal.
	configDir, err := config.ConfigDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish start: %v\n", err)
		os.Exit(1)
	}

	// Set up structured logger based on config (pre-load default).
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	// Load config.
	cfg, err := config.Load(configDir, logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish start: config load: %v\n", err)
		os.Exit(1)
	}

	// Re-create logger with configured level and format.
	logger = buildLogger(cfg)

	logger.Info("bubblefish start",
		"component", "main",
		"version", version.Version,
		"config_dir", configDir,
		"mode", cfg.Daemon.Mode,
	)

	// Create daemon.
	d := daemon.New(cfg, logger)

	// Start web dashboard in background (non-fatal on failure).
	var dashboard *web.Dashboard
	if cfg.Daemon.Web.Port > 0 {
		dashboard = web.New(web.Config{
			Port:             cfg.Daemon.Web.Port,
			RequireAuth:      cfg.Daemon.Web.RequireAuth,
			AdminKey:         cfg.ResolvedAdminKey,
			Logger:           logger,
			SecurityProvider: daemon.NewDashboardSecurityProvider(d),
			AuditProvider:    daemon.NewDashboardAuditProvider(d),
			AdminHandler:     d.BuildAdminRouter(),
			DashboardHTML:    dashboardui.HTML,
			LogoPNG:          dashboardui.LogoPNG,
		})
		go func() {
			if err := dashboard.Start(); err != nil {
				logger.Warn("bubblefish start: web dashboard failed — continuing without dashboard",
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
				// Tray "Stop" menu item triggers daemon shutdown.
				if err := d.Stop(); err != nil {
					slog.Error("tray: stop daemon", "error", err)
				}
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

	// Wait for signal, API shutdown request, or daemon error.
	select {
	case sig := <-sigCh:
		logger.Info("bubblefish start: received signal, shutting down",
			"component", "main",
			"signal", sig.String(),
		)
	case <-d.ShutdownRequested():
		logger.Info("bubblefish start: shutdown requested via API",
			"component", "main",
		)
	case err := <-daemonErr:
		if err != nil {
			logger.Error("bubblefish start: daemon exited with error",
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
		logger.Error("bubblefish start: shutdown error",
			"component", "main",
			"error", err,
		)
		os.Exit(1)
	}

	logger.Info("bubblefish start: shutdown complete",
		"component", "main",
	)
}

// buildLogger creates a slog.Logger from the daemon config's log_level and
// log_format settings.
func buildLogger(cfg *config.Config) *slog.Logger {
	var level slog.Level
	switch cfg.Daemon.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	switch cfg.Daemon.LogFormat {
	case "json":
		handler = slog.NewJSONHandler(os.Stderr, opts)
	default:
		handler = slog.NewTextHandler(os.Stderr, opts)
	}
	return slog.New(handler)
}
