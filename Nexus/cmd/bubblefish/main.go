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
	"path/filepath"
	"strings"
	"time"

	"github.com/BubbleFish-Nexus/internal/config"
	"github.com/BubbleFish-Nexus/internal/lint"
	"github.com/BubbleFish-Nexus/internal/mcp"
	"github.com/BubbleFish-Nexus/internal/signing"
	"github.com/BubbleFish-Nexus/internal/version"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Printf("bubblefish nexus v%s (pre-1.0, API subject to change)\n", version.Version)
		fmt.Fprintln(os.Stderr, "usage: bubblefish <command>")
		fmt.Fprintln(os.Stderr, "commands:")
		fmt.Fprintln(os.Stderr, "  install      create config directory and initial configuration")
		fmt.Fprintln(os.Stderr, "  start        start daemon + MCP + dashboard + tray")
		fmt.Fprintln(os.Stderr, "  dev          start daemon with debug logging and auto-reload")
		fmt.Fprintln(os.Stderr, "  build        compile policies and validate configuration")
		fmt.Fprintln(os.Stderr, "  lint         check configuration for dangerous or suboptimal settings")
		fmt.Fprintln(os.Stderr, "  mcp          MCP server management")
		fmt.Fprintln(os.Stderr, "  backup       create or restore a backup of config, WAL, and database")
		fmt.Fprintln(os.Stderr, "  bench        throughput, latency, and retrieval evaluation benchmarks")
		fmt.Fprintln(os.Stderr, "  demo         reliability demo: crash-recovery with 50 memories")
		fmt.Fprintln(os.Stderr, "  audit        query, export, and tail the interaction audit log")
		fmt.Fprintln(os.Stderr, "  stop         gracefully stop a running bubblefish daemon")
	fmt.Fprintln(os.Stderr, "  status       show daemon health and resolved paths")
		fmt.Fprintln(os.Stderr, "  tui          interactive terminal dashboard (Bubble Tea)")
		fmt.Fprintln(os.Stderr, "  doctor       run configuration and connectivity health checks")
		fmt.Fprintln(os.Stderr, "  anchor       manage external anchoring (setup --gist)")
		fmt.Fprintln(os.Stderr, "  source       manage source signing keys (rotate-key, pubkey)")
		fmt.Fprintln(os.Stderr, "  timeline     show full audit history of a memory (no daemon required)")
		fmt.Fprintln(os.Stderr, "  verify       verify a cryptographic proof bundle (no daemon required)")
		fmt.Fprintln(os.Stderr, "  sign-config  sign compiled config files for signed-mode deployments")
		fmt.Fprintln(os.Stderr, "  version      print version string")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "install":
		runInstall(os.Args[2:])
	case "start":
		runStart()
	case "dev":
		runDev()
	case "build":
		runBuild()
	case "lint":
		runLint()
	case "mcp":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: bubblefish mcp <subcommand>")
			fmt.Fprintln(os.Stderr, "subcommands:")
			fmt.Fprintln(os.Stderr, "  test   start MCP server and verify nexus_status responds within 5 seconds")
			fmt.Fprintln(os.Stderr, "  stdio  bridge stdin/stdout to the daemon's HTTP MCP listener (for Claude Desktop MCPB)")
			os.Exit(1)
		}
		switch os.Args[2] {
		case "test":
			runMCPTest()
		case "stdio":
			runMCPStdio(os.Args[3:])
		default:
			fmt.Fprintf(os.Stderr, "bubblefish mcp: unknown subcommand %q\n", os.Args[2])
			os.Exit(1)
		}
	case "tui":
		runTUI()
	case "doctor":
		runDoctor()
	case "stop":
		runStop(os.Args[2:])
	case "status":
		runStatus(os.Args[2:])
	case "audit":
		runAudit(os.Args[2:])
	case "backup":
		runBackup(os.Args[2:])
	case "bench":
		runBench(os.Args[2:])
	case "demo":
		runDemo(os.Args[2:])
	case "anchor":
		runAnchor(os.Args[2:])
	case "source":
		runSource(os.Args[2:])
	case "timeline":
		runTimeline(os.Args[2:])
	case "verify":
		runVerify(os.Args[2:])
	case "sign-config":
		runSignConfig()
	case "version", "--version":
		fmt.Printf("bubblefish nexus v%s (pre-1.0, API subject to change)\n", version.Version)
	default:
		fmt.Fprintf(os.Stderr, "bubblefish: unknown command %q\n", os.Args[1])
		fmt.Fprintln(os.Stderr, "usage: bubblefish <install|start|stop|dev|build|lint|doctor|status|audit|backup|bench|demo|sign-config|mcp|version>")
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
	if err := ln.Close(); err != nil {
		slog.Error("mcp test: close listener", "error", err)
	}

	// Start temporary MCP server with TestPipeline (no real daemon required).
	pipeline := &mcp.TestPipeline{}
	srv := mcp.New("127.0.0.1", port, []byte(testKey), "test", pipeline, slog.New(
		slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}),
	))
	if err := srv.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish mcp test: start failed: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		if err := srv.Stop(); err != nil {
			slog.Error("mcp test: stop server", "error", err)
		}
	}()

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
	defer func() { _ = resp.Body.Close() }()

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

