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
	"context"
	"errors"
	"os"
	"testing"
)

func TestBuiltinProvider_MissingModel(t *testing.T) {
	t.Helper()
	cfg := BuiltinConfig{
		ModelPath:  "/nonexistent/model.gguf",
		ServerPath: "/nonexistent/llama-server",
	}
	_, err := NewBuiltinProvider(cfg)
	if err == nil {
		t.Fatal("expected error for missing model")
	}
	if !os.IsNotExist(errors.Unwrap(err)) {
		t.Logf("error (acceptable): %v", err)
	}
}

func TestBuiltinProvider_MissingServer(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	modelFile := tmp + "/fake.gguf"
	os.WriteFile(modelFile, []byte("fake"), 0600)

	cfg := BuiltinConfig{
		ModelPath:  modelFile,
		ServerPath: "/nonexistent/llama-server",
	}
	_, err := NewBuiltinProvider(cfg)
	if err == nil {
		t.Fatal("expected error for missing server")
	}
}

func TestBuiltinProvider_Dimensions(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	modelFile := tmp + "/fake.gguf"
	serverFile := tmp + "/fake-server"
	os.WriteFile(modelFile, []byte("fake"), 0600)
	os.WriteFile(serverFile, []byte("fake"), 0755)

	cfg := BuiltinConfig{
		ModelPath:  modelFile,
		ServerPath: serverFile,
		Dimensions: 768,
	}
	p, err := NewBuiltinProvider(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Dimensions() != 768 {
		t.Fatalf("expected 768 dimensions, got %d", p.Dimensions())
	}
}

func TestBuiltinProvider_EmbedBeforeStart(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	modelFile := tmp + "/fake.gguf"
	serverFile := tmp + "/fake-server"
	os.WriteFile(modelFile, []byte("fake"), 0600)
	os.WriteFile(serverFile, []byte("fake"), 0755)

	p, _ := NewBuiltinProvider(BuiltinConfig{
		ModelPath:  modelFile,
		ServerPath: serverFile,
		Dimensions: 768,
	})

	_, err := p.Embed(context.Background(), "test")
	if !errors.Is(err, ErrEmbeddingUnavailable) {
		t.Fatalf("expected ErrEmbeddingUnavailable, got %v", err)
	}
}

func TestBuiltinProvider_Close_Idempotent(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	modelFile := tmp + "/fake.gguf"
	serverFile := tmp + "/fake-server"
	os.WriteFile(modelFile, []byte("fake"), 0600)
	os.WriteFile(serverFile, []byte("fake"), 0755)

	p, _ := NewBuiltinProvider(BuiltinConfig{
		ModelPath:  modelFile,
		ServerPath: serverFile,
		Dimensions: 768,
	})

	if err := p.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func TestBuiltinProvider_FreePort(t *testing.T) {
	t.Helper()
	port, err := freePort()
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	if port < 1024 || port > 65535 {
		t.Fatalf("port %d out of range", port)
	}
}

func TestBuiltinProvider_HealthStatus_BeforeStart(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	modelFile := tmp + "/fake.gguf"
	serverFile := tmp + "/fake-server"
	os.WriteFile(modelFile, []byte("fake"), 0600)
	os.WriteFile(serverFile, []byte("fake"), 0755)

	p, _ := NewBuiltinProvider(BuiltinConfig{
		ModelPath:  modelFile,
		ServerPath: serverFile,
	})

	status, _ := p.HealthStatus()
	if status != "stopped" {
		t.Fatalf("expected 'stopped', got %q", status)
	}
}

func TestBuiltinProvider_Integration(t *testing.T) {
	if os.Getenv("NEXUS_TEST_BUILTIN_EMBEDDING") == "" {
		t.Skip("set NEXUS_TEST_BUILTIN_EMBEDDING=1 to run")
	}

	cfg := DefaultBuiltinConfig("D:\\BubbleFish\\Nexus")
	p, err := NewBuiltinProvider(cfg)
	if err != nil {
		t.Fatalf("NewBuiltinProvider: %v", err)
	}
	defer p.Close()

	ctx := context.Background()
	if err := p.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	vec, err := p.Embed(ctx, "hello world")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vec) != 768 {
		t.Fatalf("expected 768 dims, got %d", len(vec))
	}
	t.Logf("dims=%d first3=%v", len(vec), vec[:3])
}
