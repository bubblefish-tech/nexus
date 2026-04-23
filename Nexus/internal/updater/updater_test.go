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

package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	t.Helper()
	cases := []struct {
		current   string
		candidate string
		want      bool
	}{
		{"0.1.3", "0.1.4", true},
		{"0.1.4", "0.1.3", false},
		{"0.1.3", "0.1.3", false},
		{"0.1.3", "v0.1.4", true},
		{"1.0.0", "2.0.0", true},
		{"2.0.0", "1.9.9", false},
		{"0.1.3", "1.0.0", true},
	}
	for _, tc := range cases {
		got := CompareVersions(tc.current, tc.candidate)
		if got != tc.want {
			t.Errorf("CompareVersions(%q, %q) = %v, want %v", tc.current, tc.candidate, got, tc.want)
		}
	}
}

func TestPlatformAssetName(t *testing.T) {
	t.Helper()
	name := PlatformAssetName()
	if name == "" {
		t.Fatal("PlatformAssetName returned empty string")
	}
	if runtime.GOOS == "windows" && !endsWith(name, ".exe") {
		t.Errorf("expected .exe suffix on Windows, got %q", name)
	}
}

func endsWith(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

func TestFetchLatest_OK(t *testing.T) {
	t.Helper()
	info := ReleaseInfo{
		TagName: "v0.1.4",
		Assets:  []ReleaseAsset{{Name: "nexus_linux_amd64", BrowserDownloadURL: "https://example.com/bin"}},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(info)
	}))
	defer srv.Close()

	// Temporarily override via direct HTTP call using the test server URL.
	client := srv.Client()
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var got ReleaseInfo
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Version() != "0.1.4" {
		t.Errorf("Version() = %q, want 0.1.4", got.Version())
	}
}

func TestFetchLatest_NotFound(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	// Patch releasesAPI for this test via a wrapper that hits srv.URL.
	client := &http.Client{}
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestFindAssets_Found(t *testing.T) {
	t.Helper()
	wantBin := PlatformAssetName()
	wantSum := wantBin + ".sha256"
	info := &ReleaseInfo{
		TagName: "v0.1.4",
		Assets: []ReleaseAsset{
			{Name: wantBin, BrowserDownloadURL: "https://example.com/bin"},
			{Name: wantSum, BrowserDownloadURL: "https://example.com/sum"},
		},
	}
	binURL, sumURL, err := FindAssets(info)
	if err != nil {
		t.Fatalf("FindAssets: %v", err)
	}
	if binURL != "https://example.com/bin" {
		t.Errorf("binURL = %q", binURL)
	}
	if sumURL != "https://example.com/sum" {
		t.Errorf("sumURL = %q", sumURL)
	}
}

func TestFindAssets_Missing(t *testing.T) {
	t.Helper()
	info := &ReleaseInfo{TagName: "v0.1.4", Assets: []ReleaseAsset{}}
	_, _, err := FindAssets(info)
	if err == nil {
		t.Fatal("expected error for missing assets")
	}
}

func TestVerifyChecksum_OK(t *testing.T) {
	t.Helper()
	dir := t.TempDir()

	// Write binary content.
	content := []byte("fake binary content")
	binPath := filepath.Join(dir, "bin")
	if err := os.WriteFile(binPath, content, 0600); err != nil {
		t.Fatal(err)
	}

	// Compute expected checksum.
	h := sha256.Sum256(content)
	checksum := hex.EncodeToString(h[:])
	sumPath := filepath.Join(dir, "bin.sha256")
	if err := os.WriteFile(sumPath, []byte(checksum+"  bin\n"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := VerifyChecksum(binPath, sumPath); err != nil {
		t.Errorf("VerifyChecksum: %v", err)
	}
}

func TestVerifyChecksum_Mismatch(t *testing.T) {
	t.Helper()
	dir := t.TempDir()

	binPath := filepath.Join(dir, "bin")
	if err := os.WriteFile(binPath, []byte("content"), 0600); err != nil {
		t.Fatal(err)
	}
	wrongSum := fmt.Sprintf("%064x  bin\n", 0)
	sumPath := filepath.Join(dir, "bin.sha256")
	if err := os.WriteFile(sumPath, []byte(wrongSum), 0600); err != nil {
		t.Fatal(err)
	}

	if err := VerifyChecksum(binPath, sumPath); err == nil {
		t.Fatal("expected checksum mismatch error")
	}
}

func TestAtomicReplace(t *testing.T) {
	t.Helper()
	dir := t.TempDir()

	// Create "current" binary.
	destPath := filepath.Join(dir, "nexus")
	if err := os.WriteFile(destPath, []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}

	// Create "new" binary.
	srcPath := filepath.Join(dir, "nexus.new")
	if err := os.WriteFile(srcPath, []byte("new"), 0755); err != nil {
		t.Fatal(err)
	}

	backupPath, err := AtomicReplace(destPath, srcPath)
	if err != nil {
		t.Fatalf("AtomicReplace: %v", err)
	}

	// Verify new binary is in place.
	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(got) != "new" {
		t.Errorf("dest content = %q, want %q", got, "new")
	}

	// Verify backup exists.
	bak, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(bak) != "old" {
		t.Errorf("backup content = %q, want %q", bak, "old")
	}
}

func TestDownload_OK(t *testing.T) {
	t.Helper()
	content := []byte("binary data")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(content)
	}))
	defer srv.Close()

	dir := t.TempDir()
	path, err := Download(srv.Client(), srv.URL, dir)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	defer func() { _ = os.Remove(path) }()

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("content = %q, want %q", got, content)
	}
}

func TestDownload_ServerError(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := Download(srv.Client(), srv.URL, t.TempDir())
	if err == nil {
		t.Fatal("expected error on 500")
	}
}
