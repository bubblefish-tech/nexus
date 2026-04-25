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

// nexus-supervisor: sidecar watchdog with tiered degradation.
// Spawns nexus, monitors via pipe, escalates through T0–T3 tiers.
// T0: instant restart (<3 failures/60s)
// T1: reduced features (disable embedding)
// T2: read-only mode
// T3: emergency shutdown
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/bubblefish-tech/nexus/internal/supervisor"
)

func main() {
	nexusBin := findNexusBinary()
	if nexusBin == "" {
		fmt.Fprintln(os.Stderr, "nexus-supervisor: cannot find nexus binary")
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	cfg := supervisor.DefaultSidecarConfig()

	spawner := func(ctx context.Context, tier supervisor.DegradationTier, pipe supervisor.Pipe) error {
		args := []string{"start", "--foreground"}
		if tier >= supervisor.TierReducedFeatures {
			args = append(args, "--disable-embedding")
		}
		if tier >= supervisor.TierReadOnly {
			args = append(args, "--read-only")
		}

		cmd := exec.CommandContext(ctx, nexusBin, args...)
		cmd.Stdout = os.Stdout
		stderrBuf := &circularBuffer{max: 2048}
		cmd.Stderr = io.MultiWriter(os.Stderr, stderrBuf)

		// Send ready signal to supervisor.
		_ = pipe.Send(supervisor.PipeMsg{
			Type:      supervisor.PipeMsgReady,
			Timestamp: time.Now(),
		})

		if err := cmd.Run(); err != nil {
			writeCrashFile(stderrBuf.String())
			return err
		}
		return nil
	}

	sidecar := supervisor.NewSidecar(cfg, spawner, logger)

	// Handle OS signals for clean shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := sidecar.Run(ctx); err != nil {
		logger.Error("nexus-supervisor: fatal",
			"error", err,
		)
		os.Exit(1)
	}
}

func findNexusBinary() string {
	exe, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(exe)
		candidate := filepath.Join(dir, "nexus")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		candidate = filepath.Join(dir, "nexus.exe")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	if path, err := exec.LookPath("nexus"); err == nil {
		return path
	}
	return ""
}

func writeCrashFile(lastStderr string) {
	f, err := os.Create("nexus.crash")
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "Last stderr (up to 2KB):\n%s\n", lastStderr)
}

type circularBuffer struct {
	data []byte
	max  int
}

func (b *circularBuffer) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	if len(b.data) > b.max {
		b.data = b.data[len(b.data)-b.max:]
	}
	return len(p), nil
}

func (b *circularBuffer) String() string {
	return string(b.data)
}
