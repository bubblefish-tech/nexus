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

// nexus-supervisor: watchdog for non-service installs.
// Spawns nexus, captures stderr on crash, respawns with exponential backoff.
// Gives up after 5 crashes in 60 seconds.
package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func main() {
	nexusBin := findNexusBinary()
	if nexusBin == "" {
		fmt.Fprintln(os.Stderr, "nexus-supervisor: cannot find nexus binary")
		os.Exit(1)
	}

	var crashes []time.Time
	var lastStderr string
	backoff := 5 * time.Second

	for {
		cutoff := time.Now().Add(-60 * time.Second)
		var recent []time.Time
		for _, t := range crashes {
			if t.After(cutoff) {
				recent = append(recent, t)
			}
		}
		crashes = recent

		if len(crashes) >= 5 {
			writeCrashFile(lastStderr)
			fmt.Fprintf(os.Stderr, "nexus crashed %d times in 60s — giving up. See .crash file.\n", len(crashes))
			os.Exit(1)
		}

		cmd := exec.Command(nexusBin, "start", "--foreground")
		cmd.Stdout = os.Stdout
		stderrBuf := &circularBuffer{max: 2048}
		cmd.Stderr = io.MultiWriter(os.Stderr, stderrBuf)

		if err := cmd.Run(); err != nil {
			crashes = append(crashes, time.Now())
			lastStderr = stderrBuf.String()
			slog.Warn("nexus exited", "err", err, "backoff", backoff)
			time.Sleep(backoff)
			if backoff < 60*time.Second {
				backoff *= 2
			}
		} else {
			break
		}
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
