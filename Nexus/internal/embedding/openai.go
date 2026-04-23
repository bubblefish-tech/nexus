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

	"github.com/bubblefish-tech/nexus/internal/httputil"
	"github.com/bubblefish-tech/nexus/internal/pool"
)

// openAIRequest is the JSON body sent to /v1/embeddings (single text).
type openAIRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

// openAIBatchRequest is the JSON body sent to /v1/embeddings (batch).
type openAIBatchRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

// openAIResponse is the JSON response from /v1/embeddings.
type openAIResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

// openAIClient implements EmbeddingClient using the OpenAI /v1/embeddings API.
// The same implementation handles provider="openai" and provider="compatible".
// All struct state is held in fields — no package-level variables.
//
// Reference: Tech Spec Section 14.4, Phase 5 Behavioral Contract 2.
type openAIClient struct {
	httpClient *http.Client
	baseURL    string
	model      string
	dimensions int
	// resolvedKey is the pre-resolved API key. NEVER log this value.
	resolvedKey string
}

// newOpenAIClient creates an OpenAI-compatible embedding client.
// baseURL is the endpoint root (e.g. "https://api.openai.com").
// resolvedKey is the pre-resolved API key — never re-read per call.
// INVARIANT: resolvedKey is never logged at any level.
func newOpenAIClient(baseURL, model, resolvedKey string, dimensions int, timeout time.Duration) *openAIClient {
	return &openAIClient{
		httpClient:  httputil.NewClient(timeout),
		baseURL:     baseURL,
		model:       model,
		dimensions:  dimensions,
		resolvedKey: resolvedKey,
	}
}

// Embed calls the /v1/embeddings endpoint and returns the embedding vector.
// Returns ErrEmbeddingUnavailable on any network, timeout, or HTTP error.
// INVARIANT: resolvedKey is never written to any log.
//
// Reference: Tech Spec Section 14.4.
func (c *openAIClient) Embed(ctx context.Context, text string) ([]float32, error) {
	reqBody := openAIRequest{
		Model: c.model,
		Input: text,
	}

	buf := pool.GetJSONBuf()
	if err := json.NewEncoder(buf).Encode(reqBody); err != nil {
		pool.PutJSONBuf(buf)
		return nil, fmt.Errorf("%w: marshal request: %v", ErrEmbeddingUnavailable, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/v1/embeddings", buf)
	if err != nil {
		return nil, fmt.Errorf("%w: create request: %v", ErrEmbeddingUnavailable, err)
	}

	req.Header.Set("Content-Type", "application/json")
	// Attach auth header only when a key is configured.
	// INVARIANT: c.resolvedKey is never logged at any level.
	if c.resolvedKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.resolvedKey)
	}

	resp, err := c.httpClient.Do(req)
	pool.PutJSONBuf(buf)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEmbeddingUnavailable, err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			slog.Default().Debug("openai: close response body", "error", cerr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		// Read and discard body to allow connection reuse; cap read size.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("%w: HTTP %d", ErrEmbeddingUnavailable, resp.StatusCode)
	}

	var result openAIResponse
	result.Data = make([]struct {
		Embedding []float32 `json:"embedding"`
	}, 1)
	if c.dimensions > 0 {
		result.Data[0].Embedding = make([]float32, 0, c.dimensions)
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("%w: decode response: %v", ErrEmbeddingUnavailable, err)
	}

	if len(result.Data) == 0 || len(result.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("%w: empty embedding in response", ErrEmbeddingUnavailable)
	}

	return result.Data[0].Embedding, nil
}

// BatchEmbed sends multiple texts in a single /v1/embeddings request.
// The OpenAI API natively supports array input.
func (c *openAIClient) BatchEmbed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	reqBody := openAIBatchRequest{Model: c.model, Input: texts}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal batch request: %v", ErrEmbeddingUnavailable, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/v1/embeddings", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("%w: create request: %v", ErrEmbeddingUnavailable, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.resolvedKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.resolvedKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEmbeddingUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("%w: HTTP %d", ErrEmbeddingUnavailable, resp.StatusCode)
	}

	var result openAIResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("%w: decode batch response: %v", ErrEmbeddingUnavailable, err)
	}

	vectors := make([][]float32, len(result.Data))
	for i, d := range result.Data {
		vectors[i] = d.Embedding
	}
	return vectors, nil
}

// Dimensions returns the configured embedding dimension count.
func (c *openAIClient) Dimensions() int { return c.dimensions }

// Close is a no-op for the OpenAI client (http.Client has no explicit close).
// Implemented to satisfy the EmbeddingClient interface.
func (c *openAIClient) Close() error { return nil }
