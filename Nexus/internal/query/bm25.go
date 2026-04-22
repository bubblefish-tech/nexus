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

package query

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// BM25Result holds a memory ID and its BM25 rank score from FTS5.
type BM25Result struct {
	MemoryID string
	RowID    int64
	Rank     float64
	Content  string
}

// BM25Searcher can run BM25 sparse keyword queries against an FTS5 index.
type BM25Searcher interface {
	BM25Search(ctx context.Context, query string, namespace string, limit int) ([]BM25Result, error)
}

// SQLBM25Searcher implements BM25Searcher using a *sql.DB with an FTS5 table.
type SQLBM25Searcher struct {
	DB *sql.DB
}

// BM25Search runs a sparse keyword search against the FTS5 index.
// Results are ordered by BM25 relevance (best first).
func (s *SQLBM25Searcher) BM25Search(ctx context.Context, queryText string, namespace string, limit int) ([]BM25Result, error) {
	if queryText == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 100
	}

	queryText = sanitizeFTS5Query(queryText)

	const q = `
		SELECT m.payload_id, m.rowid, rank, m.content
		FROM memories_fts
		JOIN memories m ON memories_fts.rowid = m.rowid
		WHERE memories_fts MATCH ?
		AND (? = '' OR m.namespace = ?)
		ORDER BY rank
		LIMIT ?
	`

	rows, err := s.DB.QueryContext(ctx, q, queryText, namespace, namespace, limit)
	if err != nil {
		return nil, fmt.Errorf("bm25 search: %w", err)
	}
	defer rows.Close()

	var results []BM25Result
	for rows.Next() {
		var r BM25Result
		if err := rows.Scan(&r.MemoryID, &r.RowID, &r.Rank, &r.Content); err != nil {
			return nil, fmt.Errorf("bm25 scan: %w", err)
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// sanitizeFTS5Query wraps each token in double quotes so FTS5 treats hyphens
// and other special characters as literals rather than operators.
func sanitizeFTS5Query(q string) string {
	return "\"" + strings.ReplaceAll(q, "\"", "") + "\""
}
