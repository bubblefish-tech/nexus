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

// Package updater implements self-update logic for the bubblefish binary.
//
// The update flow is:
//  1. Fetch latest release metadata from the GitHub releases API.
//  2. Compare with the running version — abort if already current.
//  3. Download the platform binary and its SHA-256 checksum sidecar.
//  4. Verify the checksum before writing anything to disk.
//  5. Atomically replace the current executable (rename-then-write pattern).
//  6. Restore the backup on any failure.
package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	// releasesAPI is the GitHub API endpoint for the latest release.
	releasesAPI = "https://api.github.com/repos/bubblefish-tech/nexus/releases/latest"

	// httpTimeout caps all outbound HTTP requests made by the updater.
	httpTimeout = 30 * time.Second
)

// ReleaseInfo holds the parsed response from the GitHub releases API.
type ReleaseInfo struct {
	TagName string        `json:"tag_name"` // e.g. "v0.1.4"
	Assets  []ReleaseAsset `json:"assets"`
}

// Version returns the version string without a leading "v".
func (r ReleaseInfo) Version() string {
	return strings.TrimPrefix(r.TagName, "v")
}

// ReleaseAsset is one downloadable file in a GitHub release.
type ReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// FetchLatest retrieves the latest release metadata. Uses the provided HTTP
// client so tests can inject a stub server.
func FetchLatest(client *http.Client) (*ReleaseInfo, error) {
	if client == nil {
		client = &http.Client{Timeout: httpTimeout}
	}
	req, err := http.NewRequest(http.MethodGet, releasesAPI, nil)
	if err != nil {
		return nil, fmt.Errorf("updater: build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("updater: fetch releases: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("updater: no releases published yet")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("updater: releases API returned %d", resp.StatusCode)
	}

	var info ReleaseInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("updater: parse release: %w", err)
	}
	return &info, nil
}

// PlatformAssetName returns the expected binary asset name for the current OS/arch.
// Convention: "bubblefish_<os>_<arch>[.exe]"
func PlatformAssetName() string {
	name := fmt.Sprintf("bubblefish_%s_%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

// FindAssets locates the binary and its SHA-256 checksum sidecar in a release.
// Returns ("", "", err) when either asset is missing.
func FindAssets(info *ReleaseInfo) (binURL, sumURL string, err error) {
	want := PlatformAssetName()
	wantSum := want + ".sha256"
	for _, a := range info.Assets {
		switch a.Name {
		case want:
			binURL = a.BrowserDownloadURL
		case wantSum:
			sumURL = a.BrowserDownloadURL
		}
	}
	if binURL == "" {
		return "", "", fmt.Errorf("updater: no asset for %s in release %s", want, info.TagName)
	}
	if sumURL == "" {
		return "", "", fmt.Errorf("updater: no checksum asset for %s in release %s", want, info.TagName)
	}
	return binURL, sumURL, nil
}

// Download fetches url into a temp file inside dir and returns its path.
// The caller is responsible for removing the temp file on failure.
func Download(client *http.Client, url, dir string) (string, error) {
	if client == nil {
		client = &http.Client{Timeout: httpTimeout}
	}
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("updater: download %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("updater: download %s: status %d", url, resp.StatusCode)
	}

	f, err := os.CreateTemp(dir, ".nexus-update-*")
	if err != nil {
		return "", fmt.Errorf("updater: temp file: %w", err)
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", fmt.Errorf("updater: write download: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return "", fmt.Errorf("updater: close download: %w", err)
	}
	return f.Name(), nil
}

// VerifyChecksum reads sumPath (a file containing "<hex>  <name>" lines) and
// checks that the SHA-256 of binPath matches. Returns nil on success.
func VerifyChecksum(binPath, sumPath string) error {
	sumBytes, err := os.ReadFile(sumPath)
	if err != nil {
		return fmt.Errorf("updater: read checksum: %w", err)
	}

	// Parse first hex token.
	line := strings.TrimSpace(string(sumBytes))
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return fmt.Errorf("updater: empty checksum file")
	}
	want := strings.ToLower(parts[0])
	if len(want) != 64 {
		return fmt.Errorf("updater: checksum is not a hex SHA-256 (%d chars)", len(want))
	}

	f, err := os.Open(binPath)
	if err != nil {
		return fmt.Errorf("updater: open binary for hash: %w", err)
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("updater: hash binary: %w", err)
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != want {
		return fmt.Errorf("updater: checksum mismatch — want %s got %s", want, got)
	}
	return nil
}

// AtomicReplace atomically replaces destPath with srcPath.
//
// Strategy:
//  1. Rename destPath → destPath + ".bak" (preserves original on failure).
//  2. Copy srcPath → destPath with the original file's permissions.
//  3. On any error after step 1, restore the backup.
//
// On Windows the running binary cannot be overwritten; renaming it first works
// because the OS holds a handle to the original inode, not the name.
func AtomicReplace(destPath, srcPath string) (backupPath string, err error) {
	// Resolve the real executable path on Windows (may have a symlink).
	info, statErr := os.Stat(destPath)
	if statErr != nil {
		return "", fmt.Errorf("updater: stat dest %s: %w", destPath, statErr)
	}
	mode := info.Mode()

	backupPath = destPath + ".bak"
	if err := os.Rename(destPath, backupPath); err != nil {
		return "", fmt.Errorf("updater: rename to backup: %w", err)
	}

	if err := copyFile(srcPath, destPath, mode); err != nil {
		// Restore backup.
		_ = os.Remove(destPath)
		_ = os.Rename(backupPath, destPath)
		return "", fmt.Errorf("updater: install new binary: %w", err)
	}
	return backupPath, nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// CompareVersions returns true if candidate is strictly newer than current.
// Both strings must be in "major.minor.patch" form (leading "v" is stripped).
func CompareVersions(current, candidate string) bool {
	cv := parseVer(current)
	nv := parseVer(candidate)
	for i := range cv {
		if nv[i] > cv[i] {
			return true
		}
		if nv[i] < cv[i] {
			return false
		}
	}
	return false
}

func parseVer(s string) [3]int {
	s = strings.TrimPrefix(s, "v")
	var maj, min, pat int
	_, _ = fmt.Sscanf(s, "%d.%d.%d", &maj, &min, &pat)
	return [3]int{maj, min, pat}
}

// CurrentExecutable returns os.Executable() resolved to an absolute path.
func CurrentExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("updater: locate self: %w", err)
	}
	return filepath.Abs(exe)
}
