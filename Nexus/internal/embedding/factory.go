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
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/bubblefish-tech/nexus/internal/config"
)

const (
	defaultTimeoutSeconds = 10
	defaultDimensions     = 1536
)

// NewClient creates an EmbeddingClient from the daemon.toml embedding config.
//
// Returns (nil, nil) when embedding is disabled (cfg.Enabled = false). Callers
// must treat a nil client as "embedding not configured" and degrade gracefully:
// skip Stages 2 and 4, set _nexus.semantic_unavailable = true.
//
// resolvedAPIKey is the pre-resolved API key (from config.ResolveEnv). It is
// passed in so this function never calls os.Getenv — all key resolution happens
// once at startup.
//
// INVARIANT: resolvedAPIKey is never logged at any level.
//
// Reference: Tech Spec Section 9.2.8, Phase 5 Behavioral Contract 2.
func NewClient(cfg config.EmbeddingConfig, resolvedAPIKey string, logger *slog.Logger) (EmbeddingClient, error) {
	if !cfg.Enabled {
		if logger != nil {
			logger.Info("embedding: disabled; Stages 2+4 will be bypassed",
				"component", "embedding",
			)
		}
		return nil, nil
	}

	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = defaultTimeoutSeconds * time.Second
	}

	dimensions := cfg.Dimensions
	if dimensions <= 0 {
		dimensions = defaultDimensions
	}

	switch cfg.Provider {
	case ProviderOpenAI, ProviderCompatible, "":
		baseURL := cfg.URL
		if baseURL == "" {
			baseURL = "https://api.openai.com"
		}
		model := cfg.Model
		if model == "" {
			model = "text-embedding-3-small"
		}
		// INVARIANT: resolvedAPIKey is never logged.
		client := newOpenAIClient(baseURL, model, resolvedAPIKey, dimensions, timeout)
		if logger != nil {
			logger.Info("embedding: OpenAI-compatible client created",
				"component", "embedding",
				"provider", cfg.Provider,
				"url", baseURL,
				"model", model,
				"dimensions", dimensions,
				"timeout_seconds", timeout.Seconds(),
			)
		}
		return client, nil

	case ProviderOllama:
		baseURL := cfg.URL
		// Ollama defaults to localhost:11434.
		if baseURL == "" {
			baseURL = defaultOllamaURL
		}
		model := cfg.Model
		if model == "" {
			model = "nomic-embed-text"
		}
		client := newOllamaClient(baseURL, model, dimensions, timeout)
		if logger != nil {
			logger.Info("embedding: Ollama client created",
				"component", "embedding",
				"url", baseURL,
				"model", model,
				"dimensions", dimensions,
				"timeout_seconds", timeout.Seconds(),
			)
		}
		return client, nil

	case ProviderBuiltin:
		configDir := cfg.URL
		if configDir == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return nil, fmt.Errorf("embedding: builtin provider requires config dir (set url field or NEXUS_HOME): %w", err)
			}
			configDir = filepath.Join(home, ".nexus", "Nexus")
		}
		bcfg := DefaultBuiltinConfig(configDir)
		if cfg.Dimensions > 0 {
			bcfg.Dimensions = cfg.Dimensions
		}
		bcfg.Logger = logger
		provider, err := NewBuiltinProvider(bcfg)
		if err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if err := provider.Start(ctx); err != nil {
			return nil, fmt.Errorf("builtin embedding start: %w", err)
		}
		if logger != nil {
			logger.Info("embedding: builtin provider started",
				"component", "embedding",
				"model", filepath.Base(bcfg.ModelPath),
				"dimensions", bcfg.Dimensions,
			)
		}
		return provider, nil

	default:
		return nil, fmt.Errorf("embedding: unknown provider %q; valid values: openai, ollama, compatible, builtin", cfg.Provider)
	}
}
