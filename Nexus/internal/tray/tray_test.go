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

package tray

import (
	"log/slog"
	"os"
	"testing"
	"time"
)

func TestRunNilLogger(t *testing.T) {
	t.Helper()

	// Run with nil logger should return immediately without panic.
	done := make(chan struct{})
	go func() {
		Run(Config{Logger: nil})
		close(done)
	}()

	select {
	case <-done:
		// Good — returned without panic.
	case <-time.After(2 * time.Second):
		t.Fatal("Run with nil logger did not return within 2 seconds")
	}
}

func TestRunExitsOnQuit(t *testing.T) {
	t.Helper()

	// We need a fresh quit channel for this test since the package-level
	// quitCh may already be closed from a previous test. Use a sub-test
	// approach: verify the function returns when quit is signalled.
	// Since quitOnce/quitCh are package-level, we test the contract:
	// Run blocks and Quit causes it to exit. We call Quit before Run
	// to guarantee immediate return.

	// Reset is not safe in production, but for test coverage we verify
	// that if quitCh is already closed, Run returns immediately.
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	Quit() // ensure quitCh is closed

	done := make(chan struct{})
	go func() {
		Run(Config{
			DaemonPort:    8080,
			DashboardPort: 8081,
			Logger:        logger,
			NoBrowser:     true,
		})
		close(done)
	}()

	select {
	case <-done:
		// Good — returned after Quit.
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit after Quit within 2 seconds")
	}
}

func TestMenuItemCount(t *testing.T) {
	t.Helper()
	count := MenuItemCount()
	if count != 4 {
		t.Fatalf("expected 4 menu items (Status, Open Dashboard, Stop Nexus, Quit), got %d", count)
	}
}
