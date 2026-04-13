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

package chaos

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// createTestDB creates a temp SQLite with the memories table and returns its path.
func createTestDB(t *testing.T, ids []string) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "memories.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`CREATE TABLE memories (
		payload_id TEXT PRIMARY KEY,
		source TEXT NOT NULL DEFAULT '',
		destination TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
	)`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}
	for _, id := range ids {
		_, err = db.Exec(`INSERT INTO memories (payload_id) VALUES (?)`, id)
		if err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	return dbPath
}

func TestRunReportGeneration(t *testing.T) {
	var writeCount atomic.Int64

	// The mock server assigns payload_id = "mock-N" for each write.
	// We pre-populate a DB with mock-1 through mock-5000 so that the DB
	// is a superset of any accepted writes. The mock /admin/memories
	// reads directly from that same DB, ensuring DB == HTTP.
	var preloadIDs []string
	for i := 1; i <= 5000; i++ {
		preloadIDs = append(preloadIDs, fmt.Sprintf("mock-%d", i))
	}
	dbPath := createTestDB(t, preloadIDs)

	mux := http.NewServeMux()
	mux.HandleFunc("/inbound/", func(w http.ResponseWriter, r *http.Request) {
		n := writeCount.Add(1)
		id := fmt.Sprintf("mock-%d", n)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"payload_id":"%s","status":"accepted"}`, id)
	})
	mux.HandleFunc("/admin/memories", func(w http.ResponseWriter, r *http.Request) {
		// Read directly from the test DB to ensure DB == HTTP.
		db, err := sql.Open("sqlite", dbPath+"?mode=ro")
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer db.Close()
		rows, err := db.Query("SELECT payload_id FROM memories ORDER BY payload_id")
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer rows.Close()
		var memories []map[string]string
		for rows.Next() {
			var id string
			rows.Scan(&id)
			memories = append(memories, map[string]string{
				"payload_id":  id,
				"created_at":  "2026-04-13T00:00:00.000Z",
				"source":      "test",
				"destination": "sqlite",
			})
		}
		resp := map[string]interface{}{
			"memories": memories,
			"_admin":   map[string]interface{}{"has_more": false, "next_cursor": "", "result_count": len(memories)},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	report, err := Run(Options{
		URL:           srv.URL,
		Source:        "test",
		DBPath:        dbPath,
		APIKey:        "test-key",
		AdminKey:      "admin-key",
		Duration:      2 * time.Second,
		Concurrency:   2,
		FaultInterval: 1 * time.Second,
		Seed:          42,
	})
	if err != nil {
		t.Fatal(err)
	}

	if report.Seed != 42 {
		t.Errorf("seed = %d, want 42", report.Seed)
	}
	if report.WritesAccepted == 0 {
		t.Error("expected writes > 0")
	}
	if !report.Pass {
		t.Errorf("expected pass against mock server: %s", report.Verdict)
	}
	if report.Duration <= 0 {
		t.Error("expected positive duration")
	}
	if report.AcceptedNotInDB != 0 {
		t.Errorf("accepted_not_in_db = %d, want 0", report.AcceptedNotInDB)
	}
	if report.DuplicateCount != 0 {
		t.Errorf("duplicate_count = %d, want 0", report.DuplicateCount)
	}
}

func TestRunMissingURL(t *testing.T) {
	_, err := Run(Options{})
	if err == nil {
		t.Error("expected error for empty URL")
	}
}

func TestRunMissingAPIKey(t *testing.T) {
	_, err := Run(Options{URL: "http://localhost:9999"})
	if err == nil {
		t.Error("expected error for empty API key")
	}
}

func TestRunMissingDBPath(t *testing.T) {
	_, err := Run(Options{URL: "http://localhost:9999", APIKey: "key"})
	if err == nil {
		t.Error("expected error for empty DBPath")
	}
}

func TestRunMissingAdminKey(t *testing.T) {
	_, err := Run(Options{URL: "http://localhost:9999", APIKey: "key", DBPath: "/tmp/test.db"})
	if err == nil {
		t.Error("expected error for empty AdminKey")
	}
}

func TestVerifyAgainstDB(t *testing.T) {
	ids := []string{"aaa", "bbb", "ccc"}
	dbPath := createTestDB(t, ids)

	set, err := verifyAgainstDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(set) != 3 {
		t.Fatalf("set size = %d, want 3", len(set))
	}
	for _, id := range ids {
		if !set[id] {
			t.Errorf("missing expected ID: %s", id)
		}
	}
}

func TestVerifyAgainstDB_EmptyDB(t *testing.T) {
	dbPath := createTestDB(t, nil)
	set, err := verifyAgainstDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(set) != 0 {
		t.Errorf("set size = %d, want 0", len(set))
	}
}

func TestVerifyAgainstAdminList(t *testing.T) {
	// Mock server returns 3 pages of 2 items each.
	page := 0
	pages := []struct {
		ids     []string
		hasMore bool
		cursor  string
	}{
		{ids: []string{"a1", "a2"}, hasMore: true, cursor: base64.StdEncoding.EncodeToString([]byte("ts|a2"))},
		{ids: []string{"b1", "b2"}, hasMore: true, cursor: base64.StdEncoding.EncodeToString([]byte("ts|b2"))},
		{ids: []string{"c1"}, hasMore: false, cursor: ""},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := pages[page]
		page++
		var memories []map[string]string
		for _, id := range p.ids {
			memories = append(memories, map[string]string{"payload_id": id})
		}
		resp := map[string]interface{}{
			"memories": memories,
			"_admin": map[string]interface{}{
				"has_more":     p.hasMore,
				"next_cursor":  p.cursor,
				"result_count": len(memories),
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	set, dupes, err := verifyAgainstAdminList(http.DefaultClient, srv.URL, "admin-key")
	if err != nil {
		t.Fatal(err)
	}
	if len(set) != 5 {
		t.Errorf("set size = %d, want 5", len(set))
	}
	if dupes != 0 {
		t.Errorf("duplicates = %d, want 0", dupes)
	}
	for _, id := range []string{"a1", "a2", "b1", "b2", "c1"} {
		if !set[id] {
			t.Errorf("missing expected ID: %s", id)
		}
	}
}

func TestVerifyAgainstAdminList_DetectsDuplicates(t *testing.T) {
	// Server returns same ID twice across pages.
	page := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ids []string
		hasMore := false
		cursor := ""
		if page == 0 {
			ids = []string{"dup1", "unique1"}
			hasMore = true
			cursor = base64.StdEncoding.EncodeToString([]byte("ts|unique1"))
		} else {
			ids = []string{"dup1", "unique2"} // dup1 is a duplicate
			hasMore = false
		}
		page++
		var memories []map[string]string
		for _, id := range ids {
			memories = append(memories, map[string]string{"payload_id": id})
		}
		resp := map[string]interface{}{
			"memories": memories,
			"_admin":   map[string]interface{}{"has_more": hasMore, "next_cursor": cursor, "result_count": len(memories)},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	set, dupes, err := verifyAgainstAdminList(http.DefaultClient, srv.URL, "admin-key")
	if err != nil {
		t.Fatal(err)
	}
	if dupes != 1 {
		t.Errorf("duplicates = %d, want 1", dupes)
	}
	if len(set) != 3 {
		t.Errorf("set size = %d, want 3 (dup1, unique1, unique2)", len(set))
	}
}

func TestSetDifferenceDiagnostics(t *testing.T) {
	tests := []struct {
		name        string
		accepted    map[string]bool
		db          map[string]bool
		httpSet     map[string]bool
		httpDupes   int
		wantPass    bool
		wantVerdict string // substring to match in verdict
	}{
		{
			name:        "all agree",
			accepted:    map[string]bool{"a": true, "b": true},
			db:          map[string]bool{"a": true, "b": true, "old": true},
			httpSet:     map[string]bool{"a": true, "b": true, "old": true},
			wantPass:    true,
			wantVerdict: "PASS",
		},
		{
			name:        "durability bug",
			accepted:    map[string]bool{"a": true, "b": true, "lost": true},
			db:          map[string]bool{"a": true, "b": true},
			httpSet:     map[string]bool{"a": true, "b": true},
			wantPass:    false,
			wantVerdict: "DURABILITY BUG",
		},
		{
			name:        "read-path bug (accepted not in HTTP)",
			accepted:    map[string]bool{"a": true},
			db:          map[string]bool{"a": true},
			httpSet:     map[string]bool{},
			wantPass:    false,
			wantVerdict: "READ-PATH BUG",
		},
		{
			name:        "phantom data",
			accepted:    map[string]bool{"a": true},
			db:          map[string]bool{"a": true},
			httpSet:     map[string]bool{"a": true, "ghost": true},
			wantPass:    false,
			wantVerdict: "PHANTOM DATA",
		},
		{
			name:        "cursor instability",
			accepted:    map[string]bool{"a": true},
			db:          map[string]bool{"a": true},
			httpSet:     map[string]bool{"a": true},
			httpDupes:   3,
			wantPass:    false,
			wantVerdict: "CURSOR INSTABILITY",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := &Report{
				WritesAccepted: int64(len(tt.accepted)),
			}

			// Compute set differences (same logic as Run).
			for id := range tt.accepted {
				if !tt.db[id] {
					report.AcceptedNotInDB++
				}
			}
			for id := range tt.accepted {
				if !tt.httpSet[id] {
					report.AcceptedNotInHTTP++
				}
			}
			for id := range tt.db {
				if !tt.httpSet[id] {
					report.DBNotInHTTP++
				}
			}
			for id := range tt.httpSet {
				if !tt.db[id] {
					report.HTTPNotInDB++
				}
			}
			report.DuplicateCount = tt.httpDupes
			report.DBRecoveredCount = len(tt.db)
			report.HTTPRecoveredCount = len(tt.httpSet)

			pass := report.AcceptedNotInDB == 0 &&
				report.AcceptedNotInHTTP == 0 &&
				report.DBNotInHTTP == 0 &&
				report.HTTPNotInDB == 0 &&
				report.DuplicateCount == 0
			report.Pass = pass

			if pass {
				report.Verdict = fmt.Sprintf("PASS — %d writes accepted, %d in DB, %d via admin API, all sets agree, 0 faults injected",
					report.WritesAccepted, report.DBRecoveredCount, report.HTTPRecoveredCount)
			} else {
				var parts []string
				if report.AcceptedNotInDB > 0 {
					parts = append(parts, fmt.Sprintf("DURABILITY BUG: %d accepted writes missing from DB", report.AcceptedNotInDB))
				}
				if report.AcceptedNotInHTTP > 0 {
					parts = append(parts, fmt.Sprintf("READ-PATH BUG: %d accepted writes missing from admin API", report.AcceptedNotInHTTP))
				}
				if report.DBNotInHTTP > 0 {
					parts = append(parts, fmt.Sprintf("READ-PATH BUG: %d DB rows not returned by admin API", report.DBNotInHTTP))
				}
				if report.HTTPNotInDB > 0 {
					parts = append(parts, fmt.Sprintf("PHANTOM DATA: %d admin API rows not in DB", report.HTTPNotInDB))
				}
				if report.DuplicateCount > 0 {
					parts = append(parts, fmt.Sprintf("CURSOR INSTABILITY: %d duplicates in admin API pagination", report.DuplicateCount))
				}
				report.Verdict = "FAIL — " + strings.Join(parts, "; ")
			}

			if report.Pass != tt.wantPass {
				t.Errorf("pass = %v, want %v; verdict: %s", report.Pass, tt.wantPass, report.Verdict)
			}
			if !strings.Contains(report.Verdict, tt.wantVerdict) {
				t.Errorf("verdict %q does not contain %q", report.Verdict, tt.wantVerdict)
			}
		})
	}
}
