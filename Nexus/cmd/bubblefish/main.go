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

// Command bubblefish is the entry point for BubbleFish Nexus.
package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/BubbleFish-Nexus/internal/config"
	"github.com/BubbleFish-Nexus/internal/version"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Printf("bubblefish nexus v%s (pre-1.0, API subject to change)\n", version.Version)
		fmt.Fprintln(os.Stderr, "usage: bubblefish <command>")
		fmt.Fprintln(os.Stderr, "commands:")
		fmt.Fprintln(os.Stderr, "  build    compile policies and validate configuration")
		fmt.Fprintln(os.Stderr, "  version  print version string")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "build":
		runBuild()
	case "version", "--version":
		fmt.Printf("bubblefish nexus v%s (pre-1.0, API subject to change)\n", version.Version)
	default:
		fmt.Fprintf(os.Stderr, "bubblefish: unknown command %q\n", os.Args[1])
		fmt.Fprintln(os.Stderr, "usage: bubblefish <build|version>")
		os.Exit(1)
	}
}

// runBuild executes the `bubblefish build` command.
// It resolves the config directory, loads and validates the full
// configuration, then writes compiled/policies.json.
func runBuild() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	configDir, err := config.ConfigDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish build: %v\n", err)
		os.Exit(1)
	}

	if err := config.RunBuild(configDir, logger); err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish build: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("bubblefish build: ok — compiled/policies.json written\n")
}
