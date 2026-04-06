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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

const defaultOllamaURL = "http://localhost:11434"

// ollamaRequest is the JSON body sent to the Ollama embeddings endpoint.
type ollamaRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

// ollamaResponse is the JSON response from the Ollama embeddings endpoint.
type ollamaResponse struct {
	Embedding []float32 `json:"embedding"`
}

// ollamaClient implements EmbeddingClient using the Ollama local API.
// Ollama uses a different request/response shape than OpenAI:
// - Request: POST /api/embeddings {"model": "...", "prompt": "..."}
// - Response: {"embedding": [...]}
//
// Reference: Tech Spec Section 14.4, Phase 5 Behavioral Contract 2.
type ollamaClient struct {
	httpClient *http.Client
	baseURL    string
	model      string
	dimensions int
}

// newOllamaClient creates an Ollama embedding client.
// baseURL defaults to http://localhost:11434 when empty.
func newOllamaClient(baseURL, model string, dimensions int, timeout time.Duration) *ollamaClient {
	if baseURL == "" {
		baseURL = defaultOllamaURL
	}
	return &ollamaClient{
		httpClient: &http.Client{Timeout: timeout},
		baseURL:    baseURL,
		model:      model,
		dimensions: dimensions,
	}
}

// Embed calls POST /api/embeddings on the Ollama server and returns the vector.
// Returns ErrEmbeddingUnavailable on any network, timeout, or HTTP error.
//
// Reference: Tech Spec Section 14.4.
func (c *ollamaClient) Embed(ctx context.Context, text string) ([]float32, error) {
	reqBody := ollamaRequest{
		Model:  c.model,
		Prompt: text,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal request: %v", ErrEmbeddingUnavailable, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/api/embeddings", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("%w: create request: %v", ErrEmbeddingUnavailable, err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEmbeddingUnavailable, err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			slog.Default().Debug("ollama: close response body", "error", cerr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("%w: HTTP %d", ErrEmbeddingUnavailable, resp.StatusCode)
	}

	var result ollamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("%w: decode response: %v", ErrEmbeddingUnavailable, err)
	}

	if len(result.Embedding) == 0 {
		return nil, fmt.Errorf("%w: empty embedding in response", ErrEmbeddingUnavailable)
	}

	return result.Embedding, nil
}

// Dimensions returns the configured embedding dimension count.
func (c *ollamaClient) Dimensions() int { return c.dimensions }

// Close is a no-op for the Ollama client.
func (c *ollamaClient) Close() error { return nil }
