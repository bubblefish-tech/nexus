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

package embedding_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bubblefish-tech/nexus/internal/config"
	"github.com/bubblefish-tech/nexus/internal/embedding"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// openAIHandler returns an httptest handler that mimics /v1/embeddings.
func openAIHandler(vec []float32) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		resp := map[string]interface{}{
			"data": []map[string]interface{}{
				{"embedding": vec, "index": 0, "object": "embedding"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}
}

// ollamaHandler returns an httptest handler that mimics /api/embeddings.
func ollamaHandler(vec []float32) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		resp := map[string]interface{}{"embedding": vec}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}
}

// errorHandler returns HTTP 500 for all requests (simulates provider outage).
func errorHandler(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "internal server error", http.StatusInternalServerError)
}

// ---------------------------------------------------------------------------
// OpenAI-compatible client
// ---------------------------------------------------------------------------

func TestOpenAIClient_Embed_Success(t *testing.T) {
	want := []float32{0.1, 0.2, 0.3}
	srv := httptest.NewServer(openAIHandler(want))
	defer srv.Close()

	cfg := config.EmbeddingConfig{
		Enabled:        true,
		Provider:       embedding.ProviderOpenAI,
		URL:            srv.URL,
		Model:          "text-embedding-3-small",
		Dimensions:     3,
		TimeoutSeconds: 5,
	}

	client, err := embedding.NewClient(cfg, "test-key", nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			t.Logf("close: %v", err)
		}
	}()

	got, err := client.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("len(embedding) = %d; want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("embedding[%d] = %v; want %v", i, got[i], want[i])
		}
	}
}

func TestOpenAIClient_Embed_ProviderError_ReturnsUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(errorHandler))
	defer srv.Close()

	cfg := config.EmbeddingConfig{
		Enabled:        true,
		Provider:       embedding.ProviderOpenAI,
		URL:            srv.URL,
		Model:          "text-embedding-3-small",
		Dimensions:     3,
		TimeoutSeconds: 5,
	}

	client, err := embedding.NewClient(cfg, "key", nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			t.Logf("close: %v", err)
		}
	}()

	_, err = client.Embed(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, embedding.ErrEmbeddingUnavailable) {
		t.Errorf("error = %v; want errors.Is(err, ErrEmbeddingUnavailable)", err)
	}
}

func TestOpenAIClient_Embed_Timeout_ReturnsUnavailable(t *testing.T) {
	// Handler that blocks longer than the client timeout.
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
		http.Error(w, "timeout", http.StatusServiceUnavailable)
	}))
	defer slow.Close()

	cfg := config.EmbeddingConfig{
		Enabled:        true,
		Provider:       embedding.ProviderOpenAI,
		URL:            slow.URL,
		Model:          "text-embedding-3-small",
		Dimensions:     3,
		TimeoutSeconds: 1, // short timeout
	}

	client, err := embedding.NewClient(cfg, "key", nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			t.Logf("close: %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err = client.Embed(ctx, "test")
	if err == nil {
		t.Fatal("expected error on timeout, got nil")
	}
	if !errors.Is(err, embedding.ErrEmbeddingUnavailable) {
		t.Errorf("error = %v; want errors.Is(err, ErrEmbeddingUnavailable)", err)
	}
}

// ---------------------------------------------------------------------------
// Compatible provider (same as OpenAI shape)
// ---------------------------------------------------------------------------

func TestCompatibleClient_Embed_Success(t *testing.T) {
	want := []float32{0.5, 0.6}
	srv := httptest.NewServer(openAIHandler(want))
	defer srv.Close()

	cfg := config.EmbeddingConfig{
		Enabled:        true,
		Provider:       embedding.ProviderCompatible,
		URL:            srv.URL,
		Dimensions:     2,
		TimeoutSeconds: 5,
	}

	client, err := embedding.NewClient(cfg, "", nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer func() { if err := client.Close(); err != nil { t.Logf("close: %v", err) } }()

	got, err := client.Embed(context.Background(), "test")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(embedding) = %d; want 2", len(got))
	}
}

// ---------------------------------------------------------------------------
// Ollama client
// ---------------------------------------------------------------------------

func TestOllamaClient_Embed_Success(t *testing.T) {
	want := []float32{0.9, 0.8, 0.7, 0.6}
	srv := httptest.NewServer(ollamaHandler(want))
	defer srv.Close()

	cfg := config.EmbeddingConfig{
		Enabled:        true,
		Provider:       embedding.ProviderOllama,
		URL:            srv.URL,
		Model:          "nomic-embed-text",
		Dimensions:     4,
		TimeoutSeconds: 5,
	}

	client, err := embedding.NewClient(cfg, "", nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer func() { if err := client.Close(); err != nil { t.Logf("close: %v", err) } }()

	got, err := client.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("len(embedding) = %d; want %d", len(got), len(want))
	}
}

func TestOllamaClient_Embed_Unreachable_ReturnsUnavailable(t *testing.T) {
	cfg := config.EmbeddingConfig{
		Enabled:        true,
		Provider:       embedding.ProviderOllama,
		URL:            "http://127.0.0.1:19999", // nothing listening
		Model:          "nomic-embed-text",
		Dimensions:     4,
		TimeoutSeconds: 1,
	}

	client, err := embedding.NewClient(cfg, "", nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer func() { if err := client.Close(); err != nil { t.Logf("close: %v", err) } }()

	_, err = client.Embed(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, embedding.ErrEmbeddingUnavailable) {
		t.Errorf("error = %v; want errors.Is(err, ErrEmbeddingUnavailable)", err)
	}
}

// ---------------------------------------------------------------------------
// Factory — disabled embedding
// ---------------------------------------------------------------------------

func TestFactory_Disabled_ReturnsNil(t *testing.T) {
	cfg := config.EmbeddingConfig{
		Enabled: false,
	}
	client, err := embedding.NewClient(cfg, "", nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if client != nil {
		t.Fatalf("expected nil client when disabled, got %T", client)
	}
}

func TestFactory_UnknownProvider_ReturnsError(t *testing.T) {
	cfg := config.EmbeddingConfig{
		Enabled:  true,
		Provider: "mystery-provider",
	}
	_, err := embedding.NewClient(cfg, "", nil)
	if err == nil {
		t.Fatal("expected error for unknown provider, got nil")
	}
}

// ---------------------------------------------------------------------------
// Graceful degradation: nil client is safe to check
// ---------------------------------------------------------------------------

func TestFactory_NilClient_IsNilInterface(t *testing.T) {
	// Callers check for nil before using the client.
	// A disabled config must return a nil interface — not a non-nil interface
	// containing a nil pointer.
	cfg := config.EmbeddingConfig{Enabled: false}
	client, err := embedding.NewClient(cfg, "", nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	var ec = client
	if ec != nil {
		t.Fatal("expected nil EmbeddingClient interface when disabled")
	}
}

// ---------------------------------------------------------------------------
// Dimensions
// ---------------------------------------------------------------------------

func TestClient_Dimensions_ReturnsConfiguredValue(t *testing.T) {
	srv := httptest.NewServer(openAIHandler(nil))
	defer srv.Close()

	cfg := config.EmbeddingConfig{
		Enabled:    true,
		Provider:   embedding.ProviderOpenAI,
		URL:        srv.URL,
		Dimensions: 768,
	}
	client, err := embedding.NewClient(cfg, "", nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			t.Logf("Close: %v", err)
		}
	}()

	if client.Dimensions() != 768 {
		t.Errorf("Dimensions() = %d; want 768", client.Dimensions())
	}
}
