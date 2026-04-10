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

package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"

	"github.com/bubblefish-tech/nexus/internal/destination"
	"github.com/bubblefish-tech/nexus/internal/version"
)

// benchPipeline is a minimal Pipeline implementation for benchmarks.
type benchPipeline struct{}

func (p *benchPipeline) Write(_ context.Context, params WriteParams) (WriteResult, error) {
	return WriteResult{PayloadID: "bench-payload-1", Status: "accepted"}, nil
}

func (p *benchPipeline) Search(_ context.Context, params SearchParams) (SearchResult, error) {
	records := make([]destination.TranslatedPayload, 10)
	for i := range records {
		records[i] = destination.TranslatedPayload{
			PayloadID: fmt.Sprintf("result-%d", i),
			Content:   fmt.Sprintf("Search result %d content", i),
			Source:    "bench",
		}
	}
	return SearchResult{Records: records, RetrievalStage: 3}, nil
}

func (p *benchPipeline) Status(_ context.Context) (StatusResult, error) {
	return StatusResult{Status: "ok", Version: version.Version, QueueDepth: 0}, nil
}

// benchRPCCall sends a JSON-RPC request and returns the response body.
func benchRPCCall(client *http.Client, url, key, method string, params interface{}) ([]byte, error) {
	var rawParams json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		rawParams = b
	}

	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
	}
	if rawParams != nil {
		reqBody["params"] = rawParams
	}

	body, _ := json.Marshal(reqBody)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func benchStartServer(b *testing.B) (baseURL, key string, cleanup func()) {
	b.Helper()
	key = "bfn_mcp_benchkey1234567890abcdef1234567890abcdef1234567890abcdef"

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatalf("find free port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	srv := New("127.0.0.1", port, []byte(key), "bench-source", &benchPipeline{}, nil)
	if err := srv.Start(); err != nil {
		b.Fatalf("start server: %v", err)
	}

	baseURL = "http://" + srv.Addr() + "/mcp"
	return baseURL, key, func() { srv.Stop() }
}

// BenchmarkMCP_NexusStatus measures a full JSON-RPC round-trip for nexus_status.
func BenchmarkMCP_NexusStatus(b *testing.B) {
	b.ReportAllocs()
	url, key, cleanup := benchStartServer(b)
	b.Cleanup(cleanup)
	client := &http.Client{}

	// Warm up.
	if _, err := benchRPCCall(client, url, key, "tools/call", map[string]interface{}{
		"name":      "nexus_status",
		"arguments": map[string]interface{}{},
	}); err != nil {
		b.Fatalf("warmup: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := benchRPCCall(client, url, key, "tools/call", map[string]interface{}{
			"name":      "nexus_status",
			"arguments": map[string]interface{}{},
		})
		if err != nil {
			b.Fatalf("rpc: %v", err)
		}
	}
}

// BenchmarkMCP_NexusWrite_SmallMemory measures a full JSON-RPC round-trip for
// writing a ~256 byte memory through nexus_write.
func BenchmarkMCP_NexusWrite_SmallMemory(b *testing.B) {
	b.ReportAllocs()
	url, key, cleanup := benchStartServer(b)
	b.Cleanup(cleanup)
	client := &http.Client{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := benchRPCCall(client, url, key, "tools/call", map[string]interface{}{
			"name": "nexus_write",
			"arguments": map[string]interface{}{
				"content": "This is a benchmark memory entry that is approximately 256 bytes in total when serialized into the JSON-RPC request payload sent to the nexus_write tool handler for performance measurement purposes during development.",
			},
		})
		if err != nil {
			b.Fatalf("rpc: %v", err)
		}
	}
}

// BenchmarkMCP_NexusSearch_10Results measures a full JSON-RPC round-trip for
// nexus_search returning ~10 results.
func BenchmarkMCP_NexusSearch_10Results(b *testing.B) {
	b.ReportAllocs()
	url, key, cleanup := benchStartServer(b)
	b.Cleanup(cleanup)
	client := &http.Client{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := benchRPCCall(client, url, key, "tools/call", map[string]interface{}{
			"name": "nexus_search",
			"arguments": map[string]interface{}{
				"q": "benchmark query",
			},
		})
		if err != nil {
			b.Fatalf("rpc: %v", err)
		}
	}
}
