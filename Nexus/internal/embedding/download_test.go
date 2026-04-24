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
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLlamaServerURL_CurrentPlatform(t *testing.T) {
	t.Helper()
	url, err := LlamaServerURL()
	if err != nil {
		t.Fatalf("LlamaServerURL: %v", err)
	}
	if url == "" {
		t.Fatal("expected non-empty URL")
	}
	t.Logf("platform=%s/%s url=%s", runtime.GOOS, runtime.GOARCH, url)
}

func TestLlamaServerBinaryName(t *testing.T) {
	t.Helper()
	name := LlamaServerBinaryName()
	if runtime.GOOS == "windows" {
		if name != "llama-server.exe" {
			t.Fatalf("expected llama-server.exe on windows, got %s", name)
		}
	} else {
		if name != "llama-server" {
			t.Fatalf("expected llama-server on unix, got %s", name)
		}
	}
}

func TestEnsureModelDownloaded_AlreadyExists(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	existing := filepath.Join(dir, NomicModelFile)
	os.WriteFile(existing, []byte("fake model"), 0600)

	err := EnsureModelDownloaded(dir, nil)
	if err != nil {
		t.Fatalf("expected no error for existing model, got: %v", err)
	}
}

func TestEnsureServerDownloaded_AlreadyExists(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	existing := filepath.Join(dir, LlamaServerBinaryName())
	os.WriteFile(existing, []byte("fake server"), 0755)

	err := EnsureServerDownloaded(dir, nil)
	if err != nil {
		t.Fatalf("expected no error for existing server, got: %v", err)
	}
}
