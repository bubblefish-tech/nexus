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

// Package secrets manages the BubbleFish Nexus secrets directory.
//
// The secrets directory lives at $BUBBLEFISH_HOME/secrets/ (typically
// ~/.bubblefish/Nexus/secrets/) and holds material that must NOT appear in
// TOML config files: LSH seeds, Ed25519 signing keys, and future HMAC keys.
//
// All files in the secrets directory are 0600. The directory itself is 0700.
// On Windows, os.Chmod has no effect on ACLs; operators are responsible for
// securing the directory via NTFS permissions.
//
// Reference: v0.1.3 Build Plan Phase 2 Subtask 2.6.
package secrets

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
)

const (
	// dirPerm is the permission mode for the secrets directory.
	dirPerm = 0o700
	// filePerm is the permission mode for individual secret files.
	filePerm = 0o600
	// seedSize is the number of random bytes in an LSH tier seed.
	seedSize = 32
	// maxTier is the maximum supported tier level (inclusive).
	maxTier = 3
)

// Dir is an open handle to the secrets directory.
type Dir struct {
	path string
}

// Open creates (if absent) and opens the secrets directory at basePath/secrets.
// Returns an error if the directory cannot be created or its permissions set.
func Open(basePath string) (*Dir, error) {
	p := filepath.Join(basePath, "secrets")
	if err := os.MkdirAll(p, dirPerm); err != nil {
		return nil, fmt.Errorf("secrets: create directory %s: %w", p, err)
	}
	// Enforce permissions even if the directory pre-existed.
	if err := os.Chmod(p, dirPerm); err != nil {
		return nil, fmt.Errorf("secrets: chmod directory %s: %w", p, err)
	}
	return &Dir{path: p}, nil
}

// Path returns the absolute path of the secrets directory.
func (d *Dir) Path() string { return d.path }

// LoadOrGenerateLSHSeed returns the 32-byte LSH seed for the given tier.
// If the seed file does not exist, a cryptographically random seed is
// generated, written atomically, and returned. Subsequent calls return the
// same seed.
//
// tier must be in range [0, 3]. Returns an error for out-of-range values.
func (d *Dir) LoadOrGenerateLSHSeed(tier int) ([]byte, error) {
	if tier < 0 || tier > maxTier {
		return nil, fmt.Errorf("secrets: invalid tier %d (must be 0-%d)", tier, maxTier)
	}
	name := fmt.Sprintf("lsh-tier-%d.seed", tier)
	path := filepath.Join(d.path, name)

	data, err := os.ReadFile(path)
	if err == nil {
		if len(data) != seedSize {
			return nil, fmt.Errorf("secrets: seed file %s has unexpected size %d (want %d)", path, len(data), seedSize)
		}
		return data, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("secrets: read seed file %s: %w", path, err)
	}

	// Generate fresh seed.
	seed := make([]byte, seedSize)
	if _, err := rand.Read(seed); err != nil {
		return nil, fmt.Errorf("secrets: generate seed: %w", err)
	}
	if err := writeAtomic(path, seed); err != nil {
		return nil, fmt.Errorf("secrets: write seed file %s: %w", path, err)
	}
	return seed, nil
}

// LoadOrGenerateAllLSHSeeds returns seeds for all 4 tiers (0-3) in order.
// See LoadOrGenerateLSHSeed for file layout.
func (d *Dir) LoadOrGenerateAllLSHSeeds() ([maxTier + 1][]byte, error) {
	var seeds [maxTier + 1][]byte
	for tier := 0; tier <= maxTier; tier++ {
		s, err := d.LoadOrGenerateLSHSeed(tier)
		if err != nil {
			return seeds, err
		}
		seeds[tier] = s
	}
	return seeds, nil
}

// WriteSecret writes arbitrary secret bytes to name within the directory.
// The file is written atomically and set to 0600.
// name must not contain path separators.
func (d *Dir) WriteSecret(name string, data []byte) error {
	if filepath.Base(name) != name {
		return fmt.Errorf("secrets: name %q must not contain path separators", name)
	}
	return writeAtomic(filepath.Join(d.path, name), data)
}

// ReadSecret reads the named secret file. Returns os.ErrNotExist if absent.
func (d *Dir) ReadSecret(name string) ([]byte, error) {
	if filepath.Base(name) != name {
		return nil, fmt.Errorf("secrets: name %q must not contain path separators", name)
	}
	return os.ReadFile(filepath.Join(d.path, name))
}

// WriteSecretPath writes arbitrary secret bytes to a relative path within the
// secrets directory. Unlike WriteSecret, the path may contain subdirectories
// (e.g. "sources/agent-a.ed25519"). Intermediate directories are created with
// 0700 permissions. The file is written atomically and set to 0600.
//
// relPath must be relative (no leading slash) and must not escape the secrets
// directory via ".." components.
// Reference: v0.1.3 Build Plan Phase 4 Subtask 4.1.
func (d *Dir) WriteSecretPath(relPath string, data []byte) error {
	if err := validateRelPath(relPath); err != nil {
		return err
	}
	absPath := filepath.Join(d.path, filepath.FromSlash(relPath))
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("secrets: create directory %s: %w", dir, err)
	}
	return writeAtomic(absPath, data)
}

// ReadSecretPath reads a secret at a relative path within the secrets
// directory. The path may contain subdirectories. Returns os.ErrNotExist
// if absent.
// Reference: v0.1.3 Build Plan Phase 4 Subtask 4.1.
func (d *Dir) ReadSecretPath(relPath string) ([]byte, error) {
	if err := validateRelPath(relPath); err != nil {
		return nil, err
	}
	return os.ReadFile(filepath.Join(d.path, filepath.FromSlash(relPath)))
}

// validateRelPath checks that a relative path is safe: non-empty, no ".."
// traversal, and no absolute path prefix.
func validateRelPath(relPath string) error {
	if relPath == "" {
		return fmt.Errorf("secrets: relative path must not be empty")
	}
	if filepath.IsAbs(relPath) {
		return fmt.Errorf("secrets: path %q must be relative", relPath)
	}
	cleaned := filepath.Clean(filepath.FromSlash(relPath))
	for _, part := range splitPath(cleaned) {
		if part == ".." {
			return fmt.Errorf("secrets: path %q must not contain '..'", relPath)
		}
	}
	return nil
}

// splitPath splits a cleaned filepath into its components.
func splitPath(p string) []string {
	var parts []string
	for p != "" && p != "." {
		dir, file := filepath.Split(p)
		if file != "" {
			parts = append(parts, file)
		}
		p = filepath.Clean(dir)
		if p == "." || p == string(filepath.Separator) {
			break
		}
	}
	return parts
}

// writeAtomic writes data to path atomically via a temp file + rename,
// setting file permissions to 0600. The temp file is created in the same
// directory as path so the rename is within-filesystem.
func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".secret-tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()

	cleanup := func() { _ = os.Remove(tmpName) }

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Chmod(tmpName, filePerm); err != nil {
		cleanup()
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}
