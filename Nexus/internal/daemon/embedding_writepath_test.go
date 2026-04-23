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

package daemon

import (
	"context"
	"errors"
	"log/slog"
	"testing"
)

// fakeEmbedClient implements embedding.EmbeddingClient for testing.
type fakeEmbedClient struct {
	vec    []float32
	err    error
	called bool
}

func (f *fakeEmbedClient) Embed(_ context.Context, _ string) ([]float32, error) {
	f.called = true
	return f.vec, f.err
}

func (f *fakeEmbedClient) BatchEmbed(_ context.Context, texts []string) ([][]float32, error) {
	results := make([][]float32, len(texts))
	for i := range texts {
		results[i] = f.vec
	}
	return results, f.err
}
func (f *fakeEmbedClient) Dimensions() int { return len(f.vec) }
func (f *fakeEmbedClient) Close() error    { return nil }

func TestEmbedContent_NoClient(t *testing.T) {
	t.Helper()
	d := &Daemon{
		logger: slog.Default(),
		// embeddingClient is nil
	}
	got := d.embedContent(context.Background(), "pay-1", "hello world")
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestEmbedContent_EmptyContent(t *testing.T) {
	t.Helper()
	fake := &fakeEmbedClient{vec: []float32{1, 2, 3}}
	d := &Daemon{
		logger:          slog.Default(),
		embeddingClient: fake,
	}
	for _, content := range []string{"", "   ", "\t\n"} {
		got := d.embedContent(context.Background(), "pay-2", content)
		if got != nil {
			t.Fatalf("expected nil for content %q, got %v", content, got)
		}
		if fake.called {
			t.Fatalf("embedder should not be called for content %q", content)
		}
	}
}

func TestEmbedContent_Success(t *testing.T) {
	t.Helper()
	want := []float32{0.1, 0.2, 0.3}
	fake := &fakeEmbedClient{vec: want}
	d := &Daemon{
		logger:          slog.Default(),
		embeddingClient: fake,
	}
	got := d.embedContent(context.Background(), "pay-3", "some content")
	if got == nil {
		t.Fatal("expected non-nil vector")
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d dimensions, got %d", len(want), len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("dimension %d: expected %f, got %f", i, want[i], got[i])
		}
	}
	if !fake.called {
		t.Fatal("embedder should have been called")
	}
}

func TestEmbedContent_ErrorIsGraceful(t *testing.T) {
	t.Helper()
	fake := &fakeEmbedClient{err: errors.New("provider down")}
	d := &Daemon{
		logger:          slog.Default(),
		embeddingClient: fake,
	}
	got := d.embedContent(context.Background(), "pay-4", "some content")
	if got != nil {
		t.Fatalf("expected nil on error, got %v", got)
	}
	if !fake.called {
		t.Fatal("embedder should have been called")
	}
}
