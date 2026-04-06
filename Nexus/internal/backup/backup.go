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

// Package backup implements online backup and restore for BubbleFish Nexus.
//
// Backup creates a crash-safe snapshot of config files, compiled policies,
// WAL segments, and optionally the SQLite database. Restore verifies SHA256
// checksums from a manifest before writing any files.
//
// Reference: Tech Spec Section 14.5.
package backup

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BubbleFish-Nexus/internal/config"
	"github.com/BubbleFish-Nexus/internal/version"

	// Pure-Go SQLite driver for VACUUM INTO.
	_ "modernc.org/sqlite"
)

// Manifest describes a backup's contents. It is written as manifest.json
// in the backup directory.
type Manifest struct {
	Version    string         `json:"version"`
	CreatedAt  time.Time      `json:"created_at"`
	ConfigDir  string         `json:"config_dir"`
	IncludesDB bool           `json:"includes_db"`
	Files      []ManifestFile `json:"files"`
}

// ManifestFile is a single file entry within a backup manifest.
type ManifestFile struct {
	RelPath  string `json:"rel_path"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
	Category string `json:"category"` // "config", "compiled", "wal", "database"
}

// CreateOptions configures a backup create operation.
type CreateOptions struct {
	Dest      string // destination directory for the backup
	IncludeDB bool   // include SQLite database snapshot
	Logger    *slog.Logger
}

// RestoreOptions configures a backup restore operation.
type RestoreOptions struct {
	From   string // backup directory to restore from
	Force  bool   // overwrite existing files
	Logger *slog.Logger
}

// Create performs an online backup of BubbleFish Nexus state.
//
// Order of operations (per Tech Spec Section 14.5):
//  1. Config TOML files (daemon.toml, sources/, destinations/, policies/)
//  2. Compiled JSON files (and .sig sidecars)
//  3. SQLite database (if --include-db, via VACUUM INTO)
//  4. WAL segments (copied LAST for online safety)
//  5. manifest.json with SHA256 checksums
func Create(opts CreateOptions) error {
	if opts.Dest == "" {
		return fmt.Errorf("backup: --dest is required")
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	configDir, err := config.ConfigDir()
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}

	cfg, err := config.Load(configDir, logger)
	if err != nil {
		return fmt.Errorf("backup: load config: %w", err)
	}

	// Resolve WAL and DB paths.
	walPath, err := resolveWALPath(cfg)
	if err != nil {
		return fmt.Errorf("backup: resolve WAL path: %w", err)
	}

	// Create destination directory.
	if err := os.MkdirAll(opts.Dest, 0700); err != nil {
		return fmt.Errorf("backup: create dest dir: %w", err)
	}

	manifest := Manifest{
		Version:    version.Version,
		CreatedAt:  time.Now().UTC(),
		ConfigDir:  configDir,
		IncludesDB: opts.IncludeDB,
	}

	// Phase 1: Config files — daemon.toml
	daemonTOML := filepath.Join(configDir, "daemon.toml")
	if err := backupFile(opts.Dest, configDir, daemonTOML, "config", &manifest, logger); err != nil {
		return err
	}

	// Config subdirectories: sources/, destinations/, policies/
	for _, sub := range []string{"sources", "destinations", "policies"} {
		subDir := filepath.Join(configDir, sub)
		if err := backupDir(opts.Dest, configDir, subDir, "config", &manifest, logger); err != nil {
			return err
		}
	}

	// Phase 2: Compiled files.
	compiledDir := filepath.Join(configDir, "compiled")
	if err := backupDir(opts.Dest, configDir, compiledDir, "compiled", &manifest, logger); err != nil {
		return err
	}

	// Phase 3: SQLite database (optional).
	if opts.IncludeDB {
		dbPath, err := resolveSQLitePath(cfg)
		if err != nil {
			return fmt.Errorf("backup: resolve SQLite path: %w", err)
		}
		if _, statErr := os.Stat(dbPath); statErr == nil {
			if err := backupSQLite(dbPath, opts.Dest, configDir, &manifest, logger); err != nil {
				return fmt.Errorf("backup: SQLite backup: %w", err)
			}
		} else {
			logger.Warn("backup: SQLite database not found, skipping", "path", dbPath)
		}
	}

	// Phase 4: WAL segments (LAST for online safety).
	if err := backupDir(opts.Dest, configDir, walPath, "wal", &manifest, logger); err != nil {
		return err
	}

	// Phase 5: Write manifest.
	manifestPath := filepath.Join(opts.Dest, "manifest.json")
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("backup: marshal manifest: %w", err)
	}
	if err := os.WriteFile(manifestPath, data, 0600); err != nil {
		return fmt.Errorf("backup: write manifest: %w", err)
	}

	logger.Info("backup created",
		"dest", opts.Dest,
		"files", len(manifest.Files),
		"includes_db", opts.IncludeDB,
	)
	return nil
}

// Restore reads a backup and restores all files to their original locations.
// SHA256 checksums are verified before any files are written.
//
// Without --force, restore refuses to overwrite existing files.
//
// Reference: Tech Spec Section 14.5.
func Restore(opts RestoreOptions) error {
	if opts.From == "" {
		return fmt.Errorf("backup: --from is required")
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// Read manifest.
	manifestPath := filepath.Join(opts.From, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("backup: read manifest: %w", err)
	}

	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("backup: parse manifest: %w", err)
	}

	logger.Info("restoring backup",
		"from", opts.From,
		"created_at", manifest.CreatedAt,
		"files", len(manifest.Files),
		"includes_db", manifest.IncludesDB,
	)

	// Phase 1: Verify all checksums BEFORE writing anything.
	for _, mf := range manifest.Files {
		srcPath := filepath.Join(opts.From, mf.RelPath)
		checksum, err := sha256File(srcPath)
		if err != nil {
			return fmt.Errorf("backup: verify %s: %w", mf.RelPath, err)
		}
		if checksum != mf.SHA256 {
			return fmt.Errorf("backup: checksum mismatch for %s: expected %s, got %s",
				mf.RelPath, mf.SHA256, checksum)
		}
	}
	logger.Info("backup integrity verified", "files", len(manifest.Files))

	// Phase 2: Check for existing files (refuse without --force).
	configDir := manifest.ConfigDir
	if !opts.Force {
		for _, mf := range manifest.Files {
			destPath := filepath.Join(configDir, mf.RelPath)
			if _, err := os.Stat(destPath); err == nil {
				return fmt.Errorf("backup: file exists: %s (use --force to overwrite)", destPath)
			}
		}
	}

	// Phase 3: Restore files.
	for _, mf := range manifest.Files {
		srcPath := filepath.Join(opts.From, mf.RelPath)
		destPath := filepath.Join(configDir, mf.RelPath)

		destDir := filepath.Dir(destPath)
		if err := os.MkdirAll(destDir, 0700); err != nil {
			return fmt.Errorf("backup: create dir %s: %w", destDir, err)
		}

		if err := copyFile(srcPath, destPath); err != nil {
			return fmt.Errorf("backup: restore %s: %w", mf.RelPath, err)
		}

		logger.Info("restored", "file", mf.RelPath, "category", mf.Category)
	}

	logger.Info("restore complete", "files", len(manifest.Files), "config_dir", configDir)
	return nil
}

// backupFile copies a single file into the backup directory, recording it in
// the manifest with its SHA256 checksum.
func backupFile(destDir, baseDir, filePath, category string, manifest *Manifest, logger *slog.Logger) error {
	info, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // skip missing optional files
		}
		return fmt.Errorf("backup: stat %s: %w", filePath, err)
	}
	if info.IsDir() {
		return nil
	}

	relPath, err := filepath.Rel(baseDir, filePath)
	if err != nil {
		return fmt.Errorf("backup: rel path for %s: %w", filePath, err)
	}

	destPath := filepath.Join(destDir, relPath)
	if err := os.MkdirAll(filepath.Dir(destPath), 0700); err != nil {
		return fmt.Errorf("backup: mkdir for %s: %w", destPath, err)
	}

	checksum, err := copyFileWithHash(filePath, destPath)
	if err != nil {
		return fmt.Errorf("backup: copy %s: %w", relPath, err)
	}

	manifest.Files = append(manifest.Files, ManifestFile{
		RelPath:  relPath,
		SHA256:   checksum,
		Size:     info.Size(),
		Category: category,
	})

	logger.Info("backed up", "file", relPath, "category", category)
	return nil
}

// backupDir copies all files in a directory (non-recursive for flat dirs,
// recursive for nested ones) into the backup.
func backupDir(destDir, baseDir, dirPath, category string, manifest *Manifest, logger *slog.Logger) error {
	info, err := os.Stat(dirPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // skip missing directories
		}
		return fmt.Errorf("backup: stat dir %s: %w", dirPath, err)
	}
	if !info.IsDir() {
		return nil
	}

	return filepath.Walk(dirPath, func(path string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if fi.IsDir() {
			return nil
		}
		return backupFile(destDir, baseDir, path, category, manifest, logger)
	})
}

// backupSQLite performs a crash-safe SQLite backup using VACUUM INTO.
// This creates a clean, defragmented copy that is safe to run while the
// daemon is writing (it acquires a read lock on the source database).
func backupSQLite(dbPath, destDir, baseDir string, manifest *Manifest, logger *slog.Logger) error {
	relPath, err := filepath.Rel(baseDir, dbPath)
	if err != nil {
		return fmt.Errorf("rel path for %s: %w", dbPath, err)
	}

	destPath := filepath.Join(destDir, relPath)
	if err := os.MkdirAll(filepath.Dir(destPath), 0700); err != nil {
		return fmt.Errorf("mkdir for %s: %w", destPath, err)
	}

	// Open the source database read-only for VACUUM INTO.
	db, err := sql.Open("sqlite", dbPath+"?mode=ro")
	if err != nil {
		return fmt.Errorf("open sqlite: %w", err)
	}
	defer db.Close()

	// VACUUM INTO creates a complete, defragmented copy at the target path.
	// The target file must NOT already exist.
	if err := os.Remove(destPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove existing backup db: %w", err)
	}

	if _, err := db.Exec("VACUUM INTO ?", destPath); err != nil {
		return fmt.Errorf("VACUUM INTO: %w", err)
	}

	// Set secure permissions on the backup copy.
	if err := os.Chmod(destPath, 0600); err != nil {
		return fmt.Errorf("chmod backup db: %w", err)
	}

	// Compute checksum of the backup copy.
	checksum, err := sha256File(destPath)
	if err != nil {
		return fmt.Errorf("hash backup db: %w", err)
	}

	fi, err := os.Stat(destPath)
	if err != nil {
		return fmt.Errorf("stat backup db: %w", err)
	}

	manifest.Files = append(manifest.Files, ManifestFile{
		RelPath:  relPath,
		SHA256:   checksum,
		Size:     fi.Size(),
		Category: "database",
	})

	logger.Info("backed up", "file", relPath, "category", "database", "method", "VACUUM INTO")
	return nil
}

// copyFileWithHash copies src to dst, computing and returning the SHA256
// hex digest. The destination file is created with 0600 permissions.
func copyFileWithHash(src, dst string) (string, error) {
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return "", err
	}
	defer out.Close()

	h := sha256.New()
	w := io.MultiWriter(out, h)

	if _, err := io.Copy(w, in); err != nil {
		return "", err
	}
	if err := out.Sync(); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// copyFile copies src to dst with 0600 permissions.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

// sha256File computes the SHA256 hex digest of a file.
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// resolveWALPath returns the absolute WAL directory path from config.
func resolveWALPath(cfg *config.Config) (string, error) {
	p := cfg.Daemon.WAL.Path
	if p == "" {
		configDir, err := config.ConfigDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(configDir, "wal"), nil
	}
	return expandPath(p)
}

// resolveSQLitePath returns the absolute SQLite database path from config.
func resolveSQLitePath(cfg *config.Config) (string, error) {
	for _, dst := range cfg.Destinations {
		if dst.Type == "sqlite" && dst.DBPath != "" {
			return expandPath(dst.DBPath)
		}
	}
	configDir, err := config.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "memories.db"), nil
}

// expandPath expands a leading ~ to the user's home directory.
func expandPath(p string) (string, error) {
	if !strings.HasPrefix(p, "~") {
		return filepath.Clean(p), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("os.UserHomeDir: %w", err)
	}
	return filepath.Join(home, p[1:]), nil
}
