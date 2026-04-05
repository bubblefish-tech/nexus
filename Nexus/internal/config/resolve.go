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
	"os"
	"strings"
)

// ResolveEnv resolves a secret reference string to its plaintext value.
//
// Supported prefixes:
//   - "env:VAR_NAME"  — reads the environment variable VAR_NAME.
//   - "file:/path"    — reads the file at /path, trims leading/trailing whitespace.
//   - anything else   — returned as-is (literal value).
//
// The resolved path for "file:" references is logged at DEBUG level.
// The resolved VALUE is NEVER logged at any level.
//
// Reference: Tech Spec Section 6.1.
func ResolveEnv(ref string, logger *slog.Logger) (string, error) {
	switch {
	case strings.HasPrefix(ref, "env:"):
		varName := strings.TrimPrefix(ref, "env:")
		val := os.Getenv(varName)
		// Empty env var is a valid build-time failure: the caller must check.
		return val, nil

	case strings.HasPrefix(ref, "file:"):
		path := strings.TrimPrefix(ref, "file:")
		if logger != nil {
			logger.Debug("config: resolving file reference",
				"component", "config",
				"path", path,
			)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("config: resolve file reference %q: %w", path, err)
		}
		return strings.TrimSpace(string(data)), nil

	default:
		// Plain literal — no resolution needed.
		return ref, nil
	}
}
