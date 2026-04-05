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

// Package embedding provides the EmbeddingClient interface and provider
// implementations (OpenAI-compatible, Ollama) for BubbleFish Nexus.
//
// Callers MUST treat all errors from Embed as graceful-degradation signals —
// never crash or return 5xx to the client when the embedding provider is
// unreachable or times out.
//
// Reference: Tech Spec Section 14.4, Phase 5 Behavioral Contract.
package embedding

import (
	"context"
	"errors"
)

// EmbeddingClient computes dense vector embeddings for text.
// All implementations must be safe for concurrent use.
//
// Reference: Tech Spec Section 14.4.
type EmbeddingClient interface {
	// Embed returns a vector embedding for the given text.
	// Returns ErrEmbeddingUnavailable when the provider is unreachable or times
	// out. Callers must degrade gracefully on any error — do not propagate to
	// the end client.
	Embed(ctx context.Context, text string) ([]float32, error)

	// Dimensions returns the number of dimensions this client produces.
	// Used to validate stored embeddings are compatible with the query vector.
	Dimensions() int

	// Close releases any resources held by the client (idle connections, etc.).
	// Safe to call once. No-op after the first call.
	Close() error
}

// ErrEmbeddingUnavailable is returned when the embedding provider is
// unreachable, times out, or returns a non-success HTTP response.
// Callers should treat this as a graceful degradation signal: skip Stages 2
// and 4 and set _nexus.semantic_unavailable = true in the response.
//
// Reference: Tech Spec Section 3.4 — Stage 4, Phase 5 Behavioral Contract 4.
var ErrEmbeddingUnavailable = errors.New("embedding provider unavailable")

// Provider name constants match the daemon.toml embedding.provider values.
const (
	ProviderOpenAI     = "openai"
	ProviderOllama     = "ollama"
	ProviderCompatible = "compatible"
)
