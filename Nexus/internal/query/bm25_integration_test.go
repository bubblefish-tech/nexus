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

package query_test

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/bubblefish-tech/nexus/internal/destination"
	"github.com/bubblefish-tech/nexus/internal/query"
)

func TestBM25Search_Integration(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	dest, err := destination.OpenSQLite(dbPath, slog.Default())
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer dest.Close()

	if err := dest.Migrate(context.Background(), 0); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	memories := []destination.TranslatedPayload{
		{PayloadID: "m1", Content: "TPS-42 quarterly report discussion", Namespace: "default", Destination: "sqlite", Source: "test"},
		{PayloadID: "m2", Content: "lunch plans for friday", Namespace: "default", Destination: "sqlite", Source: "test"},
		{PayloadID: "m3", Content: "running the deployment pipeline", Namespace: "default", Destination: "sqlite", Source: "test"},
	}
	for _, m := range memories {
		if err := dest.Write(m); err != nil {
			t.Fatalf("write %s: %v", m.PayloadID, err)
		}
	}

	searcher := &query.SQLBM25Searcher{DB: dest.DB()}

	results, err := searcher.BM25Search(context.Background(), "TPS-42", "default", 10)
	if err != nil {
		t.Fatalf("BM25Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result for 'TPS-42'")
	}
	if results[0].MemoryID != "m1" {
		t.Errorf("expected first result to be m1, got %s", results[0].MemoryID)
	}
}

func TestBM25Search_EmptyQuery(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	dest, err := destination.OpenSQLite(dbPath, slog.Default())
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer dest.Close()

	searcher := &query.SQLBM25Searcher{DB: dest.DB()}
	results, err := searcher.BM25Search(context.Background(), "", "default", 10)
	if err != nil {
		t.Fatalf("BM25Search empty: %v", err)
	}
	if results != nil {
		t.Errorf("expected nil results for empty query, got %d", len(results))
	}
}

func TestBM25Search_PorterStemming(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	dest, err := destination.OpenSQLite(dbPath, slog.Default())
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer dest.Close()

	if err := dest.Migrate(context.Background(), 0); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if err := dest.Write(destination.TranslatedPayload{
		PayloadID: "s1", Content: "the runner was running through the park", Namespace: "ns", Destination: "sqlite", Source: "test",
	}); err != nil {
		t.Fatalf("write: %v", err)
	}

	searcher := &query.SQLBM25Searcher{DB: dest.DB()}
	results, err := searcher.BM25Search(context.Background(), "run", "ns", 10)
	if err != nil {
		t.Fatalf("BM25Search stemming: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected porter stemming to match 'run' against 'running'")
	}
}
