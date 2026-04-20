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
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/bubblefish-tech/nexus/internal/config"
)

// runMCPStdio is the entry point for `nexus mcp stdio`.
//
// It is a thin bridge between Claude Desktop's stdio MCP transport and the
// daemon's HTTP MCP listener. Claude Desktop spawns this command as a child
// process and pipes JSON-RPC messages over stdin/stdout. The bridge forwards
// each message to http://<mcp.bind>:<mcp.port>/mcp with the configured
// bearer token and writes the HTTP response body back to stdout.
//
// Design:
//   - Pure byte forwarding. Bridge does NOT parse JSON-RPC. Any method the
//     daemon supports works without bridge updates.
//   - No auth between Claude Desktop and bridge. The pipe is the trust boundary.
//   - Structured logging to os.Stderr ONLY. Stdout is reserved for the
//     JSON-RPC protocol stream and must never receive non-protocol bytes.
//   - HTTP 204 / empty body responses are NOT written to stdout. This handles
//     JSON-RPC notifications, which per spec MUST NOT receive a response.
//   - Clean shutdown on stdin EOF.
//
// Invariant: nothing non-JSON-RPC ever touches os.Stdout.
func runMCPStdio(args []string) {
	fs := flag.NewFlagSet("mcp stdio", flag.ExitOnError)
	homeFlag := fs.String("home", "", "override config directory (defaults to $BUBBLEFISH_HOME or ~/.nexus)")
	if err := fs.Parse(args); err != nil {
		fatalStdio("parse args: %v", err)
	}

	var configDir string
	if *homeFlag != "" {
		configDir = *homeFlag
	} else {
		d, err := config.ConfigDir()
		if err != nil {
			fatalStdio("resolve config dir: %v", err)
		}
		configDir = d
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	logger = logger.With("component", "mcp-stdio")

	logger.Info("starting mcp stdio bridge", "config_dir", configDir)

	cfg, err := config.Load(configDir, logger)
	if err != nil {
		fatalStdio("load config from %s: %v (try 'nexus install')", configDir, err)
	}

	if !cfg.Daemon.MCP.Enabled {
		fatalStdio("mcp is disabled in daemon.toml ([daemon.mcp] enabled = false)")
	}
	if len(cfg.ResolvedMCPKey) == 0 {
		fatalStdio("mcp api_key is empty in daemon.toml. Run 'nexus install' or add an api_key under [daemon.mcp]")
	}
	if cfg.Daemon.MCP.Bind == "" || cfg.Daemon.MCP.Port == 0 {
		fatalStdio("mcp bind/port missing in daemon.toml")
	}

	targetURL := fmt.Sprintf("http://%s:%d/mcp", cfg.Daemon.MCP.Bind, cfg.Daemon.MCP.Port)
	authHeader := "Bearer " + string(cfg.ResolvedMCPKey)

	logger.Info("bridge configured", "target", targetURL)

	client := &http.Client{
		Timeout: 120 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        2,
			MaxIdleConnsPerHost: 2,
			IdleConnTimeout:     60 * time.Second,
			DisableCompression:  true,
		},
	}

	if err := preflight(client, targetURL, authHeader); err != nil {
		fatalStdio("daemon unreachable at %s: %v (is the daemon running? try: nexus start)", targetURL, err)
	}

	logger.Info("daemon reachable, entering bridge loop")

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)

	stdout := bufio.NewWriter(os.Stdout)
	defer func() { _ = stdout.Flush() }()

	// Mutex protects stdout writes so concurrent goroutines don't
	// interleave JSON-RPC response lines.
	var stdoutMu sync.Mutex
	var wg sync.WaitGroup

	for scanner.Scan() {
		// Copy the line — scanner reuses the buffer.
		line := make([]byte, len(scanner.Bytes()))
		copy(line, scanner.Bytes())
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		wg.Add(1)
		go func(reqLine []byte) {
			defer wg.Done()
			if err := forward(client, targetURL, authHeader, reqLine, stdout, &stdoutMu, logger); err != nil {
				logger.Warn("forward failed", "error", err)
			}
		}(line)
	}

	wg.Wait()

	if err := scanner.Err(); err != nil && err != io.EOF {
		logger.Warn("stdin scanner error", "error", err)
	}

	logger.Info("stdin closed, bridge exiting")
}

