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

//go:build !windows

package tray

import "os"

// Run on non-Windows platforms checks for a display server. If $DISPLAY is
// empty (headless Linux), the tray is gracefully skipped with an INFO log —
// this is NOT an error.
//
// On macOS with a display, the tray logs availability and blocks until quit.
//
// Reference: Tech Spec Section 2.1 — "Headless Linux: tray gracefully skipped
// when $DISPLAY is empty (log INFO, not error)."
func Run(cfg Config) {
	if cfg.Logger == nil {
		return
	}

	display := os.Getenv("DISPLAY")
	waylandDisplay := os.Getenv("WAYLAND_DISPLAY")

	if display == "" && waylandDisplay == "" {
		// Headless environment — skip tray, log INFO (not error).
		cfg.Logger.Info("tray: no display server detected — system tray skipped",
			"component", "tray",
		)
		// Block until Quit() to maintain lifecycle contract.
		<-QuitCh()
		return
	}

	// Display server available — log tray availability.
	cfg.Logger.Info("tray: display server detected — system tray available",
		"component", "tray",
		"display", display,
	)

	// Block until Quit() is called.
	<-QuitCh()

	cfg.Logger.Info("tray: system tray stopped",
		"component", "tray",
	)
}
