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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bubblefish-tech/nexus/internal/config"
	"github.com/bubblefish-tech/nexus/internal/safego"
	"github.com/go-chi/chi/v5"

	"log/slog"
)

func newHealthTestDaemon() *Daemon {
	cfg := &config.Config{}
	logger := slog.Default()
	d := &Daemon{
		cfg:             cfg,
		logger:          logger,
		subsystemHealth: safego.NewStatusTracker(),
		stopped:         make(chan struct{}),
		shutdownReq:     make(chan struct{}),
		writeNotify:     make(chan struct{}, 1),
	}
	d.walHealthy.Store(1)
	return d
}

func TestHealthReasons_ContainsGoroutineAndHeapSaturation(t *testing.T) {
	t.Helper()
	d := newHealthTestDaemon()

	r := chi.NewRouter()
	r.Get("/health", d.handleHealth)
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var hr healthResponse
	json.NewDecoder(resp.Body).Decode(&hr)

	// Goroutines and heap saturation metrics should always be present.
	if _, ok := hr.Subsystems["goroutines"]; !ok {
		t.Error("missing goroutines subsystem")
	}
	if _, ok := hr.Subsystems["heap"]; !ok {
		t.Error("missing heap subsystem")
	}
}

func TestHealthReasons_IncludesDegradedSubsystem(t *testing.T) {
	t.Helper()
	d := newHealthTestDaemon()
	d.walHealthy.Store(0) // WAL unhealthy

	r := chi.NewRouter()
	r.Get("/health", d.handleHealth)
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var hr healthResponse
	json.NewDecoder(resp.Body).Decode(&hr)
	if hr.Status != "degraded" {
		t.Errorf("status = %q, want %q", hr.Status, "degraded")
	}
	if len(hr.Reasons) == 0 {
		t.Error("expected non-empty reasons when WAL degraded")
	}
	found := false
	for _, r := range hr.Reasons {
		if r == "wal:degraded" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'wal:degraded' in reasons, got %v", hr.Reasons)
	}
}

func TestHealthReasons_MultipleDegraded(t *testing.T) {
	t.Helper()
	d := newHealthTestDaemon()
	d.walHealthy.Store(0)
	d.subsystemHealth.MarkDegraded("test-goroutine", "test panic")

	r := chi.NewRouter()
	r.Get("/health", d.handleHealth)
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var hr healthResponse
	json.NewDecoder(resp.Body).Decode(&hr)
	if len(hr.Reasons) < 2 {
		t.Errorf("expected at least 2 reasons, got %d: %v", len(hr.Reasons), hr.Reasons)
	}
}
