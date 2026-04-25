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
	"io"
	"net/http"
	"os"
	"time"
)

// runTrace captures a runtime/trace from the running daemon.
func runTrace() {
	seconds := 10
	port := 8080
	adminKey := os.Getenv("NEXUS_ADMIN_KEY")

	if adminKey == "" {
		fmt.Fprintln(os.Stderr, "nexus trace: NEXUS_ADMIN_KEY not set")
		os.Exit(1)
	}

	url := fmt.Sprintf("http://127.0.0.1:%d/debug/pprof/trace?seconds=%d", port, seconds)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus trace: %v\n", err)
		os.Exit(1)
	}
	req.Header.Set("Authorization", "Bearer "+adminKey)

	client := &http.Client{Timeout: time.Duration(seconds+30) * time.Second}
	fmt.Printf("nexus trace: capturing %d seconds of trace data...\n", seconds)

	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus trace: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "nexus trace: server returned %d\n", resp.StatusCode)
		os.Exit(1)
	}

	outFile := fmt.Sprintf("nexus-trace-%d.trace", time.Now().Unix())
	f, err := os.Create(outFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus trace: create output: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	n, err := io.Copy(f, resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus trace: download: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("nexus trace: saved %d bytes to %s\n", n, outFile)
	fmt.Printf("Open with: go tool trace %s\n", outFile)
}