func forward(client *http.Client, targetURL, authHeader string, reqLine []byte, stdout *bufio.Writer, stdoutMu *sync.Mutex, logger *slog.Logger) error {
	httpReq, err := http.NewRequest(http.MethodPost, targetURL, bytes.NewReader(reqLine))
	if err != nil {
		return writeBridgeErrorMu(stdout, stdoutMu, reqLine, -32603, "bridge: build http request: "+err.Error())
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", authHeader)

	httpResp, err := client.Do(httpReq)
	if err != nil {
		return writeBridgeErrorMu(stdout, stdoutMu, reqLine, -32603, "bridge: http transport: "+err.Error())
	}
	defer func() { _ = httpResp.Body.Close() }()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return writeBridgeErrorMu(stdout, stdoutMu, reqLine, -32603, "bridge: read response body: "+err.Error())
	}

	// HTTP 204 No Content (or any 2xx with empty body) means the request was a
	// notification — per JSON-RPC 2.0 spec, the server MUST NOT send a response.
	// We must not write anything to stdout, not even a stray newline.
	if httpResp.StatusCode == http.StatusNoContent || len(bytes.TrimSpace(body)) == 0 {
		if httpResp.StatusCode >= 200 && httpResp.StatusCode < 300 {
			return nil
		}
	}

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		if isJSONRPCResponse(body) {
			body = bytes.TrimRight(body, "\r\n")
			stdoutMu.Lock()
			_, _ = stdout.Write(body)
			_ = stdout.WriteByte('\n')
			_ = stdout.Flush()
			stdoutMu.Unlock()
			return nil
		}
		return writeBridgeErrorMu(stdout, stdoutMu, reqLine, -32603, fmt.Sprintf("bridge: daemon returned HTTP %d: %s", httpResp.StatusCode, truncate(string(body), 200)))
	}

	// Strip trailing newline added by Go's json.NewEncoder(w).Encode() before
	// writing to stdout. Encode always appends \n; if we don't strip it we
	// emit two lines — the JSON object and then an empty line. Claude Desktop's
	// readMessage calls JSON.parse on each line, hits the empty line, and
	// throws "Unexpected end of JSON input".
	body = bytes.TrimRight(body, "\r\n")
	stdoutMu.Lock()
	_, writeErr := stdout.Write(body)
	if writeErr == nil {
		writeErr = stdout.WriteByte('\n')
	}
	if writeErr == nil {
		writeErr = stdout.Flush()
	}
	stdoutMu.Unlock()
	return writeErr
}

func preflight(client *http.Client, targetURL, authHeader string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader([]byte(`{}`)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", authHeader)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

func writeBridgeErrorMu(stdout *bufio.Writer, mu *sync.Mutex, reqLine []byte, code int, message string) error {
	mu.Lock()
	defer mu.Unlock()
	return writeBridgeError(stdout, reqLine, code, message)
}

func writeBridgeError(stdout *bufio.Writer, reqLine []byte, code int, message string) error {
	id := extractJSONRPCID(reqLine)
	resp := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]interface{}{
			"code":    code,
			"message": message,
		},
	}
	body, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	if _, err := stdout.Write(body); err != nil {
		return err
	}
	if err := stdout.WriteByte('\n'); err != nil {
		return err
	}
	return stdout.Flush()
}

func extractJSONRPCID(reqLine []byte) json.RawMessage {
	var envelope struct {
		ID json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal(reqLine, &envelope); err != nil || len(envelope.ID) == 0 {
		return json.RawMessage("null")
	}
	return envelope.ID
}

func isJSONRPCResponse(body []byte) bool {
	var envelope struct {
		JSONRPC string `json:"jsonrpc"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return false
	}
	return envelope.JSONRPC == "2.0"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func fatalStdio(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "nexus mcp stdio: "+format+"\n", args...)
	os.Exit(1)
}
