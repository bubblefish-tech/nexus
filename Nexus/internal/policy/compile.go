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

package policy

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/BubbleFish-Nexus/internal/version"
)

// Compile writes a compiled/policies.json artifact to outputDir containing
// all provided PolicyEntry values. Validation is the caller's responsibility
// (call Validate before Compile).
//
// The write is atomic: data is written to a temp file in outputDir, fsynced,
// then renamed to policies.json. Both the temp file and the final file receive
// 0600 permissions. outputDir is created with 0700 if it does not exist.
//
// Reference: Tech Spec Section 9.1, Phase 1 Behavioral Contract items 4–5.
func Compile(entries []PolicyEntry, outputDir string, logger *slog.Logger) error {
	compiled := CompiledPolicies{
		Version:    version.Version,
		CompiledAt: time.Now().UTC(),
		Policies:   entries,
	}

	data, err := json.MarshalIndent(compiled, "", "  ")
	if err != nil {
		return fmt.Errorf("policy: marshal compiled policies: %w", err)
	}

	// Ensure the output directory exists with restricted permissions.
	if err := os.MkdirAll(outputDir, 0700); err != nil {
		return fmt.Errorf("policy: create compiled directory %q: %w", outputDir, err)
	}

	// Atomic write: temp file in the same directory → fsync → rename.
	// Temp file in the same directory guarantees same filesystem (no EXDEV).
	tmpFile, err := os.CreateTemp(outputDir, "policies-*.json.tmp")
	if err != nil {
		return fmt.Errorf("policy: create temp file in %q: %w", outputDir, err)
	}
	tmpPath := tmpFile.Name()

	// Track whether the rename completed so the defer knows whether to clean up.
	renamed := false
	defer func() {
		// Close is idempotent on an already-closed *os.File; ignore the error.
		_ = tmpFile.Close()
		if !renamed {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmpFile.Write(data); err != nil {
		return fmt.Errorf("policy: write temp file %q: %w", tmpPath, err)
	}
	// Restrict permissions before the file is visible at its final path.
	if err := tmpFile.Chmod(0600); err != nil {
		return fmt.Errorf("policy: chmod temp file %q: %w", tmpPath, err)
	}
	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("policy: fsync temp file %q: %w", tmpPath, err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("policy: close temp file %q: %w", tmpPath, err)
	}

	outPath := filepath.Join(outputDir, "policies.json")
	if err := os.Rename(tmpPath, outPath); err != nil {
		return fmt.Errorf("policy: rename %q → %q: %w", tmpPath, outPath, err)
	}
	renamed = true

	if logger != nil {
		logger.Info("policy: compiled policies written",
			"component", "policy",
			"path", outPath,
			"sources", len(entries),
		)
	}

	return nil
}

// OutputPath returns the canonical path for the compiled policies artifact
// within the given compiled directory.
func OutputPath(compiledDir string) string {
	return filepath.Join(compiledDir, "policies.json")
}
