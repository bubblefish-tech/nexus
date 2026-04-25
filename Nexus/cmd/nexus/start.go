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
	"crypto/rand"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/bubblefish-tech/nexus/internal/config"
	"github.com/bubblefish-tech/nexus/internal/crypto"
	"github.com/bubblefish-tech/nexus/internal/daemon"
	"github.com/bubblefish-tech/nexus/internal/logging"
	"github.com/bubblefish-tech/nexus/internal/tray"
	"github.com/bubblefish-tech/nexus/internal/version"
	"github.com/bubblefish-tech/nexus/internal/web"
	"github.com/shirou/gopsutil/v3/mem"
	_ "go.uber.org/automaxprocs"
	"gopkg.in/lumberjack.v2"
	dashboardui "github.com/bubblefish-tech/nexus/web/dashboard"
)

// runStart executes the `nexus start` command.
//
// It loads configuration, starts the daemon (HTTP + MCP), launches the web
// dashboard, starts the system tray (Windows; headless: skipped with INFO),
// and blocks until SIGINT/SIGTERM.
//
// Reference: Tech Spec Section 13.1.
func runStart() {
	// Startup jitter: prevent thundering-herd when multiple services start at login/boot.
	n, _ := rand.Int(rand.Reader, big.NewInt(int64(500*time.Millisecond)))
	if n != nil {
		time.Sleep(time.Duration(n.Int64()))
	}

	// Runtime governor: GODEBUG + GOMEMLIMIT.
	setGodebugDefaults()
	setMemoryGovernor()

	// Entropy pool check: verify crypto/rand is responsive.
	t0 := time.Now()
	buf := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		slog.Error("crypto/rand failed", "err", err)
		os.Exit(1)
	}
	if d := time.Since(t0); d > time.Second {
		slog.Warn("entropy pool slow — crypto/rand took over 1s", "duration", d)
	}

	// Resolve config directory — os.UserHomeDir failure is fatal.
	configDir, err := config.ConfigDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus start: %v\n", err)
		os.Exit(1)
	}

	// Acquire instance lock — prevents two daemons from running simultaneously.
	fl, err := daemon.AcquireLock(configDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus start: %v\n", err)
		os.Exit(1)
	}
	defer fl.Unlock()

	// Auto-run doctor checks: CRITICAL blocks startup, WARN is logged.
	results := RunAllChecks(configDir)
	criticals := results.Criticals()
	if len(criticals) > 0 {
		for _, c := range criticals {
			slog.Error("pre-flight check CRITICAL", "check", c.Name, "message", c.Message)
		}
		fmt.Fprintf(os.Stderr, "nexus doctor found %d critical issues — run `nexus doctor` for details\n", len(criticals))
		os.Exit(1)
	}
	for _, w := range results.Warnings() {
		slog.Warn("pre-flight check warning", "check", w.Name, "message", w.Message)
	}

	// Set up structured logger based on config (pre-load default).
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	// Derive master key before loading config so ENC:v1: fields can be decrypted.
	// Non-fatal: if no password is configured, mkm is disabled and plaintext config is used.
	var mkm *crypto.MasterKeyManager
	if home, homeErr := os.UserHomeDir(); homeErr == nil {
		saltPath := filepath.Join(home, ".nexus", "crypto.salt")
		if m, mkmErr := crypto.NewMasterKeyManager("", saltPath); mkmErr == nil {
			mkm = m
		}
	}

	// Load config, decrypting any ENC:v1: fields with the config sub-key.
	cfg, err := config.LoadWithKey(configDir, logger, mkm)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus start: config load: %v\n", err)
		os.Exit(1)
	}

	// Re-create logger with configured level, format, and file output.
	logger = buildLogger(cfg, configDir)

	logger.Info("nexus start",
		"component", "main",
		"version", version.Version,
		"config_dir", configDir,
		"mode", cfg.Daemon.Mode,
	)

	// Create daemon.
	d := daemon.New(cfg, logger)

	// Resolve dashboard TLS cert/key (CU.0.7). Dashboard serves HTTPS by default
	// unless tls_disabled = true. Auto-generates ~/.nexus/keys/tls.crt when no
	// operator cert is configured.
	var dashCertFile, dashKeyFile string
	if cfg.Daemon.Web.Port > 0 && !cfg.Daemon.Web.TLSDisabled {
		if cfg.Daemon.Web.TLSCertFile != "" && cfg.Daemon.Web.TLSKeyFile != "" {
			dashCertFile = cfg.Daemon.Web.TLSCertFile
			dashKeyFile = cfg.Daemon.Web.TLSKeyFile
		} else if home, homeErr := os.UserHomeDir(); homeErr == nil {
			keysDir := filepath.Join(home, ".nexus", "keys")
			c, k, certErr := daemon.EnsureAutoTLSCert(keysDir)
			if certErr != nil {
				logger.Warn("nexus start: auto TLS cert generation failed — dashboard using HTTP",
					"component", "main", "error", certErr)
			} else {
				dashCertFile = c
				dashKeyFile = k
				logger.Info("nexus start: dashboard TLS cert ready",
					"component", "main", "cert", dashCertFile)
			}
		}
	}

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
			TLSCertFile:      dashCertFile,
			TLSKeyFile:       dashKeyFile,
		})
		go func() {
			if err := dashboard.Start(); err != nil {
				logger.Warn("nexus start: web dashboard failed — continuing without dashboard",
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
		logger.Info("nexus start: received signal, shutting down",
			"component", "main",
			"signal", sig.String(),
		)
	case <-d.ShutdownRequested():
		logger.Info("nexus start: shutdown requested via API",
			"component", "main",
		)
	case err := <-daemonErr:
		if err != nil {
			logger.Error("nexus start: daemon exited with error",
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
		logger.Error("nexus start: shutdown error",
			"component", "main",
			"error", err,
		)
		os.Exit(1)
	}

	logger.Info("nexus start: shutdown complete",
		"component", "main",
	)
}

// buildLogger creates a slog.Logger from the daemon config's log_level and
// log_format settings. When configDir is non-empty it also opens
// <configDir>/logs/nexus.log for structured JSON append logging.
func buildLogger(cfg *config.Config, configDir string) *slog.Logger {
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

	// Stderr handler — format follows config.
	var stderrHandler slog.Handler
	switch cfg.Daemon.LogFormat {
	case "json":
		stderrHandler = slog.NewJSONHandler(os.Stderr, opts)
	default:
		stderrHandler = slog.NewTextHandler(os.Stderr, opts)
	}

	// File handler — always JSON, for nexus logs command to parse.
	// Uses lumberjack for automatic rotation: 100 MiB max, 5 backups, 30 day retention.
	if configDir != "" {
		logPath := filepath.Join(configDir, "logs", "nexus.log")
		fileWriter := &lumberjack.Logger{
			Filename:   logPath,
			MaxSize:    100,
			MaxBackups: 5,
			MaxAge:     30,
			Compress:   true,
		}
		fileHandler := slog.NewJSONHandler(fileWriter, &slog.HandlerOptions{Level: slog.LevelDebug})
		return slog.New(logging.NewSanitizingHandler(newTeeHandler(stderrHandler, fileHandler)))
	}

	return slog.New(logging.NewSanitizingHandler(stderrHandler))
}

// teeHandler fans out a slog.Record to two handlers.
// Errors from the secondary handler are silently ignored to avoid breaking
// the primary (stderr) path if the log file becomes unavailable.
type teeHandler struct {
	primary   slog.Handler
	secondary slog.Handler
}

func newTeeHandler(primary, secondary slog.Handler) *teeHandler {
	return &teeHandler{primary: primary, secondary: secondary}
}

func (h *teeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.primary.Enabled(ctx, level)
}

func (h *teeHandler) Handle(ctx context.Context, r slog.Record) error {
	_ = h.secondary.Handle(ctx, r.Clone())
	return h.primary.Handle(ctx, r)
}

func (h *teeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &teeHandler{
		primary:   h.primary.WithAttrs(attrs),
		secondary: h.secondary.WithAttrs(attrs),
	}
}

func (h *teeHandler) WithGroup(name string) slog.Handler {
	return &teeHandler{
		primary:   h.primary.WithGroup(name),
		secondary: h.secondary.WithGroup(name),
	}
}

// setMemoryGovernor sets GOMEMLIMIT to 75% of system RAM, floored at 512 MiB,
// capped at 8 GiB. Respects operator override via GOMEMLIMIT env var.
func setMemoryGovernor() {
	if os.Getenv("GOMEMLIMIT") != "" {
		return
	}
	v, err := mem.VirtualMemory()
	if err != nil || v.Total == 0 {
		slog.Warn("could not read system memory; GOMEMLIMIT not set")
		return
	}
	limit := int64(v.Total) * 3 / 4
	const floor = int64(512) << 20
	const cap_ = int64(8) << 30
	if limit < floor {
		limit = floor
	}
	if limit > cap_ {
		limit = cap_
	}
	debug.SetMemoryLimit(limit)
	slog.Info("memory governor set", "limit_gib", float64(limit)/float64(int64(1)<<30), "system_gib", float64(v.Total)/float64(int64(1)<<30))
}

// setGodebugDefaults sets GODEBUG=madvdontneed=1 to return freed memory to OS.
func setGodebugDefaults() {
	if os.Getenv("GODEBUG") == "" {
		os.Setenv("GODEBUG", "madvdontneed=1")
	}
}
