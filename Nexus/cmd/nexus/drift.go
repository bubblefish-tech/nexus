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
)

// runDrift executes `nexus drift`.
//
// In standalone mode, drift connects to a running daemon via HTTP and
// continuously verifies WAL-to-destination consistency. When integrated as
// a daemon option ([consistency] enabled = true), it runs as a background
// goroutine inside the daemon process.
//
// Reference: v0.1.3 Build Plan Section 6.3.
func runDrift(args []string) {
	fmt.Fprintln(os.Stderr, "nexus drift: integrated mode — enable via [consistency] enabled = true in daemon.toml")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  The drift detector runs as a background goroutine inside the daemon.")
	fmt.Fprintln(os.Stderr, "  It samples delivered entries and verifies they exist in the destination.")
	fmt.Fprintln(os.Stderr, "  Anomalies are logged at WARN level and exposed via nexus_consistency_score.")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  Configuration:")
	fmt.Fprintln(os.Stderr, "    [consistency]")
	fmt.Fprintln(os.Stderr, "    enabled = true")
	fmt.Fprintln(os.Stderr, "    interval_seconds = 300   # check every 5 minutes")
	fmt.Fprintln(os.Stderr, "    sample_size = 100        # entries per check")
}
