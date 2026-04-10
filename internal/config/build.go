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

package config

import (
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/bubblefish-tech/nexus/internal/policy"
)

// RunBuild implements the `bubblefish build` command.
//
// It loads the full configuration from configDir (daemon.toml + sources/*.toml
// + destinations/*.toml), validates every source's [source.policy] block
// against the known destination set, then writes compiled/policies.json to
// the compiled/ subdirectory of configDir.
//
// Any SCHEMA_ERROR (empty resolved api_key, duplicate resolved keys, unknown
// destination reference) causes RunBuild to return immediately with a
// descriptive error. On success the compiled/ directory exists with 0700
// permissions and policies.json has 0600.
//
// Reference: Tech Spec Section 9.1, Phase 1 Behavioral Contract.
func RunBuild(configDir string, logger *slog.Logger) error {
	cfg, err := Load(configDir, logger)
	if err != nil {
		return fmt.Errorf("build: load config: %w", err)
	}

	// Build the destination name set for policy validation.
	knownDests := make(map[string]bool, len(cfg.Destinations))
	for _, d := range cfg.Destinations {
		knownDests[d.Name] = true
	}

	// Convert source policies to the compiled representation.
	entries := sourcesToPolicyEntries(cfg)

	// Validate before writing anything to disk.
	if err := policy.Validate(entries, knownDests); err != nil {
		return err
	}

	compiledDir := filepath.Join(configDir, "compiled")
	if err := policy.Compile(entries, compiledDir, logger); err != nil {
		return fmt.Errorf("build: compile policies: %w", err)
	}

	return nil
}

// sourcesToPolicyEntries converts the loaded []*Source slice into the
// []policy.PolicyEntry slice required by policy.Compile. No config types
// escape into the policy package, keeping the import direction one-way.
func sourcesToPolicyEntries(cfg *Config) []policy.PolicyEntry {
	entries := make([]policy.PolicyEntry, 0, len(cfg.Sources))
	for _, src := range cfg.Sources {
		p := src.Policy
		entry := policy.PolicyEntry{
			Source:                src.Name,
			AllowedDestinations:   p.AllowedDestinations,
			AllowedOperations:     p.AllowedOperations,
			AllowedRetrievalModes: p.AllowedRetrievalModes,
			AllowedProfiles:       p.AllowedProfiles,
			MaxResults:            p.MaxResults,
			MaxResponseBytes:      p.MaxResponseBytes,
			FieldVisibility: policy.FieldVisibilityEntry{
				IncludeFields: p.FieldVisibility.IncludeFields,
				StripMetadata: p.FieldVisibility.StripMetadata,
			},
			Cache: policy.PolicyCacheEntry{
				ReadFromCache:               p.Cache.ReadFromCache,
				WriteToCache:                p.Cache.WriteToCache,
				MaxTTLSeconds:               p.Cache.MaxTTLSeconds,
				SemanticSimilarityThreshold: p.Cache.SemanticSimilarityThreshold,
			},
			Decay: policy.PolicyDecayEntry{
				HalfLifeDays:      p.Decay.HalfLifeDays,
				DecayMode:         p.Decay.DecayMode,
				StepThresholdDays: p.Decay.StepThresholdDays,
			},
		}
		entries = append(entries, entry)
	}
	return entries
}
