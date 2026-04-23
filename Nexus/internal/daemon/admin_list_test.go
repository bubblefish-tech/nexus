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

package daemon_test

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/bubblefish-tech/nexus/internal/config"
	"github.com/bubblefish-tech/nexus/internal/daemon"

	_ "modernc.org/sqlite"
)

// adminListResponse mirrors the JSON shape of GET /admin/memories.
type adminListResponse struct {
	Memories []struct {
		PayloadID   string `json:"payload_id"`
		CreatedAt   string `json:"created_at"`
		Source      string `json:"source"`
		Destination string `json:"destination"`
	} `json:"memories"`
	Admin struct {
		ResultCount int    `json:"result_count"`
		HasMore     bool   `json:"has_more"`
		NextCursor  string `json:"next_cursor"`
	} `json:"_admin"`
}

// setupAdminListTest creates a temp SQLite DB, inserts rows, and returns
// a test daemon wired to that DB plus the admin list handler.
func setupAdminListTest(t *testing.T, rows []testMemory) (http.Handler, string) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "memories.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	// Create the memories table matching the production schema.
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS memories (
			payload_id TEXT PRIMARY KEY,
			source     TEXT NOT NULL DEFAULT '',
			destination TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
		)
	`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}

	for _, r := range rows {
		_, err = db.Exec(
			`INSERT INTO memories (payload_id, source, destination, created_at) VALUES (?, ?, ?, ?)`,
			r.PayloadID, r.Source, r.Destination, r.CreatedAt,
		)
		if err != nil {
			t.Fatalf("insert row %s: %v", r.PayloadID, err)
		}
	}

	src := buildWriteSource()
	cfg := &config.Config{
		Daemon: config.DaemonConfig{
			Port: 18080,
			Bind: "127.0.0.1",
			RateLimit: config.GlobalRateLimitConfig{
				GlobalRequestsPerMinute: 1000,
			},
			QueueSize: 100,
		},
		Retrieval: config.RetrievalConfig{
			DefaultProfile: "balanced",
		},
		Sources: []*config.Source{src},
		Destinations: []*config.Destination{{
			Name:   "sqlite",
			Type:   "sqlite",
			DBPath: dbPath,
		}},
		ResolvedSourceKeys: map[string][]byte{src.Name: []byte("src-key")},
		ResolvedAdminKey:   []byte("admin-key"),
	}

	d := daemon.NewTestDaemon(t, cfg)
	return d.AdminListHandler(), dbPath
}

type testMemory struct {
	PayloadID   string
	Source      string
	Destination string
	CreatedAt   string
}

func doAdminListRequest(t *testing.T, handler http.Handler, query, token string) (*httptest.ResponseRecorder, adminListResponse) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/admin/memories"+query, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	var resp adminListResponse
	if rr.Code == http.StatusOK {
		if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
	}
	return rr, resp
}

func TestHandleAdminList_EmptyDB(t *testing.T) {
	handler, _ := setupAdminListTest(t, nil)
	rr, resp := doAdminListRequest(t, handler, "", "admin-key")

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rr.Code, rr.Body.String())
	}
	if len(resp.Memories) != 0 {
		t.Errorf("memories = %d, want 0", len(resp.Memories))
	}
	if resp.Admin.ResultCount != 0 {
		t.Errorf("result_count = %d, want 0", resp.Admin.ResultCount)
	}
	if resp.Admin.HasMore {
		t.Error("has_more = true, want false")
	}
}

func TestHandleAdminList_SingleRow(t *testing.T) {
	rows := []testMemory{
		{PayloadID: "aaa", Source: "claude", Destination: "sqlite", CreatedAt: "2026-04-13T00:00:00.000Z"},
	}
	handler, _ := setupAdminListTest(t, rows)
	rr, resp := doAdminListRequest(t, handler, "", "admin-key")

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rr.Code, rr.Body.String())
	}
	if len(resp.Memories) != 1 {
		t.Fatalf("memories = %d, want 1", len(resp.Memories))
	}
	if resp.Memories[0].PayloadID != "aaa" {
		t.Errorf("payload_id = %q, want %q", resp.Memories[0].PayloadID, "aaa")
	}
	if resp.Admin.HasMore {
		t.Error("has_more = true, want false")
	}
	if resp.Admin.NextCursor != "" {
		t.Errorf("next_cursor = %q, want empty", resp.Admin.NextCursor)
	}
}

func TestHandleAdminList_PaginationWithLimit(t *testing.T) {
	rows := []testMemory{
		{PayloadID: "aaa", Source: "s", Destination: "d", CreatedAt: "2026-04-13T00:00:01.000Z"},
		{PayloadID: "bbb", Source: "s", Destination: "d", CreatedAt: "2026-04-13T00:00:02.000Z"},
		{PayloadID: "ccc", Source: "s", Destination: "d", CreatedAt: "2026-04-13T00:00:03.000Z"},
	}
	handler, _ := setupAdminListTest(t, rows)

	// Page 1: limit=2
	rr, resp := doAdminListRequest(t, handler, "?limit=2", "admin-key")
	if rr.Code != http.StatusOK {
		t.Fatalf("page1 status = %d; body = %s", rr.Code, rr.Body.String())
	}
	if len(resp.Memories) != 2 {
		t.Fatalf("page1 memories = %d, want 2", len(resp.Memories))
	}
	if !resp.Admin.HasMore {
		t.Fatal("page1 has_more = false, want true")
	}
	if resp.Admin.NextCursor == "" {
		t.Fatal("page1 next_cursor is empty")
	}

	// Page 2: use cursor
	rr2, resp2 := doAdminListRequest(t, handler, "?limit=2&cursor="+resp.Admin.NextCursor, "admin-key")
	if rr2.Code != http.StatusOK {
		t.Fatalf("page2 status = %d; body = %s", rr2.Code, rr2.Body.String())
	}
	if len(resp2.Memories) != 1 {
		t.Fatalf("page2 memories = %d, want 1", len(resp2.Memories))
	}
	if resp2.Memories[0].PayloadID != "ccc" {
		t.Errorf("page2 payload_id = %q, want %q", resp2.Memories[0].PayloadID, "ccc")
	}
	if resp2.Admin.HasMore {
		t.Error("page2 has_more = true, want false")
	}
}

func TestHandleAdminList_FullCursorWalk(t *testing.T) {
	var rows []testMemory
	for i := 0; i < 25; i++ {
		rows = append(rows, testMemory{
			PayloadID:   fmt.Sprintf("id-%03d", i),
			Source:      "s",
			Destination: "d",
			CreatedAt:   fmt.Sprintf("2026-04-13T00:%02d:00.000Z", i),
		})
	}
	handler, _ := setupAdminListTest(t, rows)

	seen := make(map[string]bool)
	cursor := ""
	totalWalked := 0

	for {
		q := "?limit=7"
		if cursor != "" {
			q += "&cursor=" + cursor
		}
		rr, resp := doAdminListRequest(t, handler, q, "admin-key")
		if rr.Code != http.StatusOK {
			t.Fatalf("walk status = %d; body = %s", rr.Code, rr.Body.String())
		}
		for _, m := range resp.Memories {
			if seen[m.PayloadID] {
				t.Fatalf("duplicate payload_id in cursor walk: %s", m.PayloadID)
			}
			seen[m.PayloadID] = true
			totalWalked++
		}
		if !resp.Admin.HasMore {
			break
		}
		cursor = resp.Admin.NextCursor
	}

	if totalWalked != 25 {
		t.Errorf("total walked = %d, want 25", totalWalked)
	}
	if len(seen) != 25 {
		t.Errorf("unique IDs = %d, want 25", len(seen))
	}
}

func TestHandleAdminList_TiedTimestamps(t *testing.T) {
	// Two rows with identical created_at — cursor must not skip or duplicate.
	ts := "2026-04-13T12:00:00.000Z"
	rows := []testMemory{
		{PayloadID: "aaa", Source: "s", Destination: "d", CreatedAt: ts},
		{PayloadID: "bbb", Source: "s", Destination: "d", CreatedAt: ts},
	}
	handler, _ := setupAdminListTest(t, rows)

	// Page 1: limit=1
	rr, resp := doAdminListRequest(t, handler, "?limit=1", "admin-key")
	if rr.Code != http.StatusOK {
		t.Fatalf("page1 status = %d; body = %s", rr.Code, rr.Body.String())
	}
	if len(resp.Memories) != 1 {
		t.Fatalf("page1 memories = %d, want 1", len(resp.Memories))
	}
	if !resp.Admin.HasMore {
		t.Fatal("page1 has_more = false, want true")
	}
	first := resp.Memories[0].PayloadID

	// Page 2: use cursor
	rr2, resp2 := doAdminListRequest(t, handler, "?limit=1&cursor="+resp.Admin.NextCursor, "admin-key")
	if rr2.Code != http.StatusOK {
		t.Fatalf("page2 status = %d; body = %s", rr2.Code, rr2.Body.String())
	}
	if len(resp2.Memories) != 1 {
		t.Fatalf("page2 memories = %d, want 1", len(resp2.Memories))
	}
	second := resp2.Memories[0].PayloadID

	if first == second {
		t.Errorf("tied timestamps returned same row twice: %s", first)
	}
	if resp2.Admin.HasMore {
		t.Error("page2 has_more = true, want false")
	}
}

func TestHandleAdminList_InvalidCursorNotBase64(t *testing.T) {
	handler, _ := setupAdminListTest(t, nil)
	rr, _ := doAdminListRequest(t, handler, "?cursor=not-valid-base64!!!", "admin-key")
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body = %s", rr.Code, rr.Body.String())
	}
}

func TestHandleAdminList_InvalidCursorBadFormat(t *testing.T) {
	handler, _ := setupAdminListTest(t, nil)
	// Valid base64, but no pipe separator.
	cursor := base64.StdEncoding.EncodeToString([]byte("no-pipe-here"))
	rr, _ := doAdminListRequest(t, handler, "?cursor="+cursor, "admin-key")
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body = %s", rr.Code, rr.Body.String())
	}
}

func TestHandleAdminList_LimitCapped(t *testing.T) {
	// limit > max should be capped, not rejected.
	handler, _ := setupAdminListTest(t, nil)
	rr, _ := doAdminListRequest(t, handler, "?limit=9999", "admin-key")
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (capped); body = %s", rr.Code, rr.Body.String())
	}
}

func TestHandleAdminList_LimitZero(t *testing.T) {
	handler, _ := setupAdminListTest(t, nil)
	rr, _ := doAdminListRequest(t, handler, "?limit=0", "admin-key")
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body = %s", rr.Code, rr.Body.String())
	}
}

func TestHandleAdminList_LimitNegative(t *testing.T) {
	handler, _ := setupAdminListTest(t, nil)
	rr, _ := doAdminListRequest(t, handler, "?limit=-1", "admin-key")
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body = %s", rr.Code, rr.Body.String())
	}
}

func TestHandleAdminList_NoToken(t *testing.T) {
	handler, _ := setupAdminListTest(t, nil)
	rr, _ := doAdminListRequest(t, handler, "", "")
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401; body = %s", rr.Code, rr.Body.String())
	}
}

func TestHandleAdminList_DataTokenRejected(t *testing.T) {
	handler, _ := setupAdminListTest(t, nil)
	rr, _ := doAdminListRequest(t, handler, "", "src-key")
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401; body = %s", rr.Code, rr.Body.String())
	}
}
