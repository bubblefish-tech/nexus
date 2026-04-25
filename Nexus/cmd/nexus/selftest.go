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
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

// runSelfTest performs a non-destructive smoke test on a live daemon.
func runSelfTest() {
	port := 8080
	if len(os.Args) > 3 {
		fmt.Sscanf(os.Args[3], "%d", &port)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	pass := func(step, msg string) { fmt.Printf("  [PASS] %s: %s\n", step, msg) }
	fail := func(step, msg string) {
		fmt.Printf("  [FAIL] %s: %s\n", step, msg)
		os.Exit(1)
	}

	fmt.Println("nexus self-test: running smoke test...")

	// 1. Health check.
	resp, err := client.Get(baseURL + "/health")
	if err != nil {
		fail("health", fmt.Sprintf("daemon unreachable: %v", err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fail("health", fmt.Sprintf("status %d", resp.StatusCode))
		return
	}

	var health struct {
		Status string `json:"status"`
	}
	json.NewDecoder(resp.Body).Decode(&health)
	if health.Status == "ok" || health.Status == "degraded" {
		pass("health", fmt.Sprintf("daemon alive (status=%s)", health.Status))
	} else {
		fail("health", fmt.Sprintf("unexpected status %q", health.Status))
		return
	}

	// 2. Ready check.
	readyResp, err := client.Get(baseURL + "/ready")
	if err != nil {
		fail("ready", fmt.Sprintf("unreachable: %v", err))
		return
	}
	readyResp.Body.Close()
	pass("ready", fmt.Sprintf("status %d", readyResp.StatusCode))

	fmt.Println()
	fmt.Println("nexus self-test: PASS")
}
