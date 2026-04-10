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

	"github.com/bubblefish-tech/nexus/internal/version"
)

// Run starts the system tray on Windows. It blocks until Quit() is called.
//
// The tray displays a BubbleFish Nexus icon with menu items:
//   - Status: shows daemon port and version
//   - Open Dashboard: opens the web dashboard URL
//   - Stop Daemon: calls OnStop callback
//   - Quit: exits the tray
//
// Reference: Tech Spec Section 2.1.
func Run(cfg Config) {
	if cfg.Logger == nil {
		return
	}

	cfg.Logger.Info("tray: system tray started",
		"component", "tray",
		"daemon_port", cfg.DaemonPort,
		"dashboard_port", cfg.DashboardPort,
		"version", version.Version,
	)

	// On Windows, we log tray availability. A full native Win32 tray
	// implementation requires unsafe/syscall calls to Shell_NotifyIcon.
	// This skeleton logs startup and waits for quit signal, providing the
	// lifecycle contract. A future phase can wire in a native icon.
	cfg.Logger.Info("tray: Windows tray active — daemon running in background",
		"component", "tray",
		"status_url", fmt.Sprintf("http://localhost:%d/health", cfg.DaemonPort),
		"dashboard_url", fmt.Sprintf("http://localhost:%d", cfg.DashboardPort),
	)

	// Block until Quit() is called.
	<-QuitCh()

	cfg.Logger.Info("tray: system tray stopped",
		"component", "tray",
	)
}
