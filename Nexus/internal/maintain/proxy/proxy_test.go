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

package proxy_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bubblefish-tech/nexus/internal/maintain/proxy"
)

// --- AllowList tests ---

func TestAllowList_LoopbackIPAllowed(t *testing.T) {
	al := proxy.NewAllowList([]string{"http://127.0.0.1:11434"})
	if !al.IsAllowed("http://127.0.0.1:11434/api/generate") {
		t.Error("loopback URL should be allowed")
	}
}

func TestAllowList_LocalhostAllowed(t *testing.T) {
	al := proxy.NewAllowList([]string{"http://localhost:8080"})
	if !al.IsAllowed("http://localhost:8080/v1/completions") {
		t.Error("localhost should be allowed")
	}
}

func TestAllowList_ExternalIPBlocked(t *testing.T) {
	al := proxy.NewAllowList([]string{"http://1.2.3.4:11434"})
	// The Add should have silently dropped the non-loopback URL.
	if al.IsAllowed("http://1.2.3.4:11434/api/generate") {
		t.Error("non-loopback IP must be blocked by the allowlist")
	}
}

func TestAllowList_UnregisteredLoopbackBlocked(t *testing.T) {
	al := proxy.NewAllowList([]string{"http://127.0.0.1:11434"})
	if al.IsAllowed("http://127.0.0.1:1234/v1/models") {
		t.Error("loopback URL not in allowlist should be blocked")
	}
}

func TestAllowList_Snapshot(t *testing.T) {
	al := proxy.NewAllowList([]string{"http://127.0.0.1:11434", "http://127.0.0.1:1234"})
	snap := al.Snapshot()
	if len(snap) != 2 {
		t.Errorf("expected 2 entries in snapshot, got %d", len(snap))
	}
}

// --- Proxy routing tests ---

// upstreamRecorder returns an httptest.Server that records the last request it received.
func upstreamRecorder(t *testing.T) (*httptest.Server, *http.Request) {
	t.Helper()
	var captured *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Clone(r.Context())
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"ok":true}`)
	}))
	t.Cleanup(srv.Close)
	_ = &captured // reference to avoid unused-var lint; caller uses the address
	return srv, captured
}

// newTestProxy creates a Proxy bound to an ephemeral port, registers a route
// for toolName → upstreamURL, and returns the proxy's URL.
func newTestProxy(t *testing.T, toolName, upstreamURL string) (proxyBaseURL string, p *proxy.Proxy) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	p = proxy.NewProxy(proxy.Config{ListenAddr: ln.Addr().String()})
	if err := p.AddRoute(toolName, upstreamURL); err != nil {
		t.Fatalf("AddRoute: %v", err)
	}
	srv := &http.Server{Handler: p}
	go srv.Serve(ln) //nolint:errcheck
	t.Cleanup(func() { srv.Close() })
	return "http://" + ln.Addr().String(), p
}

func TestProxy_UnknownTool_404(t *testing.T) {
	proxyURL, _ := newTestProxy(t, "ollama", "http://127.0.0.1:11434")
	resp, err := http.Get(proxyURL + "/proxy/unknown-tool/api/generate")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestProxy_BadPath_400(t *testing.T) {
	proxyURL, _ := newTestProxy(t, "ollama", "http://127.0.0.1:11434")
	resp, err := http.Get(proxyURL + "/not-proxy-prefix/anything")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestProxy_ForwardsToUpstream(t *testing.T) {
	var receivedPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		fmt.Fprintln(w, `{"model":"llama3"}`)
	}))
	defer upstream.Close()

	proxyURL, _ := newTestProxy(t, "myollama", upstream.URL)
	resp, err := http.Get(proxyURL + "/proxy/myollama/api/tags")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if receivedPath != "/api/tags" {
		t.Errorf("expected upstream to see /api/tags, got %q", receivedPath)
	}
}

func TestProxy_InjectsNexusHeaders(t *testing.T) {
	var gotProxy, gotVersion string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotProxy = r.Header.Get("X-Nexus-Proxy")
		gotVersion = r.Header.Get("X-Nexus-Version")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	proxyURL, _ := newTestProxy(t, "hdr-tool", upstream.URL)
	http.Get(proxyURL + "/proxy/hdr-tool/v1/models") //nolint:errcheck

	if gotProxy != "1" {
		t.Errorf("X-Nexus-Proxy not set, got %q", gotProxy)
	}
	if gotVersion == "" {
		t.Error("X-Nexus-Version not set")
	}
}

func TestProxy_ResponseBody_PassedThrough(t *testing.T) {
	want := `{"models":["llama3","mistral"]}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, want)
	}))
	defer upstream.Close()

	proxyURL, _ := newTestProxy(t, "body-tool", upstream.URL)
	resp, err := http.Get(proxyURL + "/proxy/body-tool/v1/models")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if strings.TrimSpace(string(body)) != want {
		t.Errorf("body mismatch: got %q, want %q", string(body), want)
	}
}

func TestProxy_StreamingResponse(t *testing.T) {
	chunks := []string{
		`data: {"token":"Hello"}`,
		`data: {"token":" world"}`,
		`data: [DONE]`,
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Transfer-Encoding", "chunked")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("ResponseWriter does not implement Flusher")
			return
		}
		for _, chunk := range chunks {
			fmt.Fprintln(w, chunk)
			flusher.Flush()
		}
	}))
	defer upstream.Close()

	proxyURL, _ := newTestProxy(t, "stream-tool", upstream.URL)
	resp, err := http.Get(proxyURL + "/proxy/stream-tool/api/generate")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	for _, chunk := range chunks {
		if !strings.Contains(string(body), chunk) {
			t.Errorf("chunk %q missing from streamed response", chunk)
		}
	}
}

func TestProxy_QueryString_Forwarded(t *testing.T) {
	var gotQuery string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	proxyURL, _ := newTestProxy(t, "query-tool", upstream.URL)
	http.Get(proxyURL + "/proxy/query-tool/v1/chat?model=llama3&stream=true") //nolint:errcheck

	if gotQuery != "model=llama3&stream=true" {
		t.Errorf("query not forwarded: got %q", gotQuery)
	}
}

func TestProxy_MemoryInterceptor(t *testing.T) {
	var gotMemHeader string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMemHeader = r.Header.Get("X-Nexus-Memory-Context")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	proxyURL, p := newTestProxy(t, "mem-tool", upstream.URL)
	p.AddInterceptor(proxy.NewMemoryInterceptor())
	http.Get(proxyURL + "/proxy/mem-tool/api/generate") //nolint:errcheck

	if gotMemHeader == "" {
		t.Error("X-Nexus-Memory-Context not injected by MemoryInterceptor")
	}
}

func TestProxy_AddRoute_NonLoopbackRejected(t *testing.T) {
	p := proxy.NewProxy(proxy.Config{ListenAddr: "127.0.0.1:0"})
	if err := p.AddRoute("bad", "http://8.8.8.8:11434"); err == nil {
		t.Error("expected error for non-loopback upstream")
	}
}

func TestProxy_Start_Stop(t *testing.T) {
	p := proxy.NewProxy(proxy.Config{ListenAddr: "127.0.0.1:0"})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Start(ctx) }()

	time.Sleep(50 * time.Millisecond) // let listener bind
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Start returned error on clean shutdown: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("proxy did not stop after context cancellation")
	}
}
