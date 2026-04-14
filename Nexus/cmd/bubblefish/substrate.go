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
	"io"
	"net/http"
	"os"
)

// runSubstrate handles the `bubblefish substrate` command family.
func runSubstrate(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: bubblefish substrate <subcommand>")
		fmt.Fprintln(os.Stderr, "subcommands:")
		fmt.Fprintln(os.Stderr, "  status           show substrate status")
		fmt.Fprintln(os.Stderr, "  rotate-ratchet   manually advance the ratchet")
		fmt.Fprintln(os.Stderr, "  prove-deletion   produce a deletion proof for a memory")
		os.Exit(1)
	}

	switch args[0] {
	case "status":
		runSubstrateStatus(args[1:])
	case "rotate-ratchet":
		runSubstrateRotateRatchet(args[1:])
	case "prove-deletion":
		runSubstrateProveDeletion(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "bubblefish substrate: unknown subcommand %q\n", args[0])
		os.Exit(1)
	}
}

func runSubstrateStatus(args []string) {
	asJSON := false
	for _, a := range args {
		if a == "--json" {
			asJSON = true
		}
	}

	resp, err := daemonGet("/api/substrate/status")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if asJSON {
		fmt.Println(string(body))
		return
	}

	var status map[string]interface{}
	if err := json.Unmarshal(body, &status); err != nil {
		fmt.Println(string(body))
		return
	}

	enabled, _ := status["enabled"].(bool)
	if !enabled {
		fmt.Println("substrate: disabled")
		return
	}

	fmt.Println("substrate: enabled")
	if v, ok := status["ratchet_state_id"]; ok {
		fmt.Printf("  ratchet state_id: %.0f\n", v)
	}
	if v, ok := status["sketch_count"]; ok {
		fmt.Printf("  sketch count:     %.0f\n", v)
	}
	if v, ok := status["cuckoo_count"]; ok {
		fmt.Printf("  cuckoo count:     %.0f\n", v)
	}
	if v, ok := status["cuckoo_capacity"]; ok {
		fmt.Printf("  cuckoo capacity:  %.0f\n", v)
	}
}

func runSubstrateRotateRatchet(args []string) {
	resp, err := daemonPost("/api/substrate/rotate-ratchet", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "error: %s\n", string(body))
		os.Exit(1)
	}
	fmt.Println(string(body))
}

func runSubstrateProveDeletion(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: bubblefish substrate prove-deletion <memory_id>")
		os.Exit(1)
	}
	memoryID := args[0]

	resp, err := daemonPost("/api/substrate/prove-deletion?memory_id="+memoryID, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "error: %s\n", string(body))
		os.Exit(1)
	}

	// Pretty-print the proof
	var proof map[string]interface{}
	if err := json.Unmarshal(body, &proof); err != nil {
		fmt.Println(string(body))
		return
	}
	pretty, _ := json.MarshalIndent(proof, "", "  ")
	fmt.Println(string(pretty))
}

// daemonGet performs a GET request to the daemon's HTTP API.
func daemonGet(path string) (*http.Response, error) {
	port := os.Getenv("BUBBLEFISH_PORT")
	if port == "" {
		port = "8080"
	}
	url := fmt.Sprintf("http://127.0.0.1:%s%s", port, path)
	return http.Get(url)
}

// daemonPost performs a POST request to the daemon's HTTP API.
func daemonPost(path string, body io.Reader) (*http.Response, error) {
	port := os.Getenv("BUBBLEFISH_PORT")
	if port == "" {
		port = "8080"
	}
	url := fmt.Sprintf("http://127.0.0.1:%s%s", port, path)
	return http.Post(url, "application/json", body)
}
