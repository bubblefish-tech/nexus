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

	"github.com/bubblefish-tech/nexus/internal/config"
	"github.com/bubblefish-tech/nexus/internal/tui"
	"github.com/bubblefish-tech/nexus/internal/tui/api"
	"github.com/bubblefish-tech/nexus/internal/tui/tabs"
	tea "github.com/charmbracelet/bubbletea"
)

func runTUI() {
	configDir, err := config.ConfigDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus tui: %v\n", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	cfg, err := config.Load(configDir, logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus tui: failed to load config: %v\n", err)
		os.Exit(1)
	}

	bindHost := cfg.Daemon.Bind
	if bindHost == "0.0.0.0" || bindHost == "::" {
		bindHost = "127.0.0.1"
	}
	addr := fmt.Sprintf("http://%s:%d", bindHost, cfg.Daemon.Port)
	if cfg.Daemon.Bind != "127.0.0.1" && cfg.Daemon.Bind != "localhost" && cfg.Daemon.Bind != "0.0.0.0" && !cfg.Daemon.TLS.Enabled {
		slog.Warn("admin key will be sent over plain HTTP (TLS not enabled, bind is not loopback)",
			"bind", cfg.Daemon.Bind)
	}
	client := api.NewClient(addr, string(cfg.ResolvedAdminKey))
	defer client.Close()

	tabList := []tabs.Tab{
		tabs.NewControlTab(),
		tabs.NewAuditTab(),
		tabs.NewSecurityTab(),
		tabs.NewPipelineTab(),
		tabs.NewConflictsTab(),
		tabs.NewTimeTravelTab(),
		tabs.NewSettingsTab(),
	}

	prefs, err := tui.LoadPrefs(configDir)
	if err != nil {
		slog.Warn("failed to load tui prefs, using defaults", "err", err)
	}

	app := tui.NewRunningApp(client, tabList, prefs)

	p := tea.NewProgram(app,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "nexus tui: %v\n", err)
		os.Exit(1)
	}
}
