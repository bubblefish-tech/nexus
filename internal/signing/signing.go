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

// Package signing implements HMAC-SHA256 config signing and verification for
// BubbleFish Nexus compiled config files. The sign-config CLI writes *.sig
// files alongside each compiled *.json; the daemon verifies them at startup
// and on hot reload when [daemon.signing] enabled = true.
//
// Reference: Tech Spec Section 6.5.
package signing

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/bubblefish-tech/nexus/internal/fsutil"
)

// SecurityEventFunc is a callback invoked when a signature verification
// failure occurs. The eventType is always "config_signature_invalid".
type SecurityEventFunc func(eventType string, attrs ...slog.Attr)

// SignFile computes HMAC-SHA256 over the contents of jsonPath using key
// and writes the hex-encoded signature to jsonPath + ".sig" atomically
// (temp file + fsync + rename in the same directory).
//
// Permissions: sig file is 0600.
func SignFile(jsonPath string, key []byte) error {
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		return fmt.Errorf("signing: read %q: %w", jsonPath, err)
	}

	sig := computeHMAC(data, key)
	sigPath := jsonPath + ".sig"

	// Atomic write: temp file in same directory → fsync → rename.
	dir := filepath.Dir(jsonPath)
	tmpFile, err := os.CreateTemp(dir, "sig-*.tmp")
	if err != nil {
		return fmt.Errorf("signing: create temp file in %q: %w", dir, err)
	}
	tmpPath := tmpFile.Name()

	renamed := false
	defer func() {
		_ = tmpFile.Close()
		if !renamed {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmpFile.WriteString(sig + "\n"); err != nil {
		return fmt.Errorf("signing: write temp file %q: %w", tmpPath, err)
	}
	if err := tmpFile.Chmod(0600); err != nil {
		return fmt.Errorf("signing: chmod temp file %q: %w", tmpPath, err)
	}
	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("signing: fsync temp file %q: %w", tmpPath, err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("signing: close temp file %q: %w", tmpPath, err)
	}

	if err := fsutil.RobustRename(tmpPath, sigPath); err != nil {
		return fmt.Errorf("signing: rename %q → %q: %w", tmpPath, sigPath, err)
	}
	renamed = true
	return nil
}

// VerifyFile checks that jsonPath has a valid *.sig sidecar containing the
// correct HMAC-SHA256 for the file contents. Returns nil on success.
//
// If onEvent is non-nil, it is called with event type "config_signature_invalid"
// on any verification failure.
func VerifyFile(jsonPath string, key []byte, onEvent SecurityEventFunc) error {
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		emitEvent(onEvent, jsonPath, "file_read_error")
		return fmt.Errorf("signing: read %q: %w", jsonPath, err)
	}

	sigPath := jsonPath + ".sig"
	sigData, err := os.ReadFile(sigPath)
	if err != nil {
		emitEvent(onEvent, jsonPath, "sig_file_missing")
		return fmt.Errorf("signing: read signature %q: %w", sigPath, err)
	}

	expectedHex := strings.TrimSpace(string(sigData))
	if !validateHMAC(data, key, expectedHex) {
		emitEvent(onEvent, jsonPath, "hmac_mismatch")
		return fmt.Errorf("signing: invalid signature for %q", jsonPath)
	}
	return nil
}

// SignAll signs every *.json file in compiledDir. Returns an error on the
// first failure. The key is never logged.
func SignAll(compiledDir string, key []byte, logger *slog.Logger) error {
	files, err := filepath.Glob(filepath.Join(compiledDir, "*.json"))
	if err != nil {
		return fmt.Errorf("signing: glob compiled dir %q: %w", compiledDir, err)
	}
	if len(files) == 0 {
		return fmt.Errorf("signing: no *.json files found in %q", compiledDir)
	}

	for _, f := range files {
		if err := SignFile(f, key); err != nil {
			return err
		}
		if logger != nil {
			logger.Info("signing: signed config file",
				"component", "signing",
				"file", f,
			)
		}
	}
	return nil
}

// VerifyAll verifies the HMAC-SHA256 signature of every *.json file in
// compiledDir. Returns an error on the first failure. If onEvent is non-nil,
// it is called for each verification failure.
func VerifyAll(compiledDir string, key []byte, onEvent SecurityEventFunc, logger *slog.Logger) error {
	files, err := filepath.Glob(filepath.Join(compiledDir, "*.json"))
	if err != nil {
		return fmt.Errorf("signing: glob compiled dir %q: %w", compiledDir, err)
	}
	if len(files) == 0 {
		return fmt.Errorf("signing: no *.json files found in %q", compiledDir)
	}

	for _, f := range files {
		if err := VerifyFile(f, key, onEvent); err != nil {
			return err
		}
		if logger != nil {
			logger.Debug("signing: verified config file",
				"component", "signing",
				"file", f,
			)
		}
	}
	return nil
}

// computeHMAC returns the hex-encoded HMAC-SHA256 of data using key.
func computeHMAC(data, key []byte) string {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil))
}

// validateHMAC checks that expectedHex matches the HMAC-SHA256 of data
// using key. Uses hmac.Equal for constant-time comparison.
func validateHMAC(data, key []byte, expectedHex string) bool {
	expected, err := hex.DecodeString(expectedHex)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return hmac.Equal(mac.Sum(nil), expected)
}

// emitEvent fires the security event callback if non-nil.
func emitEvent(onEvent SecurityEventFunc, file, reason string) {
	if onEvent == nil {
		return
	}
	onEvent("config_signature_invalid",
		slog.String("file", file),
		slog.String("reason", reason),
	)
}
