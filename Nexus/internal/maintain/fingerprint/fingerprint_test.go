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

package fingerprint_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bubblefish-tech/nexus/internal/maintain/fingerprint"
)

// mockServer returns an httptest.Server that serves the given path→body map.
// Requests to unregistered paths return 404.
func mockServer(t *testing.T, routes map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(body)) //nolint:errcheck
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestFingerprint_OllamaNative verifies /api/tags with {"models":[]} → OllamaNative.
func TestFingerprint_OllamaNative(t *testing.T) {
	srv := mockServer(t, map[string]string{
		"/api/tags": `{"models":[{"name":"llama3","size":4}]}`,
	})
	fp := fingerprint.NewProber().Fingerprint(context.Background(), srv.URL)
	if fp.Protocol != fingerprint.ProtocolOllamaNative {
		t.Errorf("expected OllamaNative, got %s", fp.Protocol)
	}
	if !fp.Confirmed {
		t.Error("Confirmed must be true when probe matches")
	}
}

// TestFingerprint_OpenAICompat verifies /v1/models with {"data":[]} → OpenAICompat.
func TestFingerprint_OpenAICompat(t *testing.T) {
	srv := mockServer(t, map[string]string{
		"/v1/models": `{"object":"list","data":[{"id":"gpt-4"}]}`,
	})
	fp := fingerprint.NewProber().Fingerprint(context.Background(), srv.URL)
	if fp.Protocol != fingerprint.ProtocolOpenAICompat {
		t.Errorf("expected OpenAICompat, got %s", fp.Protocol)
	}
	if !fp.Confirmed {
		t.Error("Confirmed must be true when probe matches")
	}
}

// TestFingerprint_TGI verifies /info with {"model_id":"...","max_total_tokens":N} → TGI.
func TestFingerprint_TGI(t *testing.T) {
	srv := mockServer(t, map[string]string{
		"/info": `{"model_id":"mistralai/Mistral-7B","max_total_tokens":4096,"sha":"abc123"}`,
	})
	fp := fingerprint.NewProber().Fingerprint(context.Background(), srv.URL)
	if fp.Protocol != fingerprint.ProtocolTGI {
		t.Errorf("expected TGI, got %s", fp.Protocol)
	}
	if !fp.Confirmed {
		t.Error("Confirmed must be true")
	}
}

// TestFingerprint_KoboldCpp verifies /api/v1/info with {"result":"KoboldCpp"} → KoboldCpp.
func TestFingerprint_KoboldCpp(t *testing.T) {
	srv := mockServer(t, map[string]string{
		"/api/v1/info": `{"result":"KoboldCpp","version":"1.60","model":"llama.cpp"}`,
	})
	fp := fingerprint.NewProber().Fingerprint(context.Background(), srv.URL)
	if fp.Protocol != fingerprint.ProtocolKoboldCpp {
		t.Errorf("expected KoboldCpp, got %s", fp.Protocol)
	}
	if !fp.Confirmed {
		t.Error("Confirmed must be true")
	}
}

// TestFingerprint_Tabby verifies /v1/health with {"device":"..."} → Tabby.
func TestFingerprint_Tabby(t *testing.T) {
	srv := mockServer(t, map[string]string{
		"/v1/health": `{"device":"cuda","arch":"llama","cpu_info":"Intel","cuda_devices":[]}`,
	})
	fp := fingerprint.NewProber().Fingerprint(context.Background(), srv.URL)
	if fp.Protocol != fingerprint.ProtocolTabby {
		t.Errorf("expected Tabby, got %s", fp.Protocol)
	}
	if !fp.Confirmed {
		t.Error("Confirmed must be true")
	}
}

// TestFingerprint_Unknown verifies that a server responding 404 to all probes
// is identified as ProtocolUnknown.
func TestFingerprint_Unknown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	fp := fingerprint.NewProber().Fingerprint(context.Background(), srv.URL)
	if fp.Protocol != fingerprint.ProtocolUnknown {
		t.Errorf("expected Unknown, got %s", fp.Protocol)
	}
	if fp.Confirmed {
		t.Error("Confirmed must be false when no probe matches")
	}
}

