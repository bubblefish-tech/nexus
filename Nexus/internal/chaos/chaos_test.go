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
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunReportGeneration(t *testing.T) {
	var writeCount atomic.Int64

	mux := http.NewServeMux()
	mux.HandleFunc("/inbound/", func(w http.ResponseWriter, r *http.Request) {
		n := writeCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"payload_id":"mock-%d","status":"accepted"}`, n)
	})
	mux.HandleFunc("/query/", func(w http.ResponseWriter, r *http.Request) {
		// Return all mock IDs as recovered.
		count := int(writeCount.Load())
		var records []map[string]string
		for i := 1; i <= count; i++ {
			records = append(records, map[string]string{"payload_id": fmt.Sprintf("mock-%d", i)})
		}
		resp := map[string]interface{}{
			"records": records,
			"_nexus":  map[string]interface{}{"has_more": false, "next_cursor": ""},
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
		Destination:   "sqlite",
		APIKey:        "test-key",
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