// runSignConfig executes the `bubblefish sign-config` command.
// It reads compiled/*.json files, computes HMAC-SHA256 signatures using the
// key provided via --keyref, and writes *.sig sidecar files.
//
// Usage: bubblefish sign-config --keyref env:NEXUS_SIGNING_KEY
//
// Reference: Tech Spec Section 6.5.
func runSignConfig() {
	// Parse --keyref flag.
	var keyRef string
	args := os.Args[2:]
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--keyref" && i+1 < len(args):
			keyRef = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--keyref="):
			keyRef = strings.TrimPrefix(args[i], "--keyref=")
		}
	}
	if keyRef == "" {
		fmt.Fprintln(os.Stderr, "bubblefish sign-config: --keyref is required")
		fmt.Fprintln(os.Stderr, "usage: bubblefish sign-config --keyref env:NEXUS_SIGNING_KEY")
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	// Resolve the key reference — NEVER log the resolved value.
	resolved, err := config.ResolveEnv(keyRef, logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish sign-config: resolve key: %v\n", err)
		os.Exit(1)
	}
	if resolved == "" {
		fmt.Fprintln(os.Stderr, "bubblefish sign-config: key resolved to empty value")
		os.Exit(1)
	}

	configDir, err := config.ConfigDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish sign-config: %v\n", err)
		os.Exit(1)
	}
	compiledDir := filepath.Join(configDir, "compiled")

	if err := signing.SignAll(compiledDir, []byte(resolved), logger); err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish sign-config: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("bubblefish sign-config: ok — all compiled config files signed")
}

// runLint executes the `bubblefish lint` command.
// It loads the configuration and runs all lint checks, printing findings to
// stdout. Exit code 0 if no errors, 1 if any finding has error severity.
//
// Reference: Tech Spec Section 6.7, Phase R-11.
func runLint() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	}))

	configDir, err := config.ConfigDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish lint: %v\n", err)
		os.Exit(1)
	}

	cfg, err := config.Load(configDir, logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish lint: config load failed: %v\n", err)
		os.Exit(1)
	}

	result := lint.Run(cfg, configDir)

	if len(result.Findings) == 0 {
		fmt.Println("bubblefish lint: ok — no issues found")
		return
	}

	for _, f := range result.Findings {
		fmt.Printf("[%s] %s: %s\n", f.Severity, f.Check, f.Message)
	}

	if result.HasErrors() {
		fmt.Fprintf(os.Stderr, "\nbubblefish lint: %d issue(s) found, including errors\n", len(result.Findings))
		os.Exit(1)
	}
	fmt.Printf("\nbubblefish lint: %d warning(s), no errors\n", len(result.Findings))
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
