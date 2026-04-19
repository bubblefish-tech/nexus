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

package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/bubblefish-tech/nexus/internal/crypto"
)

const (
	bfbkMagic   = "BFBK"
	bfbkVersion = uint32(1)
	backupDomain = "nexus-backup-key-v1"

	// headerSize is the fixed prefix: 4-byte magic + 4-byte version.
	headerSize = 8
)

// ErrEncryptionDisabled is returned when a password is not configured and an
// encrypted backup/restore is attempted.
var ErrEncryptionDisabled = errors.New("backup: encryption not configured (run 'bubblefish config set-password' first)")

// ExportEncryptedOptions configures an encrypted single-file backup export.
type ExportEncryptedOptions struct {
	// SourceDir is the directory to archive. If empty, uses config.ConfigDir().
	SourceDir  string
	OutputPath string
	Logger     *slog.Logger
}

// ImportEncryptedOptions configures an encrypted single-file backup import.
type ImportEncryptedOptions struct {
	InputPath string
	// DestDir is where files are extracted. If empty, uses config.ConfigDir().
	DestDir string
	Force   bool
	Logger  *slog.Logger
}

// ExportEncrypted creates a single-file encrypted backup.
//
// File format:
//
//	[4-byte "BFBK"] [4-byte version=1 big-endian] [nonce(12)||ciphertext||tag(16)]
//
// The plaintext is a gzip-compressed tar archive of SourceDir.
// Key: mkm.SubKey("nexus-backup-key-v1").
func ExportEncrypted(mkm *crypto.MasterKeyManager, opts ExportEncryptedOptions) error {
	if mkm == nil || !mkm.IsEnabled() {
		return ErrEncryptionDisabled
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	if opts.SourceDir == "" {
		return fmt.Errorf("backup: source directory is required")
	}
	if opts.OutputPath == "" {
		return fmt.Errorf("backup: output path is required")
	}

	// Build tar.gz in memory.
	var buf bytes.Buffer
	if err := tarGzDir(opts.SourceDir, &buf); err != nil {
		return fmt.Errorf("backup: archive source: %w", err)
	}
	plaintext := buf.Bytes()

	// Derive backup key and encrypt.
	key := mkm.SubKey(backupDomain)
	blob, err := crypto.SealAES256GCM(key, plaintext, []byte(bfbkMagic))
	if err != nil {
		return fmt.Errorf("backup: encrypt: %w", err)
	}

	// Write output file atomically via temp file + rename.
	tmpPath := opts.OutputPath + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("backup: create output file: %w", err)
	}
	defer func() { _ = os.Remove(tmpPath) }()

	closeOK := false
	defer func() {
		if !closeOK {
			_ = f.Close()
		}
	}()

	// [4-byte magic]
	if _, err := f.WriteString(bfbkMagic); err != nil {
		return fmt.Errorf("backup: write magic: %w", err)
	}
	// [4-byte version big-endian]
	var vbuf [4]byte
	binary.BigEndian.PutUint32(vbuf[:], bfbkVersion)
	if _, err := f.Write(vbuf[:]); err != nil {
		return fmt.Errorf("backup: write version: %w", err)
	}
	// [nonce||ciphertext||tag]
	if _, err := f.Write(blob); err != nil {
		return fmt.Errorf("backup: write ciphertext: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("backup: sync output: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("backup: close output: %w", err)
	}
	closeOK = true

	if err := os.Rename(tmpPath, opts.OutputPath); err != nil {
		return fmt.Errorf("backup: rename to final path: %w", err)
	}

	logger.Info("encrypted backup created",
		"output", opts.OutputPath,
		"source", opts.SourceDir,
		"plaintext_bytes", len(plaintext),
		"encrypted_bytes", headerSize+len(blob),
	)
	return nil
}

// ImportEncrypted decrypts and restores an encrypted .bfbk backup file.
//
// Without Force, returns an error if any target file already exists.
func ImportEncrypted(mkm *crypto.MasterKeyManager, opts ImportEncryptedOptions) error {
	if mkm == nil || !mkm.IsEnabled() {
		return ErrEncryptionDisabled
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	if opts.InputPath == "" {
		return fmt.Errorf("backup: input path is required")
	}

	raw, err := os.ReadFile(opts.InputPath)
	if err != nil {
		return fmt.Errorf("backup: read backup file: %w", err)
	}

	if len(raw) < headerSize {
		return fmt.Errorf("backup: file too short (%d bytes, minimum %d)", len(raw), headerSize)
	}

	// Verify magic.
	if string(raw[:4]) != bfbkMagic {
		return fmt.Errorf("backup: invalid magic bytes (not a BFBK archive)")
	}

	// Verify version.
	ver := binary.BigEndian.Uint32(raw[4:8])
	if ver != bfbkVersion {
		return fmt.Errorf("backup: unsupported version %d (only version 1 is supported)", ver)
	}

	// Decrypt.
	key := mkm.SubKey(backupDomain)
	plaintext, err := crypto.OpenAES256GCM(key, raw[headerSize:], []byte(bfbkMagic))
	if err != nil {
		return fmt.Errorf("backup: decrypt failed (wrong key or corrupted file): %w", err)
	}

	destDir := opts.DestDir
	if destDir == "" {
		return fmt.Errorf("backup: destination directory is required")
	}

	// Extract tar.gz to destDir.
	if err := extractTarGz(bytes.NewReader(plaintext), destDir, opts.Force, logger); err != nil {
		return fmt.Errorf("backup: extract archive: %w", err)
	}

	logger.Info("encrypted backup restored",
		"input", opts.InputPath,
		"dest", destDir,
	)
	return nil
}

// tarGzDir writes a gzip-compressed tar archive of dir to w.
// All file paths in the archive are relative to dir.
func tarGzDir(dir string, w io.Writer) error {
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)

	err := filepath.Walk(dir, func(path string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if fi.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return fmt.Errorf("rel path: %w", err)
		}
		// Normalize to forward slashes for portability.
		rel = filepath.ToSlash(rel)

		hdr := &tar.Header{
			Name:    rel,
			Mode:    0600,
			Size:    fi.Size(),
			ModTime: fi.ModTime(),
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return fmt.Errorf("tar header for %s: %w", rel, err)
		}

		f, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("open %s: %w", path, err)
		}
		defer func() { _ = f.Close() }()

		if _, err := io.Copy(tw, f); err != nil {
			return fmt.Errorf("copy %s: %w", rel, err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	if err := tw.Close(); err != nil {
		return fmt.Errorf("close tar: %w", err)
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("close gzip: %w", err)
	}
	return nil
}

// extractTarGz reads a gzip-compressed tar archive from r and extracts files
// under destDir. Without force, returns an error if any target file exists.
func extractTarGz(r io.Reader, destDir string, force bool, logger *slog.Logger) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("open gzip reader: %w", err)
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar entry: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}

		// Guard against path traversal.
		clean := filepath.Clean(filepath.FromSlash(hdr.Name))
		if filepath.IsAbs(clean) || hasPathTraversal(clean) {
			return fmt.Errorf("backup: suspicious path in archive: %q", hdr.Name)
		}

		target := filepath.Join(destDir, clean)

		if !force {
			if _, err := os.Stat(target); err == nil {
				return fmt.Errorf("backup: file exists: %s (use --force to overwrite)", target)
			}
		}

		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return fmt.Errorf("mkdir %s: %w", filepath.Dir(target), err)
		}

		f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
		if err != nil {
			return fmt.Errorf("create %s: %w", target, err)
		}

		_, copyErr := io.Copy(f, tr)
		closeErr := f.Close()
		if copyErr != nil {
			return fmt.Errorf("write %s: %w", hdr.Name, copyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close %s: %w", target, closeErr)
		}

		logger.Info("extracted", "file", hdr.Name)
	}
	return nil
}

// hasPathTraversal returns true if p contains ".." path components.
func hasPathTraversal(p string) bool {
	return containsDotDot(p)
}

// containsDotDot checks path components for "..".
func containsDotDot(p string) bool {
	for p != "" {
		var seg string
		if i := indexSep(p); i < 0 {
			seg, p = p, ""
		} else {
			seg, p = p[:i], p[i+1:]
		}
		if seg == ".." {
			return true
		}
	}
	return false
}

func indexSep(p string) int {
	for i, c := range p {
		if c == '/' || c == os.PathSeparator {
			return i
		}
	}
	return -1
}
