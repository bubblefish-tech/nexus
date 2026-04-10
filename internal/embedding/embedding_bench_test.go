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

package embedding

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/bubblefish-tech/nexus/internal/config"
)

// benchEmbeddingHandler returns an httptest handler that mimics the OpenAI
// /v1/embeddings endpoint, returning a fixed 256-dimensional vector.
// This avoids any real network calls to external embedding services.
func benchEmbeddingHandler() http.HandlerFunc {
	// Pre-build the response once.
	vec := make([]float32, 256)
	for i := range vec {
		vec[i] = float32(i) * 0.001
	}
	resp := map[string]interface{}{
		"data": []map[string]interface{}{
			{"embedding": vec, "index": 0, "object": "embedding"},
		},
	}
	respBytes, _ := json.Marshal(resp)

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(respBytes)
	}
}

func benchEmbeddingClient(b *testing.B) EmbeddingClient {
	b.Helper()
	srv := httptest.NewServer(benchEmbeddingHandler())
	b.Cleanup(srv.Close)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	client, err := NewClient(config.EmbeddingConfig{
		Enabled:    true,
		Provider:   ProviderOpenAI,
		URL:        srv.URL + "/v1/embeddings",
		Model:      "text-embedding-3-small",
		Dimensions: 256,
	}, "fake-api-key", logger)
	if err != nil {
		b.Fatalf("NewClient: %v", err)
	}
	b.Cleanup(func() { client.Close() })
	return client
}

// BenchmarkEmbedding_Generate_ShortText measures embedding generation for a
// ~50 character string using a local httptest mock server.
// No real network calls to external services (Ollama, OpenAI) are made.
func BenchmarkEmbedding_Generate_ShortText(b *testing.B) {
	b.ReportAllocs()
	client := benchEmbeddingClient(b)
	ctx := context.Background()
	text := "Short text for embedding benchmark test."

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		vec, err := client.Embed(ctx, text)
		if err != nil {
			b.Fatalf("embed: %v", err)
		}
		if len(vec) != 256 {
			b.Fatalf("expected 256 dims, got %d", len(vec))
		}
	}
}

// BenchmarkEmbedding_Generate_ParagraphText measures embedding generation for a
// ~500 character string using a local httptest mock server.
func BenchmarkEmbedding_Generate_ParagraphText(b *testing.B) {
	b.ReportAllocs()
	client := benchEmbeddingClient(b)
	ctx := context.Background()
	text := strings.Repeat("This is a paragraph of text used to benchmark the embedding generation path. ", 7)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		vec, err := client.Embed(ctx, text)
		if err != nil {
			b.Fatalf("embed: %v", err)
		}
		if len(vec) != 256 {
			b.Fatalf("expected 256 dims, got %d", len(vec))
		}
	}
}
