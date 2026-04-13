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
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	_ "modernc.org/sqlite" // SQLite driver for read-only admin queries.
)

const (
	adminListDefaultLimit = 200
	adminListMaxLimit     = 1000
)

type adminListRow struct {
	PayloadID   string `json:"payload_id"`
	CreatedAt   string `json:"created_at"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
}

type adminListEnvelope struct {
	ResultCount int    `json:"result_count"`
	HasMore     bool   `json:"has_more"`
	NextCursor  string `json:"next_cursor,omitempty"`
}

type adminListResponse struct {
	Memories []adminListRow    `json:"memories"`
	Admin    adminListEnvelope `json:"_admin"`
}

// handleAdminList implements GET /admin/memories.
//
// Cursor format: base64("<created_at>|<payload_id>"). Strict-greater-than
// tuple ordering on (created_at, payload_id) ensures every row is returned
// exactly once even under concurrent writes, because new rows always sort
// after the cursor position.
func (d *Daemon) handleAdminList(w http.ResponseWriter, r *http.Request) {
	// Parse limit.
	limit := adminListDefaultLimit
	if s := r.URL.Query().Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n <= 0 {
			d.writeErrorResponse(w, r, http.StatusBadRequest, "invalid_limit",
				"limit must be a positive integer", 0)
			return
		}
		if n > adminListMaxLimit {
			n = adminListMaxLimit
		}
		limit = n
	}

	// Decode cursor.
	var cursorTS, cursorID string
	if c := r.URL.Query().Get("cursor"); c != "" {
		raw, err := base64.StdEncoding.DecodeString(c)
		if err != nil {
			d.writeErrorResponse(w, r, http.StatusBadRequest, "invalid_cursor",
				"cursor is not valid base64", 0)
			return
		}
		parts := strings.SplitN(string(raw), "|", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			d.writeErrorResponse(w, r, http.StatusBadRequest, "invalid_cursor",
				"cursor format must be base64 of '<created_at>|<payload_id>'", 0)
			return
		}
		cursorTS, cursorID = parts[0], parts[1]
	}

	// Open SQLite read-only. One connection per request — admin endpoints
	// are low-traffic, and this avoids coupling to the destination interface.
	dbPath, err := d.resolveSQLitePath()
	if err != nil {
		d.writeErrorResponse(w, r, http.StatusInternalServerError, "db_path_error",
			fmt.Sprintf("resolve SQLite path: %v", err), 0)
		return
	}
	db, err := sql.Open("sqlite", dbPath+"?mode=ro")
	if err != nil {
		d.writeErrorResponse(w, r, http.StatusInternalServerError, "db_open_error",
			fmt.Sprintf("open SQLite read-only: %v", err), 0)
		return
	}
	defer db.Close()

	// Query: strict-greater tuple comparison.
	// Over-fetch by 1 to detect has_more without a separate count query.
	var query string
	var args []interface{}
	if cursorTS == "" && cursorID == "" {
		query = `
			SELECT payload_id, created_at, source, destination
			FROM memories
			ORDER BY created_at ASC, payload_id ASC
			LIMIT ?
		`
		args = []interface{}{limit + 1}
	} else {
		query = `
			SELECT payload_id, created_at, source, destination
			FROM memories
			WHERE (created_at, payload_id) > (?, ?)
			ORDER BY created_at ASC, payload_id ASC
			LIMIT ?
		`
		args = []interface{}{cursorTS, cursorID, limit + 1}
	}

	rows, err := db.QueryContext(r.Context(), query, args...)
	if err != nil {
		d.writeErrorResponse(w, r, http.StatusInternalServerError, "query_error",
			fmt.Sprintf("query failed: %v", err), 0)
		return
	}
	defer rows.Close()

	out := make([]adminListRow, 0, limit)
	for rows.Next() {
		var row adminListRow
		if err := rows.Scan(&row.PayloadID, &row.CreatedAt, &row.Source, &row.Destination); err != nil {
			d.writeErrorResponse(w, r, http.StatusInternalServerError, "scan_error",
				fmt.Sprintf("scan failed: %v", err), 0)
			return
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		d.writeErrorResponse(w, r, http.StatusInternalServerError, "rows_error",
			fmt.Sprintf("rows iteration failed: %v", err), 0)
		return
	}

	hasMore := len(out) > limit
	if hasMore {
		out = out[:limit]
	}

	var nextCursor string
	if hasMore && len(out) > 0 {
		last := out[len(out)-1]
		raw := last.CreatedAt + "|" + last.PayloadID
		nextCursor = base64.StdEncoding.EncodeToString([]byte(raw))
	}

	resp := adminListResponse{
		Memories: out,
		Admin: adminListEnvelope{
			ResultCount: len(out),
			HasMore:     hasMore,
			NextCursor:  nextCursor,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		d.logger.Error("admin_list: encode failed", "error", err)
	}
}
