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

package commands

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bubblefish-tech/nexus/internal/tui/api"
)

// fakeTestServer responds to health, ready, status, config, audit, lint, security.
func fakeTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/ready", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
	})
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":            "ok",
			"version":           "0.1.3",
			"queue_depth":       0,
			"consistency_score": 1.0,
		})
	})
	mux.HandleFunc("/api/config", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{})
	})
	mux.HandleFunc("/api/audit/log", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"records": []interface{}{}})
	})
	mux.HandleFunc("/api/lint", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"findings": []interface{}{}})
	})
	mux.HandleFunc("/api/security/summary", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{})
	})
	return httptest.NewServer(mux)
}

func TestCategories_NotEmpty(t *testing.T) {
	t.Helper()
	cats := Categories()
	if len(cats) == 0 {
		t.Fatal("expected at least one category")
	}
}

func TestCategories_ContainsQuickHealth(t *testing.T) {
	t.Helper()
	for _, c := range Categories() {
		if c == "Quick Health" {
			return
		}
	}
	t.Fatal("expected 'Quick Health' category")
}

func TestCategories_ContainsFullSuite(t *testing.T) {
	t.Helper()
	for _, c := range Categories() {
		if c == "Full Suite" {
			return
		}
	}
	t.Fatal("expected 'Full Suite' category")
}

func TestTestCommand_Name(t *testing.T) {
	t.Helper()
	cmd := TestCommand{}
	if cmd.Name() != "test" {
		t.Fatal("expected name 'test'")
	}
}

func TestRunCategory_QuickHealth_AllPass(t *testing.T) {
	t.Helper()
	srv := fakeTestServer(t)
	defer srv.Close()
	client := api.NewClient(srv.URL, "token")
	cmd := RunCategory(client, "Quick Health")
	msg := cmd()
	result, ok := msg.(TestResultMsg)
	if !ok {
		t.Fatalf("expected TestResultMsg, got %T", msg)
	}
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if result.Failed != 0 {
		t.Fatalf("expected 0 failures, got %d", result.Failed)
	}
	if result.Passed == 0 {
		t.Fatal("expected at least 1 passed")
	}
}

func TestRunCategory_UnknownCategory(t *testing.T) {
	t.Helper()
	srv := fakeTestServer(t)
	defer srv.Close()
	client := api.NewClient(srv.URL, "token")
	cmd := RunCategory(client, "Nonexistent")
	msg := cmd()
	result, ok := msg.(TestResultMsg)
	if !ok {
		t.Fatalf("expected TestResultMsg, got %T", msg)
	}
	if result.Err == nil {
		t.Fatal("expected error for unknown category")
	}
}

func TestRunCategory_Core_AllPass(t *testing.T) {
	t.Helper()
	srv := fakeTestServer(t)
	defer srv.Close()
	client := api.NewClient(srv.URL, "token")
	cmd := RunCategory(client, "Core")
	msg := cmd()
	result := msg.(TestResultMsg)
	if result.Failed != 0 {
		t.Fatalf("expected 0 failures in Core, got %d", result.Failed)
	}
}

func TestRunCategory_FullSuite_HasTests(t *testing.T) {
	t.Helper()
	srv := fakeTestServer(t)
	defer srv.Close()
	client := api.NewClient(srv.URL, "token")
	cmd := RunCategory(client, "Full Suite")
	msg := cmd()
	result := msg.(TestResultMsg)
	if len(result.Results) == 0 {
		t.Fatal("expected Full Suite to have test results")
	}
}

func TestRunCategory_ResultFields(t *testing.T) {
	t.Helper()
	srv := fakeTestServer(t)
	defer srv.Close()
	client := api.NewClient(srv.URL, "token")
	cmd := RunCategory(client, "Quick Health")
	msg := cmd()
	result := msg.(TestResultMsg)
	for _, r := range result.Results {
		if r.Name == "" {
			t.Error("test case result should have a name")
		}
		if r.Desc == "" {
			t.Error("test case result should have a description")
		}
	}
}
