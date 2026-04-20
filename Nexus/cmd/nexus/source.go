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
	"encoding/hex"
	"fmt"
	"os"

	"github.com/bubblefish-tech/nexus/internal/config"
	"github.com/bubblefish-tech/nexus/internal/provenance"
	"github.com/bubblefish-tech/nexus/internal/secrets"
)

// runSource dispatches source management subcommands.
//
// Usage:
//
//	nexus source rotate-key <name>   rotate the Ed25519 signing key
//	nexus source pubkey <name>       print the public key (hex)
//
// Reference: v0.1.3 Build Plan Phase 4 Subtask 4.1.
func runSource(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: nexus source <subcommand>")
		fmt.Fprintln(os.Stderr, "subcommands:")
		fmt.Fprintln(os.Stderr, "  rotate-key <name>   rotate the Ed25519 signing key for a source")
		fmt.Fprintln(os.Stderr, "  pubkey <name>        print the source's Ed25519 public key (hex)")
		os.Exit(1)
	}

	switch args[0] {
	case "rotate-key":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: nexus source rotate-key <source-name>")
			os.Exit(1)
		}
		runSourceRotateKey(args[1])
	case "pubkey":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: nexus source pubkey <source-name>")
			os.Exit(1)
		}
		runSourcePubkey(args[1])
	default:
		fmt.Fprintf(os.Stderr, "nexus source: unknown subcommand %q\n", args[0])
		os.Exit(1)
	}
}

func openSecretsDir() *secrets.Dir {
	configDir, err := config.ConfigDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus source: resolve config dir: %v\n", err)
		os.Exit(1)
	}
	sd, err := secrets.Open(configDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus source: open secrets: %v\n", err)
		os.Exit(1)
	}
	return sd
}

func runSourceRotateKey(name string) {
	sd := openSecretsDir()

	newKP, err := provenance.RotateSourceKey(sd, name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus source rotate-key: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("nexus source rotate-key: ok — new key ID: %s\n", newKP.KeyID)
	fmt.Printf("public key (hex): %s\n", hex.EncodeToString(newKP.PublicKey))
}

func runSourcePubkey(name string) {
	sd := openSecretsDir()

	kp, err := provenance.LoadSourceKey(sd, name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus source pubkey: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("key ID:     %s\n", kp.KeyID)
	fmt.Printf("public key: %s\n", hex.EncodeToString(kp.PublicKey))
}
