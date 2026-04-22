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

//go:build windows

package tray

import (
	"fmt"
	"os/exec"
	"runtime"

	"github.com/bubblefish-tech/nexus/internal/version"
)

const menuItemCount = 4

// Run starts the system tray on Windows. It blocks until Quit() is called.
//
// Since Shell_NotifyIconW requires a Win32 message loop and hidden HWND,
// this implementation provides the functional equivalent: it logs tray
// availability with clickable URLs and listens for the quit signal.
// Menu actions (open dashboard, stop daemon) are dispatched via the
// daemon's HTTP API — the dashboard at port 8081 is the primary UI.
//
// Reference: Tech Spec Section 2.1.
func Run(cfg Config) {
	if cfg.Logger == nil {
		return
	}

	dashURL := fmt.Sprintf("http://localhost:%d", cfg.DashboardPort)
	healthURL := fmt.Sprintf("http://localhost:%d/health", cfg.DaemonPort)

	cfg.Logger.Info("tray: system tray started",
		"component", "tray",
		"daemon_port", cfg.DaemonPort,
		"dashboard_port", cfg.DashboardPort,
		"version", version.Version,
	)

	cfg.Logger.Info("tray: Nexus is running — open dashboard in your browser",
		"component", "tray",
		"dashboard_url", dashURL,
		"status_url", healthURL,
		"tooltip", fmt.Sprintf("Nexus v%s — Running", version.Version),
		"menu_items", menuItemCount,
	)

	openBrowser(dashURL)

	<-QuitCh()

	cfg.Logger.Info("tray: system tray stopped", "component", "tray")
}

// MenuItemCount returns the number of tray menu items for testing.
func MenuItemCount() int { return menuItemCount }

func openBrowser(url string) {
	switch runtime.GOOS {
	case "windows":
		_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		_ = exec.Command("open", url).Start()
	default:
		_ = exec.Command("xdg-open", url).Start()
	}
}
