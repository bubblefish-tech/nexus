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
	"path/filepath"

	"golang.org/x/term"

	"github.com/bubblefish-tech/nexus/internal/crypto"
)

// runConfig dispatches `bubblefish config <subcommand>`.
func runConfig(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: bubblefish config <subcommand>")
		fmt.Fprintln(os.Stderr, "subcommands:")
		fmt.Fprintln(os.Stderr, "  set-password   set or change the master encryption password")
		os.Exit(1)
	}

	switch args[0] {
	case "set-password":
		runConfigSetPassword()
	default:
		fmt.Fprintf(os.Stderr, "bubblefish config: unknown subcommand %q\n", args[0])
		os.Exit(1)
	}
}

// runConfigSetPassword implements `bubblefish config set-password`.
// It prompts the user for a password (with confirmation), derives a master key,
// and stores the Argon2id salt at the canonical salt path.
func runConfigSetPassword() {
	saltPath, err := defaultSaltPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config set-password: resolve salt path: %v\n", err)
		os.Exit(1)
	}

	fmt.Print("Set encryption password: ")
	pw1, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config set-password: read password: %v\n", err)
		os.Exit(1)
	}

	fmt.Print("Re-enter password: ")
	pw2, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config set-password: read confirmation: %v\n", err)
		os.Exit(1)
	}

	if string(pw1) != string(pw2) {
		fmt.Fprintln(os.Stderr, "config set-password: passwords do not match")
		os.Exit(1)
	}

	if len(pw1) == 0 {
		fmt.Fprintln(os.Stderr, "config set-password: password must not be empty")
		os.Exit(1)
	}

	// Remove any existing salt so a fresh one is generated.
	if err := os.Remove(saltPath); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "config set-password: remove old salt: %v\n", err)
		os.Exit(1)
	}

	mgr, err := crypto.NewMasterKeyManager(string(pw1), saltPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config set-password: derive master key: %v\n", err)
		os.Exit(1)
	}
	if !mgr.IsEnabled() {
		fmt.Fprintln(os.Stderr, "config set-password: key derivation failed unexpectedly")
		os.Exit(1)
	}

	fmt.Printf("Encryption password set. Salt stored at: %s\n", saltPath)
	fmt.Println("Start Nexus with NEXUS_PASSWORD=<your-password> or enter it at the prompt.")
}

// defaultSaltPath returns the canonical salt file path (~/.nexus/crypto.salt).
func defaultSaltPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".nexus", "crypto.salt"), nil
}
