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
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/BubbleFish-Nexus/internal/config"
	"github.com/BubbleFish-Nexus/internal/mcp"
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
	case "mcp":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: bubblefish mcp <subcommand>")
			fmt.Fprintln(os.Stderr, "subcommands:")
			fmt.Fprintln(os.Stderr, "  test  start MCP server and verify nexus_status responds within 5 seconds")
			os.Exit(1)
		}
		switch os.Args[2] {
		case "test":
			runMCPTest()
		default:
			fmt.Fprintf(os.Stderr, "bubblefish mcp: unknown subcommand %q\n", os.Args[2])
			os.Exit(1)
		}
	case "version", "--version":
		fmt.Printf("bubblefish nexus v%s (pre-1.0, API subject to change)\n", version.Version)
	default:
		fmt.Fprintf(os.Stderr, "bubblefish: unknown command %q\n", os.Args[1])
		fmt.Fprintln(os.Stderr, "usage: bubblefish <build|mcp|version>")
		os.Exit(1)
	}
}

// runMCPTest executes the `bubblefish mcp test` command.
//
// It starts a temporary MCP server with a TestPipeline, calls nexus_status,
// verifies the response, and exits 0 on success within 5 seconds.
//
// Reference: Tech Spec Section 14.3, Phase 7 Behavioral Contract 6.
func runMCPTest() {
	const testKey = "bubblefish-mcp-self-test-key"

	// Find a free loopback port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish mcp test: find free port: %v\n", err)
		os.Exit(1)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	// Start temporary MCP server with TestPipeline (no real daemon required).
	pipeline := &mcp.TestPipeline{}
	srv := mcp.New("127.0.0.1", port, []byte(testKey), "test", pipeline, slog.New(
		slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}),
	))
	if err := srv.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish mcp test: start failed: %v\n", err)
		os.Exit(1)
	}
	defer srv.Stop()

	// Wait briefly for the listener to be ready.
	time.Sleep(10 * time.Millisecond)

	// Call nexus_status within a 5-second budget.
	url := "http://" + srv.Addr() + "/mcp"
	client := &http.Client{Timeout: 4 * time.Second}

	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      "nexus_status",
			"arguments": map[string]interface{}{},
		},
	}
	body, _ := json.Marshal(reqBody)

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish mcp test: build request: %v\n", err)
		os.Exit(1)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testKey)

	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish mcp test: call nexus_status: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if err := json.Unmarshal(b, &result); err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish mcp test: parse response: %v\n", err)
		os.Exit(1)
	}

	if result["error"] != nil {
		fmt.Fprintf(os.Stderr, "bubblefish mcp test: nexus_status returned error: %v\n", result["error"])
		os.Exit(1)
	}

	rpcResult, _ := result["result"].(map[string]interface{})
	content, _ := rpcResult["content"].([]interface{})
	if len(content) == 0 {
		fmt.Fprintln(os.Stderr, "bubblefish mcp test: empty content in response")
		os.Exit(1)
	}
	block, _ := content[0].(map[string]interface{})
	text, _ := block["text"].(string)

	var statusResult map[string]interface{}
	if err := json.Unmarshal([]byte(text), &statusResult); err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish mcp test: parse status: %v\n", err)
		os.Exit(1)
	}
	if statusResult["status"] != "ok" {
		fmt.Fprintf(os.Stderr, "bubblefish mcp test: unexpected status=%v\n", statusResult["status"])
		os.Exit(1)
	}

	fmt.Printf("bubblefish mcp test: ok — nexus_status returned status=%q version=%v\n",
		statusResult["status"], statusResult["version"])
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
