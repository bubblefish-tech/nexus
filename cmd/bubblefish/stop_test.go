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

package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// writeStopTestConfig writes a daemon.toml with a given port (use a high port
// that nothing is listening on for "no daemon" tests).
func writeStopTestConfig(t *testing.T, dir string, port int) {
	t.Helper()

	for _, sub := range []string{"sources", "destinations", "wal"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0700); err != nil {
			t.Fatalf("mkdir %s: %v", sub, err)
		}
	}

	daemonContent := fmt.Sprintf(`[daemon]
port = %d
bind = "127.0.0.1"
admin_token = "bfn_admin_testtoken0000000000000000000000000000000000000000000000"
mode = "simple"

[daemon.wal]
path = "%s/wal"

[security_events]
enabled = false
`, port, strings.ReplaceAll(dir, `\`, `/`))
	if err := os.WriteFile(filepath.Join(dir, "daemon.toml"), []byte(daemonContent), 0600); err != nil {
		t.Fatalf("write daemon.toml: %v", err)
	}

	destContent := fmt.Sprintf(`[destination]
name = "sqlite"
type = "sqlite"
db_path = "%s/memories.db"
`, strings.ReplaceAll(dir, `\`, `/`))
	if err := os.WriteFile(filepath.Join(dir, "destinations", "sqlite.toml"), []byte(destContent), 0600); err != nil {
		t.Fatalf("write sqlite.toml: %v", err)
	}

	srcContent := `[source]
name = "default"
api_key = "bfn_data_testkey00000000000000000000000000000000000000000000000000"
`
	if err := os.WriteFile(filepath.Join(dir, "sources", "default.toml"), []byte(srcContent), 0600); err != nil {
		t.Fatalf("write default.toml: %v", err)
	}
}

func TestStop_NoDaemonRunning(t *testing.T) {
	t.Helper()

	dir := t.TempDir()
	// Use a high port that nothing is listening on.
	writeStopTestConfig(t, dir, 59123)

	var stdout, stderr bytes.Buffer
	opts := stopOptions{
		configDir: dir,
		timeout:   5,
		stdout:    &stdout,
		stderr:    &stderr,
		client:    &http.Client{},
	}

	code := doStop(opts)
	if code != 0 {
		t.Fatalf("expected exit 0 when no daemon running, got %d; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "no daemon running") {
		t.Errorf("expected 'no daemon running' message, got: %s", stdout.String())
	}
}

func TestStop_HealthcheckFails(t *testing.T) {
	t.Helper()

	dir := t.TempDir()
	writeStopTestConfig(t, dir, 59124)

	var stdout, stderr bytes.Buffer
	opts := stopOptions{
		configDir: dir,
		timeout:   5,
		stdout:    &stdout,
		stderr:    &stderr,
		client:    &http.Client{},
	}

	code := doStop(opts)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "no daemon running") {
		t.Errorf("expected 'no daemon running', got: %s", stdout.String())
	}
}

func TestStop_AdminTokenInHeader(t *testing.T) {
	t.Helper()

	var gotAuth atomic.Value
	var shutdownCalled atomic.Int32
	var healthAfterShutdown atomic.Int32

	// Fake daemon: /health always OK, /api/shutdown captures auth header and
	// returns 202, then after shutdown is called /health starts failing.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			if shutdownCalled.Load() > 0 {
				n := healthAfterShutdown.Add(1)
				// After 1 poll, stop responding to simulate daemon exit.
				if n >= 2 {
					// Close the connection abruptly.
					hj, ok := w.(http.Hijacker)
					if ok {
						conn, _, _ := hj.Hijack()
						if conn != nil {
							_ = conn.Close()
						}
						return
					}
				}
			}
			w.WriteHeader(http.StatusOK)

		case "/api/shutdown":
			gotAuth.Store(r.Header.Get("Authorization"))
			shutdownCalled.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"status":"shutting_down"}`))

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	// Write config with the test server's address.
	dir := t.TempDir()
	writeStopTestConfigWithAddr(t, dir, srv.Listener.Addr().String())

	var stdout, stderr bytes.Buffer
	opts := stopOptions{
		configDir: dir,
		timeout:   5,
		stdout:    &stdout,
		stderr:    &stderr,
		client:    srv.Client(),
	}

	code := doStop(opts)

	// Verify the admin token was sent.
	auth, _ := gotAuth.Load().(string)
	if !strings.HasPrefix(auth, "Bearer bfn_admin_") {
		t.Errorf("expected admin token in Authorization header, got: %q", auth)
	}

	// Verify shutdown was called.
	if shutdownCalled.Load() == 0 {
		t.Error("expected /api/shutdown to be called")
	}

	// The stop command should have exited 0 after the fake server stopped
	// responding to /health.
	if code != 0 {
		t.Errorf("expected exit 0, got %d; stdout: %s; stderr: %s", code, stdout.String(), stderr.String())
	}

	if !strings.Contains(stdout.String(), "shutdown requested") {
		t.Errorf("expected 'shutdown requested' message, got: %s", stdout.String())
	}
}

// writeStopTestConfigWithAddr writes config pointing at a custom host:port.
func writeStopTestConfigWithAddr(t *testing.T, dir, addr string) {
	t.Helper()

	parts := strings.SplitN(addr, ":", 2)
	host := parts[0]
	port := parts[1]

	for _, sub := range []string{"sources", "destinations", "wal"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0700); err != nil {
			t.Fatalf("mkdir %s: %v", sub, err)
		}
	}

	daemonContent := fmt.Sprintf(`[daemon]
port = %s
bind = "%s"
admin_token = "bfn_admin_testtoken0000000000000000000000000000000000000000000000"
mode = "simple"

[daemon.wal]
path = "%s/wal"

[security_events]
enabled = false
`, port, host, strings.ReplaceAll(dir, `\`, `/`))
	if err := os.WriteFile(filepath.Join(dir, "daemon.toml"), []byte(daemonContent), 0600); err != nil {
		t.Fatalf("write daemon.toml: %v", err)
	}

	destContent := fmt.Sprintf(`[destination]
name = "sqlite"
type = "sqlite"
db_path = "%s/memories.db"
`, strings.ReplaceAll(dir, `\`, `/`))
	if err := os.WriteFile(filepath.Join(dir, "destinations", "sqlite.toml"), []byte(destContent), 0600); err != nil {
		t.Fatalf("write sqlite.toml: %v", err)
	}

	srcContent := `[source]
name = "default"
api_key = "bfn_data_testkey00000000000000000000000000000000000000000000000000"
`
	if err := os.WriteFile(filepath.Join(dir, "sources", "default.toml"), []byte(srcContent), 0600); err != nil {
		t.Fatalf("write default.toml: %v", err)
	}
}