// TestFingerprint_Evidence_Recorded verifies that Evidence slices contain an
// entry for each probe attempted.
func TestFingerprint_Evidence_Recorded(t *testing.T) {
	srv := mockServer(t, map[string]string{
		"/api/tags": `{"models":[]}`,
	})
	fp := fingerprint.NewProber().Fingerprint(context.Background(), srv.URL)
	if len(fp.Evidence) == 0 {
		t.Fatal("Evidence must not be empty")
	}
	// The first probe (ollama-tags) should have matched.
	ev := fp.Evidence[0]
	if ev.ProbeName != "ollama-tags" {
		t.Errorf("expected first evidence to be ollama-tags, got %q", ev.ProbeName)
	}
	if !ev.Matched {
		t.Error("first Evidence should be Matched=true for Ollama response")
	}
	if ev.StatusCode != 200 {
		t.Errorf("expected StatusCode 200, got %d", ev.StatusCode)
	}
}

// TestFingerprint_Ollama_NotMisidentifiedAsOpenAI verifies that Ollama (which
// can also serve /v1/models) is identified by the earlier Ollama-specific probe.
func TestFingerprint_Ollama_NotMisidentifiedAsOpenAI(t *testing.T) {
	// Server responds to BOTH /api/tags (Ollama) AND /v1/models (OpenAI compat).
	srv := mockServer(t, map[string]string{
		"/api/tags":  `{"models":[{"name":"llama3"}]}`,
		"/v1/models": `{"object":"list","data":[{"id":"llama3"}]}`,
	})
	fp := fingerprint.NewProber().Fingerprint(context.Background(), srv.URL)
	if fp.Protocol != fingerprint.ProtocolOllamaNative {
		t.Errorf("Ollama should be identified as OllamaNative even with /v1/models, got %s", fp.Protocol)
	}
}

// TestFingerprint_CustomProbes verifies NewProberWithProbes uses the supplied probes.
func TestFingerprint_CustomProbes(t *testing.T) {
	srv := mockServer(t, map[string]string{
		"/custom/probe": `{"custom_field":"yes"}`,
	})
	probes := []fingerprint.Probe{
		{
			Name:  "custom-probe",
			Path:  "/custom/probe",
			Proto: fingerprint.ProtocolUnknown, // reuse any protocol constant
			Match: func(status int, body []byte) bool {
				return status == 200
			},
		},
	}
	fp := fingerprint.NewProberWithProbes(probes).Fingerprint(context.Background(), srv.URL)
	if !fp.Confirmed {
		t.Error("custom probe should have matched and set Confirmed=true")
	}
	if len(fp.Evidence) != 1 {
		t.Errorf("expected 1 evidence entry, got %d", len(fp.Evidence))
	}
}

// TestFingerprint_ContextCancelled verifies a cancelled context causes the
// prober to stop early and return ProtocolUnknown.
func TestFingerprint_ContextCancelled(t *testing.T) {
	// Server sleeps slightly longer than the context timeout.
	// The sleep is short (300ms) so srv.Close() doesn't stall the test.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	fp := fingerprint.NewProber().Fingerprint(ctx, srv.URL)
	if fp.Protocol != fingerprint.ProtocolUnknown {
		t.Errorf("expected Unknown after timeout, got %s", fp.Protocol)
	}
}

// TestFingerprint_String_NonEmpty verifies Fingerprint.String() returns a useful value.
func TestFingerprint_String_NonEmpty(t *testing.T) {
	srv := mockServer(t, map[string]string{
		"/api/tags": `{"models":[]}`,
	})
	fp := fingerprint.NewProber().Fingerprint(context.Background(), srv.URL)
	s := fp.String()
	if s == "" {
		t.Error("String() must not be empty")
	}
}

// TestFingerprint_OpenAICompat_FallbackProbe verifies the /v1/completions 400
// fallback probe identifies an OpenAI-compat server that serves no /v1/models.
func TestFingerprint_OpenAICompat_FallbackProbe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/completions":
			// Return 400 with an error field — as an OpenAI server does for missing body
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":{"message":"missing required field model","type":"invalid_request_error"}}`)) //nolint:errcheck
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	fp := fingerprint.NewProber().Fingerprint(context.Background(), srv.URL)
	if fp.Protocol != fingerprint.ProtocolOpenAICompat {
		t.Errorf("expected OpenAICompat via fallback probe, got %s", fp.Protocol)
	}
	if !fp.Confirmed {
		t.Error("Confirmed must be true")
	}
}
