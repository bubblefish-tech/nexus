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
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	NomicModelURL  = "https://huggingface.co/nomic-ai/nomic-embed-text-v1.5-GGUF/resolve/main/nomic-embed-text-v1.5.Q4_K_S.gguf"
	NomicModelFile = "nomic-embed-text-v1.5.Q4_K_S.gguf"

	LlamaServerVersion = "b8907"
)

// LlamaServerURL returns the download URL for the current platform.
func LlamaServerURL() (string, error) {
	base := fmt.Sprintf("https://github.com/ggml-org/llama.cpp/releases/download/%s", LlamaServerVersion)
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "windows/amd64":
		return base + "/llama-" + LlamaServerVersion + "-bin-win-cpu-x64.zip", nil
	case "linux/amd64":
		return base + "/llama-" + LlamaServerVersion + "-bin-ubuntu-x64.tar.gz", nil
	case "linux/arm64":
		return base + "/llama-" + LlamaServerVersion + "-bin-ubuntu-arm64.tar.gz", nil
	case "darwin/arm64":
		return base + "/llama-" + LlamaServerVersion + "-bin-macos-arm64.tar.gz", nil
	case "darwin/amd64":
		return base + "/llama-" + LlamaServerVersion + "-bin-macos-x64.tar.gz", nil
	default:
		return "", fmt.Errorf("unsupported platform: %s/%s", runtime.GOOS, runtime.GOARCH)
	}
}

// LlamaServerBinaryName returns "llama-server" or "llama-server.exe".
func LlamaServerBinaryName() string {
	if runtime.GOOS == "windows" {
		return "llama-server.exe"
	}
	return "llama-server"
}

// EnsureModelDownloaded downloads the GGUF model if not present.
// progress is called with (bytes_downloaded, total_bytes) for UI updates.
func EnsureModelDownloaded(modelsDir string, progress func(int64, int64)) error {
	dest := filepath.Join(modelsDir, NomicModelFile)
	if _, err := os.Stat(dest); err == nil {
		return nil
	}
	if err := os.MkdirAll(modelsDir, 0755); err != nil {
		return fmt.Errorf("create models dir: %w", err)
	}
	return downloadFile(NomicModelURL, dest, progress)
}

// EnsureServerDownloaded downloads and extracts llama-server if not present.
func EnsureServerDownloaded(modelsDir string, progress func(int64, int64)) error {
	dest := filepath.Join(modelsDir, LlamaServerBinaryName())
	if _, err := os.Stat(dest); err == nil {
		return nil
	}
	if err := os.MkdirAll(modelsDir, 0755); err != nil {
		return fmt.Errorf("create models dir: %w", err)
	}

	url, err := LlamaServerURL()
	if err != nil {
		return err
	}

	archivePath := filepath.Join(modelsDir, "llama-server-download.zip")
	if err := downloadFile(url, archivePath, progress); err != nil {
		return fmt.Errorf("download llama-server: %w", err)
	}

	if err := extractBinaryFromZip(archivePath, "llama-server", dest); err != nil {
		return fmt.Errorf("extract llama-server: %w", err)
	}

	if runtime.GOOS != "windows" {
		os.Chmod(dest, 0755)
	}

	os.Remove(archivePath)
	return nil
}

func downloadFile(url, dest string, progress func(int64, int64)) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d downloading %s", resp.StatusCode, url)
	}

	f, err := os.Create(dest + ".tmp")
	if err != nil {
		return err
	}

	var written int64
	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := f.Write(buf[:n]); writeErr != nil {
				f.Close()
				os.Remove(dest + ".tmp")
				return writeErr
			}
			written += int64(n)
			if progress != nil {
				progress(written, resp.ContentLength)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			f.Close()
			os.Remove(dest + ".tmp")
			return readErr
		}
	}

	if err := f.Close(); err != nil {
		os.Remove(dest + ".tmp")
		return err
	}
	return os.Rename(dest+".tmp", dest)
}

// extractBinaryFromZip finds a binary named name (or name.exe) inside the ZIP
// and extracts it to dest.
func extractBinaryFromZip(zipPath, name, dest string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer r.Close()

	exeName := name
	if runtime.GOOS == "windows" {
		exeName = name + ".exe"
	}

	for _, f := range r.File {
		base := filepath.Base(f.Name)
		if !strings.EqualFold(base, exeName) {
			continue
		}

		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("open entry %s: %w", f.Name, err)
		}

		out, err := os.Create(dest)
		if err != nil {
			rc.Close()
			return fmt.Errorf("create %s: %w", dest, err)
		}

		_, copyErr := io.Copy(out, rc)
		rc.Close()
		if closeErr := out.Close(); closeErr != nil && copyErr == nil {
			copyErr = closeErr
		}
		return copyErr
	}

	return fmt.Errorf("%s not found in %s", exeName, zipPath)
}
