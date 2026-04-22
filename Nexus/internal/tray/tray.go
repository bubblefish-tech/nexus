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

// Package tray provides system tray support for BubbleFish Nexus.
//
// On Windows, a system tray icon is displayed with menu items for status,
// opening the web dashboard, and stopping the daemon.
//
// On headless Linux ($DISPLAY is empty), the tray is gracefully skipped with
// an INFO log — it is NOT an error.
//
// Reference: Tech Spec Section 2.1 — headless skip, Section 13.1 — start.
package tray

import (
	"log/slog"
	"sync"
)

// Config holds the settings for the system tray.
type Config struct {
	DaemonPort    int
	DashboardPort int
	Logger        *slog.Logger
	OnStop        func() // Called when user clicks "Stop Daemon" in tray.
}

// quitOnce ensures Quit() is idempotent.
var quitOnce sync.Once

// quitCh is closed when Quit() is called, signalling the tray loop to exit.
var quitCh = make(chan struct{})

// Quit signals the tray to exit. Safe to call multiple times (sync.Once).
func Quit() {
	quitOnce.Do(func() {
		close(quitCh)
	})
}

// QuitCh returns a channel that is closed when Quit() is called.
func QuitCh() <-chan struct{} {
	return quitCh
}
