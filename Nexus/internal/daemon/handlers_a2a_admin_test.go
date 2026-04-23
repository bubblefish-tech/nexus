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
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/bubblefish-tech/nexus/internal/a2a/registry"
	"github.com/bubblefish-tech/nexus/internal/config"
	"github.com/bubblefish-tech/nexus/internal/metrics"
	"github.com/go-chi/chi/v5"
)

const testA2AAdminToken = "test-a2a-admin-token"

func newA2AAdminTestDaemon(t *testing.T) *Daemon {
	t.Helper()
	dir := t.TempDir()
	regStore, err := registry.NewStore(filepath.Join(dir, "registry.db"))
	if err != nil {
		t.Fatalf("registry.NewStore: %v", err)
	}
	t.Cleanup(func() { _ = regStore.Close() })
	return &Daemon{
		logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		registryStore: regStore,
		cfg:           &config.Config{ResolvedAdminKey: []byte(testA2AAdminToken)},
		metrics:       metrics.New(),
	}
}

func a2aAdminRouter(d *Daemon) http.Handler {
	r := chi.NewRouter()
	r.Use(d.requireAdminToken)
	r.Post("/a2a/admin/register-agent", d.handleA2AAdminRegisterAgent)
	return r
}

func doRegister(t *testing.T, h http.Handler, body interface{}, token string) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/a2a/admin/register-agent", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestA2AAdminRegister_Success(t *testing.T) {
	t.Helper()
	d := newA2AAdminTestDaemon(t)
	h := a2aAdminRouter(d)

	w := doRegister(t, h, registerAgentRequest{
		Name:    "test-agent",
		URL:     "http://127.0.0.1:9999",
		Methods: []string{"tasks/send"},
	}, testA2AAdminToken)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
	var resp registerAgentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.AgentID == "" {
		t.Error("AgentID should be non-empty")
	}
	if resp.Name != "test-agent" {
		t.Errorf("Name = %q, want test-agent", resp.Name)
	}
	if resp.Upserted {
		t.Error("Upserted should be false for new registration")
	}
	if resp.Status != registry.StatusActive {
		t.Errorf("Status = %q, want active", resp.Status)
	}
}

func TestA2AAdminRegister_Upsert(t *testing.T) {
	t.Helper()
	d := newA2AAdminTestDaemon(t)
	h := a2aAdminRouter(d)

	// First registration.
	w1 := doRegister(t, h, registerAgentRequest{
		Name: "upsert-agent",
		URL:  "http://127.0.0.1:9000",
	}, testA2AAdminToken)
	if w1.Code != http.StatusCreated {
		t.Fatalf("first register status = %d; body: %s", w1.Code, w1.Body.String())
	}
	var r1 registerAgentResponse
	_ = json.Unmarshal(w1.Body.Bytes(), &r1)

	// Second registration — same name, new URL.
	w2 := doRegister(t, h, registerAgentRequest{
		Name: "upsert-agent",
		URL:  "http://127.0.0.1:9001",
	}, testA2AAdminToken)
	if w2.Code != http.StatusOK {
		t.Fatalf("upsert status = %d; body: %s", w2.Code, w2.Body.String())
	}
	var r2 registerAgentResponse
	_ = json.Unmarshal(w2.Body.Bytes(), &r2)

	if r2.AgentID != r1.AgentID {
		t.Errorf("upsert AgentID = %q, want %q", r2.AgentID, r1.AgentID)
	}
	if !r2.Upserted {
		t.Error("Upserted should be true on second registration")
	}
}

func TestA2AAdminRegister_NoToken(t *testing.T) {
	t.Helper()
	d := newA2AAdminTestDaemon(t)
	h := a2aAdminRouter(d)

	w := doRegister(t, h, registerAgentRequest{Name: "x", URL: "http://localhost"}, "")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestA2AAdminRegister_BadToken(t *testing.T) {
	t.Helper()
	d := newA2AAdminTestDaemon(t)
	h := a2aAdminRouter(d)

	w := doRegister(t, h, registerAgentRequest{Name: "x", URL: "http://localhost"}, "wrong-token")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestA2AAdminRegister_MissingName(t *testing.T) {
	t.Helper()
	d := newA2AAdminTestDaemon(t)
	h := a2aAdminRouter(d)

	w := doRegister(t, h, registerAgentRequest{URL: "http://localhost"}, testA2AAdminToken)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestA2AAdminRegister_MissingURL(t *testing.T) {
	t.Helper()
	d := newA2AAdminTestDaemon(t)
	h := a2aAdminRouter(d)

	w := doRegister(t, h, registerAgentRequest{Name: "x"}, testA2AAdminToken)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestA2AAdminRegister_IDPrefix(t *testing.T) {
	t.Helper()
	d := newA2AAdminTestDaemon(t)
	h := a2aAdminRouter(d)

	w := doRegister(t, h, registerAgentRequest{
		Name: "prefix-agent",
		URL:  "http://127.0.0.1:9999",
	}, testA2AAdminToken)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d; body: %s", w.Code, w.Body.String())
	}
	var resp registerAgentResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.AgentID) < 4 || resp.AgentID[:4] != "agt_" {
		t.Errorf("AgentID %q does not start with agt_", resp.AgentID)
	}
}

func TestA2AAdminRegister_NoRegistry(t *testing.T) {
	t.Helper()
	d := &Daemon{
		logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		cfg:     &config.Config{ResolvedAdminKey: []byte(testA2AAdminToken)},
		metrics: metrics.New(),
		// registryStore intentionally nil
	}
	h := a2aAdminRouter(d)

	w := doRegister(t, h, registerAgentRequest{Name: "x", URL: "http://localhost"}, testA2AAdminToken)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}
