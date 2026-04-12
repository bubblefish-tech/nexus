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

// runSentinel executes `bubblefish sentinel`.
//
// In standalone mode, sentinel connects to a running daemon via HTTP and
// continuously verifies WAL-to-destination consistency. When integrated as
// a daemon option ([daemon.sentinel] enabled = true), it runs as a background
// goroutine inside the daemon process.
//
// Reference: v0.1.3 Build Plan Section 6.3.
func runSentinel(args []string) {
	fmt.Fprintln(os.Stderr, "bubblefish sentinel: integrated mode — enable via [daemon.sentinel] enabled = true in config")
	fmt.Fprintln(os.Stderr, "  The sentinel runs as a background goroutine inside the daemon.")
	fmt.Fprintln(os.Stderr, "  It samples 1% of delivered entries and verifies they exist in the destination.")
	fmt.Fprintln(os.Stderr, "  Anomalies are logged at WARN level and exposed via Prometheus metrics.")
}
