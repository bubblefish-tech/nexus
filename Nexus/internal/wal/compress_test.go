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

package wal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"testing"
)

func TestCompressDecompress_Roundtrip(t *testing.T) {
	t.Helper()
	input := []byte(`{"version":2,"payload_id":"test-001","content":"hello world"}`)
	compressed := compressPayload(input)

	// Must have zstd prefix.
	if len(compressed) < 5 || compressed[:5] != compressPrefix {
		t.Fatalf("expected zstd: prefix, got %q", compressed[:10])
	}

	decompressed, wasCompressed, err := decompressPayload([]byte(compressed))
	if err != nil {
		t.Fatalf("decompress failed: %v", err)
	}
	if !wasCompressed {
		t.Fatal("expected wasCompressed = true")
	}
	if !bytes.Equal(input, decompressed) {
		t.Errorf("roundtrip mismatch:\n  input:        %s\n  decompressed: %s", input, decompressed)
	}
}

func TestDecompress_UncompressedPassthrough(t *testing.T) {
	input := []byte(`{"version":2,"payload_id":"test"}`)
	out, wasCompressed, err := decompressPayload(input)
	if err != nil {
		t.Fatal(err)
	}
	if wasCompressed {
		t.Fatal("expected wasCompressed = false for uncompressed input")
	}
	if !bytes.Equal(input, out) {
		t.Error("passthrough should return identical bytes")
	}
}

func TestCompressedWAL_WriteAndReplay(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	// Open WAL WITH compression.
	w, err := Open(dir, 50, logger, WithCompression())
	if err != nil {
		t.Fatal(err)
	}

	// Write 10 entries.
	for i := 0; i < 10; i++ {
		if err := w.Append(Entry{
			PayloadID:   fmt.Sprintf("c-%03d", i),
			Source:      "test",
			Destination: "sqlite",
			Payload:     json.RawMessage(fmt.Sprintf(`{"i":%d,"content":"compressed entry number %d with enough text to benefit from compression"}`, i, i)),
		}); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	// Reopen WITHOUT compression flag (replay should auto-detect).
	w2, err := Open(dir, 50, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w2.Close() }()

	var replayed []Entry
	if err := w2.Replay(func(e Entry) {
		replayed = append(replayed, e)
	}); err != nil {
		t.Fatal(err)
	}

	if len(replayed) != 10 {
		t.Fatalf("expected 10 replayed entries, got %d", len(replayed))
	}
	for i, e := range replayed {
		expected := fmt.Sprintf("c-%03d", i)
		if e.PayloadID != expected {
			t.Errorf("entry %d: payload_id=%q, want %q", i, e.PayloadID, expected)
		}
	}
}

func TestMixedCompressedAndUncompressed(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	// Write 5 uncompressed entries.
	w1, err := Open(dir, 50, logger)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		_ = w1.Append(Entry{
			PayloadID:   fmt.Sprintf("u-%03d", i),
			Source:      "test",
			Destination: "sqlite",
			Payload:     json.RawMessage(`{"type":"uncompressed"}`),
		})
	}
	_ = w1.Close()

	// Reopen WITH compression and write 5 more.
	w2, err := Open(dir, 50, logger, WithCompression())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		_ = w2.Append(Entry{
			PayloadID:   fmt.Sprintf("c-%03d", i),
			Source:      "test",
			Destination: "sqlite",
			Payload:     json.RawMessage(`{"type":"compressed"}`),
		})
	}
	_ = w2.Close()

	// Replay all — should get 10 entries (5 uncompressed + 5 compressed).
	w3, err := Open(dir, 50, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w3.Close() }()

	var replayed []Entry
	_ = w3.Replay(func(e Entry) {
		replayed = append(replayed, e)
	})

	if len(replayed) != 10 {
		t.Fatalf("expected 10 entries, got %d", len(replayed))
	}

	// First 5 should be uncompressed IDs.
	for i := 0; i < 5; i++ {
		if replayed[i].PayloadID != fmt.Sprintf("u-%03d", i) {
			t.Errorf("entry %d: got %q, want u-%03d", i, replayed[i].PayloadID, i)
		}
	}
	// Last 5 should be compressed IDs.
	for i := 0; i < 5; i++ {
		if replayed[i+5].PayloadID != fmt.Sprintf("c-%03d", i) {
			t.Errorf("entry %d: got %q, want c-%03d", i+5, replayed[i+5].PayloadID, i)
		}
	}
}
