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
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/bubblefish-tech/nexus/internal/config"
	"github.com/bubblefish-tech/nexus/internal/updater"
	"github.com/bubblefish-tech/nexus/internal/version"
)

// runUpdate executes the `nexus update` command.
//
// Flow:
//  1. Fetch latest release from GitHub.
//  2. Compare with running version — exit 0 if already current.
//  3. Download platform binary + SHA-256 checksum.
//  4. Verify checksum.
//  5. Pre-update: warn if daemon is alive (user should stop it first).
//  6. Atomically replace the current executable.
//  7. Remove temp files; keep .bak for one boot.
//
// Reference: Tech Spec WIRE.6.
func runUpdate(args []string) {
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	checkOnly := fs.Bool("check", false, "only check for a new version, do not download")
	yes := fs.Bool("yes", false, "skip confirmation prompt")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "nexus update: %v\n", err)
		os.Exit(1)
	}

	client := &http.Client{Timeout: 30 * time.Second}

	fmt.Println("nexus update: checking for new release...")
	info, err := updater.FetchLatest(client)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus update: %v\n", err)
		os.Exit(1)
	}

	latest := info.Version()
	current := version.Version

	fmt.Printf("  current version : %s\n", current)
	fmt.Printf("  latest release  : %s\n", latest)

	if !updater.CompareVersions(current, latest) {
		fmt.Println("nexus update: already up to date")
		return
	}

	fmt.Printf("nexus update: new version available: %s → %s\n", current, latest)

	if *checkOnly {
		fmt.Println("  (run 'nexus update' without --check to install)")
		return
	}

	// Locate platform assets.
	binURL, sumURL, err := updater.FindAssets(info)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus update: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("  asset : %s\n", updater.PlatformAssetName())

	// Pre-update encryption safety check: if daemon is alive, warn.
	configDir, _ := config.ConfigDir()
	if port := resolveDaemonPort(configDir); port > 0 && checkDaemonAlive(port) {
		fmt.Println()
		fmt.Println("  WARNING: daemon is running on port", port)
		fmt.Println("           Stop the daemon before updating to avoid WAL corruption.")
		fmt.Println("           Run: nexus stop")
		if !*yes {
			fmt.Println()
			fmt.Print("  Continue anyway? [y/N] ")
			var answer string
			_, _ = fmt.Scanln(&answer)
			if answer != "y" && answer != "Y" {
				fmt.Println("nexus update: aborted")
				return
			}
		}
	}

	// Confirm if interactive.
	if !*yes {
		fmt.Printf("\nInstall %s? [y/N] ", latest)
		var answer string
		_, _ = fmt.Scanln(&answer)
		if answer != "y" && answer != "Y" {
			fmt.Println("nexus update: aborted")
			return
		}
	}

	// Find the current executable.
	exe, err := updater.CurrentExecutable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus update: %v\n", err)
		os.Exit(1)
	}
	tmpDir := filepath.Dir(exe)

	// Download binary.
	fmt.Println("nexus update: downloading binary...")
	binTmp, err := updater.Download(client, binURL, tmpDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus update: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = os.Remove(binTmp) }()

	// Download checksum.
	fmt.Println("nexus update: downloading checksum...")
	sumTmp, err := updater.Download(client, sumURL, tmpDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus update: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = os.Remove(sumTmp) }()

	// Verify checksum.
	fmt.Println("nexus update: verifying checksum...")
	if err := updater.VerifyChecksum(binTmp, sumTmp); err != nil {
		fmt.Fprintf(os.Stderr, "nexus update: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("  checksum: ok")

	// Atomic replace.
	fmt.Printf("nexus update: installing %s → %s...\n", exe, latest)
	backupPath, err := updater.AtomicReplace(exe, binTmp)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus update: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("nexus update: installed — previous binary saved as %s\n", backupPath)
	fmt.Printf("nexus update: ok — upgraded %s → %s\n", current, latest)
	fmt.Println()
	fmt.Println("  Next steps:")
	fmt.Println("  1. Run 'nexus start' to start the new version")
	fmt.Println("  2. Run 'nexus doctor' to verify the new version is healthy")
	fmt.Printf("  3. Remove backup when satisfied: rm %s\n", backupPath)
}

// resolveDaemonPort returns the configured daemon port, or 0 if config cannot
// be loaded or has no port set.
func resolveDaemonPort(configDir string) int {
	if configDir == "" {
		return 0
	}
	cfg, err := config.Load(configDir, nil)
	if err != nil {
		return 0
	}
	return cfg.Daemon.Port
}
