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
	"os"

	"github.com/bubblefish-tech/nexus/internal/config"
	"github.com/bubblefish-tech/nexus/internal/secrets"
)

// runAnchor dispatches anchor management subcommands.
//
// Usage:
//
//	bubblefish anchor setup --gist   configure GitHub Gist auto-publish
//
// Reference: v0.1.3 Build Plan Phase 4 Subtask 4.8.
func runAnchor(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: bubblefish anchor <subcommand>")
		fmt.Fprintln(os.Stderr, "subcommands:")
		fmt.Fprintln(os.Stderr, "  setup --gist   configure GitHub Gist auto-publish for daily Merkle roots")
		os.Exit(1)
	}

	switch args[0] {
	case "setup":
		runAnchorSetup(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "bubblefish anchor: unknown subcommand %q\n", args[0])
		os.Exit(1)
	}
}

func runAnchorSetup(args []string) {
	useGist := false
	for _, arg := range args {
		if arg == "--gist" {
			useGist = true
		}
	}

	if !useGist {
		fmt.Fprintln(os.Stderr, "usage: bubblefish anchor setup --gist")
		fmt.Fprintln(os.Stderr, "\nCurrently only GitHub Gist anchoring is supported.")
		os.Exit(1)
	}

	fmt.Print("Enter your GitHub personal access token (gist scope): ")
	var token string
	if _, err := fmt.Scanln(&token); err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish anchor setup: read token: %v\n", err)
		os.Exit(1)
	}

	if token == "" {
		fmt.Fprintln(os.Stderr, "bubblefish anchor setup: token cannot be empty")
		os.Exit(1)
	}

	configDir, err := config.ConfigDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish anchor setup: %v\n", err)
		os.Exit(1)
	}
	sd, err := secrets.Open(configDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish anchor setup: %v\n", err)
		os.Exit(1)
	}

	if err := sd.WriteSecret("gist-token", []byte(token)); err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish anchor setup: write token: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("bubblefish anchor setup: ok — GitHub Gist token stored in secrets/gist-token")
	fmt.Println("Daily Merkle roots will be auto-published when the daemon is running.")
}
