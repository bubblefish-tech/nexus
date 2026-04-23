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
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/tidwall/gjson"
)

func TestGenerateKey(t *testing.T) {
	t.Helper()

	tests := []struct {
		prefix  string
		wantLen int
	}{
		{"bfn_admin_", 10 + 64},
		{"bfn_data_", 9 + 64},
		{"bfn_mcp_", 8 + 64},
	}

	for _, tt := range tests {
		key := generateKey(tt.prefix)
		if !strings.HasPrefix(key, tt.prefix) {
			t.Errorf("expected prefix %q, got %q", tt.prefix, key[:len(tt.prefix)])
		}
		if len(key) != tt.wantLen {
			t.Errorf("prefix=%q: expected %d chars, got %d", tt.prefix, tt.wantLen, len(key))
		}
	}

	// Keys must be unique.
	key1 := generateKey("bfn_data_")
	key2 := generateKey("bfn_data_")
	if key1 == key2 {
		t.Fatal("two generated keys should not be identical")
	}
}

func TestBuildDaemonTOML(t *testing.T) {
	t.Helper()
	tests := []struct {
		name string
		mode string
		want []string // substrings that must appear
	}{
		{
			name: "simple mode",
			mode: "simple",
			want: []string{`mode = "simple"`, `log_format = "text"`, `global_requests_per_minute = 5000`},
		},
		{
			name: "balanced mode",
			mode: "balanced",
			want: []string{`mode = "balanced"`, `log_format = "json"`, `mode = "crc32"`},
		},
		{
			name: "safe mode",
			mode: "safe",
			want: []string{`mode = "safe"`, `mode = "mac"`, `enabled = true`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, _ := buildDaemonTOML("/test/config", tt.mode, "test-admin-key", "test-mcp-key", "")
			for _, w := range tt.want {
				if !strings.Contains(result, w) {
					t.Errorf("mode=%s: expected %q in output", tt.mode, w)
				}
			}
		})
	}
}

func TestWriteConfigFile(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.toml")

	// First write should succeed.
	if err := writeConfigFile(path, "hello", false); err != nil {
		t.Fatalf("first write failed: %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "hello" {
		t.Fatalf("expected %q, got %q", "hello", string(data))
	}

	// Second write without force should be a no-op (skip existing).
	if err := writeConfigFile(path, "world", false); err != nil {
		t.Fatalf("second write failed: %v", err)
	}
	data, _ = os.ReadFile(path)
	if string(data) != "hello" {
		t.Fatalf("expected file to remain %q, got %q", "hello", string(data))
	}

	// Write with force should overwrite.
	if err := writeConfigFile(path, "forced", true); err != nil {
		t.Fatalf("force write failed: %v", err)
	}
	data, _ = os.ReadFile(path)
	if string(data) != "forced" {
		t.Fatalf("expected %q after force, got %q", "forced", string(data))
	}
}

func TestWriteDestination(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	destDir := filepath.Join(dir, "destinations")
	if err := os.MkdirAll(destDir, 0700); err != nil {
		t.Fatal(err)
	}

	if err := writeDestination(dir, "sqlite", false); err != nil {
		t.Fatalf("writeDestination(sqlite) failed: %v", err)
	}
	path := filepath.Join(destDir, "sqlite.toml")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file %q to exist", path)
	}
}

func TestWriteDestinationUnknown(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	destDir := filepath.Join(dir, "destinations")
	if err := os.MkdirAll(destDir, 0700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	err := writeDestination(dir, "badtype", false)
	if err == nil {
		t.Fatal("expected error for unknown destination type")
	}
}

// testPrompt returns a promptFunc that feeds lines from the given slice.
func testPrompt(lines []string) promptFunc {
	idx := 0
	return func(w io.Writer, r io.Reader, prompt string) (string, error) {
		if idx >= len(lines) {
			return "", io.EOF
		}
		line := lines[idx]
		idx++
		return line, nil
	}
}

// testOpts returns installOptions pointed at a temp directory with canned
// prompt responses. The caller can override fields before calling doInstall.
func testOpts(t *testing.T, promptLines []string) installOptions {
	t.Helper()
	dir := t.TempDir()
	return installOptions{
		dest:      "sqlite",
		mode:      "balanced",
		force:     false,
		configDir: dir,
		prompt:    testPrompt(promptLines),
		stdin:     strings.NewReader(""),
		stdout:    &bytes.Buffer{},
		stderr:    &bytes.Buffer{},
	}
}

func TestDoInstallSQLite(t *testing.T) {
	t.Helper()
	opts := testOpts(t, nil)
	if err := doInstall(opts); err != nil {
		t.Fatalf("doInstall: %v", err)
	}

	// Verify files exist.
	for _, rel := range []string{
		"daemon.toml",
		filepath.Join("destinations", "sqlite.toml"),
		filepath.Join("sources", "default.toml"),
	} {
		path := filepath.Join(opts.configDir, rel)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected %q to exist", rel)
		}
	}

	// Verify TOML content contains expected keys.
	data, _ := os.ReadFile(filepath.Join(opts.configDir, "destinations", "sqlite.toml"))
	if !strings.Contains(string(data), `name = "sqlite"`) {
		t.Error("sqlite.toml missing name field")
	}
}

func TestDoInstallPostgresPrompted(t *testing.T) {
	t.Helper()
	opts := testOpts(t, []string{"postgres://user:pass@localhost:5432/testdb"})
	opts.dest = "postgres"

	if err := doInstall(opts); err != nil {
		t.Fatalf("doInstall: %v", err)
	}

	// Verify postgres.toml was written with the prompted DSN.
	data, err := os.ReadFile(filepath.Join(opts.configDir, "destinations", "postgres.toml"))
	if err != nil {
		t.Fatalf("read postgres.toml: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, `name = "postgres"`) {
		t.Error("postgres.toml missing name field")
	}
	if !strings.Contains(content, `dsn = "postgres://user:pass@localhost:5432/testdb"`) {
		t.Error("postgres.toml missing prompted DSN")
	}

	// Verify doctor output (connectivity check skipped for env: refs but
	// attempted for literal DSNs -- will fail but should not error the install).
	stdout := opts.stdout.(*bytes.Buffer).String()
	if !strings.Contains(stdout, "doctor: checking PostgreSQL") {
		t.Error("expected doctor check output for literal postgres DSN")
	}

	// Verify source targets postgres.
	srcData, _ := os.ReadFile(filepath.Join(opts.configDir, "sources", "default.toml"))
	if !strings.Contains(string(srcData), `target_destination = "postgres"`) {
		t.Error("default source should target postgres")
	}
}

func TestDoInstallPostgresEnvRef(t *testing.T) {
	t.Helper()
	// Empty input -> defaults to env:NEXUS_POSTGRES_DSN.
	opts := testOpts(t, []string{""})
	opts.dest = "postgres"

	if err := doInstall(opts); err != nil {
		t.Fatalf("doInstall: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(opts.configDir, "destinations", "postgres.toml"))
	if !strings.Contains(string(data), `dsn = "env:NEXUS_POSTGRES_DSN"`) {
		t.Error("empty prompt should default to env:NEXUS_POSTGRES_DSN")
	}

	// Doctor check should NOT run for env: refs.
	stdout := opts.stdout.(*bytes.Buffer).String()
	if strings.Contains(stdout, "doctor:") {
		t.Error("doctor check should not run for env: references")
	}
}

func TestDoInstallOpenBrainPrompted(t *testing.T) {
	t.Helper()
	opts := testOpts(t, []string{
		"https://xyzproject.supabase.co",
		"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.test",
	})
	opts.dest = "openbrain"

	if err := doInstall(opts); err != nil {
		t.Fatalf("doInstall: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(opts.configDir, "destinations", "openbrain.toml"))
	if err != nil {
		t.Fatalf("read openbrain.toml: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, `name = "openbrain"`) {
		t.Error("openbrain.toml missing name field")
	}
	if !strings.Contains(content, `url = "https://xyzproject.supabase.co"`) {
		t.Error("openbrain.toml missing prompted URL")
	}
	if !strings.Contains(content, `api_key = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.test"`) {
		t.Error("openbrain.toml missing prompted key")
	}

	// Doctor check attempted (will fail -- no real supabase).
	stdout := opts.stdout.(*bytes.Buffer).String()
	if !strings.Contains(stdout, "doctor: checking OpenBrain") {
		t.Error("expected doctor check output for literal openbrain credentials")
	}
}

func TestDoInstallOpenBrainEnvRefs(t *testing.T) {
	t.Helper()
	opts := testOpts(t, []string{"", ""})
	opts.dest = "openbrain"

	if err := doInstall(opts); err != nil {
		t.Fatalf("doInstall: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(opts.configDir, "destinations", "openbrain.toml"))
	content := string(data)
	if !strings.Contains(content, `url = "env:NEXUS_OPENBRAIN_URL"`) {
		t.Error("empty URL prompt should default to env:NEXUS_OPENBRAIN_URL")
	}
	if !strings.Contains(content, `api_key = "env:NEXUS_OPENBRAIN_KEY"`) {
		t.Error("empty key prompt should default to env:NEXUS_OPENBRAIN_KEY")
	}

	// Doctor check should NOT run for env: refs.
	stdout := opts.stdout.(*bytes.Buffer).String()
	if strings.Contains(stdout, "doctor:") {
		t.Error("doctor check should not run for env: references")
	}
}

func TestDoInstallOpenWebUIProfile(t *testing.T) {
	t.Helper()
	opts := testOpts(t, nil)
	opts.profile = "openwebui"

	if err := doInstall(opts); err != nil {
		t.Fatalf("doInstall: %v", err)
	}

	// Verify source config.
	srcPath := filepath.Join(opts.configDir, "sources", "openwebui.toml")
	data, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("openwebui.toml not created: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, `name = "openwebui"`) {
		t.Error("openwebui.toml missing name")
	}
	if !strings.Contains(content, `content = "messages.-1.content"`) {
		t.Error("openwebui.toml missing mapping")
	}

	// Verify sqlite destination was also created (default for openwebui).
	destPath := filepath.Join(opts.configDir, "destinations", "sqlite.toml")
	if _, err := os.Stat(destPath); err != nil {
		t.Error("openwebui profile should also create sqlite destination")
	}

	// Verify example provider JSON.
	jsonPath := filepath.Join(opts.configDir, "examples", "openwebui-provider.json")
	jsonData, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("openwebui-provider.json not created: %v", err)
	}
	jsonContent := string(jsonData)
	if !strings.Contains(jsonContent, `"name": "BubbleFish Nexus"`) {
		t.Error("provider JSON missing name")
	}
	if !strings.Contains(jsonContent, `/inbound/openwebui`) {
		t.Error("provider JSON missing write endpoint")
	}
}

func TestDoInstallOAuthTemplateCaddy(t *testing.T) {
	t.Helper()
	opts := testOpts(t, nil)
	opts.oauthTemplate = "caddy"

	if err := doInstall(opts); err != nil {
		t.Fatalf("doInstall: %v", err)
	}

	path := filepath.Join(opts.configDir, "examples", "oauth", "Caddyfile")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Caddyfile not created: %v", err)
	}
	if !strings.Contains(string(data), "forward_auth") {
		t.Error("Caddyfile missing forward_auth directive")
	}
}

func TestDoInstallOAuthTemplateTraefik(t *testing.T) {
	t.Helper()
	opts := testOpts(t, nil)
	opts.oauthTemplate = "traefik"

	if err := doInstall(opts); err != nil {
		t.Fatalf("doInstall: %v", err)
	}

	path := filepath.Join(opts.configDir, "examples", "oauth", "traefik.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("traefik.yml not created: %v", err)
	}
	if !strings.Contains(string(data), "forwardAuth") {
		t.Error("traefik.yml missing forwardAuth middleware")
	}
}

func TestDoInstallOAuthTemplateUnknown(t *testing.T) {
	t.Helper()
	opts := testOpts(t, nil)
	opts.oauthTemplate = "nginx"

	err := doInstall(opts)
	if err == nil {
		t.Fatal("expected error for unknown oauth template")
	}
	if !strings.Contains(err.Error(), "unknown oauth template") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDoInstallRefusesWithoutForce(t *testing.T) {
	t.Helper()
	opts := testOpts(t, nil)

	// First install succeeds.
	if err := doInstall(opts); err != nil {
		t.Fatalf("first install: %v", err)
	}

	// Second install without force should fail.
	err := doInstall(opts)
	if err == nil {
		t.Fatal("expected error without --force on existing config")
	}
	if !strings.Contains(err.Error(), "config already exists") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDoInstallForceOverwrites(t *testing.T) {
	t.Helper()
	opts := testOpts(t, nil)

	if err := doInstall(opts); err != nil {
		t.Fatalf("first install: %v", err)
	}

	opts.force = true
	if err := doInstall(opts); err != nil {
		t.Fatalf("force install should succeed: %v", err)
	}
}

func TestDoInstallSimpleModeNextSteps(t *testing.T) {
	t.Helper()
	opts := testOpts(t, nil)
	opts.mode = "simple"

	if err := doInstall(opts); err != nil {
		t.Fatalf("doInstall: %v", err)
	}

	stdout := opts.stdout.(*bytes.Buffer).String()
	if !strings.Contains(stdout, "nexus start") {
		t.Error("simple mode should print 'nexus start' as step 1")
	}
	if !strings.Contains(stdout, "curl.exe -X POST") {
		t.Error("simple mode should print curl.exe example as step 2")
	}
	if !strings.Contains(stdout, "My first BubbleFish memory") {
		t.Error("simple mode should use 'My first BubbleFish memory' in example payload")
	}
	if !strings.Contains(stdout, "/query/") {
		t.Error("simple mode should print read-back command as step 3")
	}
	if strings.Contains(stdout, "nexus build") {
		t.Error("simple mode should NOT print 'nexus build'")
	}
}

func TestDoInstallBalancedModeNextSteps(t *testing.T) {
	t.Helper()
	opts := testOpts(t, nil)
	opts.mode = "balanced"

	if err := doInstall(opts); err != nil {
		t.Fatalf("doInstall: %v", err)
	}

	stdout := opts.stdout.(*bytes.Buffer).String()
	if !strings.Contains(stdout, "nexus build") {
		t.Error("balanced mode should print 'nexus build'")
	}
	if !strings.Contains(stdout, "nexus doctor") {
		t.Error("balanced mode should print 'nexus doctor'")
	}
}

func TestDoInstallNeverSilent(t *testing.T) {
	t.Helper()
	tests := []struct {
		name string
		dest string
		mode string
	}{
		{"sqlite-balanced", "sqlite", "balanced"},
		{"sqlite-simple", "sqlite", "simple"},
		{"sqlite-safe", "sqlite", "safe"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := testOpts(t, nil)
			opts.dest = tt.dest
			opts.mode = tt.mode

			if err := doInstall(opts); err != nil {
				t.Fatalf("doInstall: %v", err)
			}

			stdout := opts.stdout.(*bytes.Buffer).String()
			if !strings.Contains(stdout, "nexus install: ok") {
				t.Error("install must print ok line -- NEVER silent")
			}
			if !strings.Contains(stdout, "config directory:") {
				t.Error("install must print config directory")
			}
			if !strings.Contains(stdout, "admin token:") {
				t.Error("install must print admin token")
			}
			if !strings.Contains(stdout, "source API key:") {
				t.Error("install must print source API key")
			}
			if !strings.Contains(stdout, "MCP API key:") {
				t.Error("install must print MCP API key")
			}
		})
	}
}

func TestWritePostgresDestination(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	destDir := filepath.Join(dir, "destinations")
	if err := os.MkdirAll(destDir, 0700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	if err := writePostgresDestination(dir, "postgres://u:p@h/d", false); err != nil {
		t.Fatalf("writePostgresDestination: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(destDir, "postgres.toml"))
	content := string(data)
	if !strings.Contains(content, `name = "postgres"`) {
		t.Error("missing name")
	}
	if !strings.Contains(content, `dsn = "postgres://u:p@h/d"`) {
		t.Error("missing prompted DSN")
	}
}

func TestWriteOpenBrainDestination(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	destDir := filepath.Join(dir, "destinations")
	if err := os.MkdirAll(destDir, 0700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	if err := writeOpenBrainDestination(dir, "https://x.supabase.co", "secret", false); err != nil {
		t.Fatalf("writeOpenBrainDestination: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(destDir, "openbrain.toml"))
	content := string(data)
	if !strings.Contains(content, `name = "openbrain"`) {
		t.Error("missing name")
	}
	if !strings.Contains(content, `url = "https://x.supabase.co"`) {
		t.Error("missing URL")
	}
	if !strings.Contains(content, `api_key = "secret"`) {
		t.Error("missing API key")
	}
}

func TestStdinPrompt(t *testing.T) {
	t.Helper()
	var out bytes.Buffer
	in := strings.NewReader("hello world\n")

	result, err := stdinPrompt(&out, in, "Enter: ")
	if err != nil {
		t.Fatalf("stdinPrompt: %v", err)
	}
	if result != "hello world" {
		t.Errorf("expected %q, got %q", "hello world", result)
	}
	if !strings.Contains(out.String(), "Enter: ") {
		t.Error("prompt string should be written to output")
	}
}

func TestStdinPromptEmpty(t *testing.T) {
	t.Helper()
	var out bytes.Buffer
	in := strings.NewReader("\n")

	result, err := stdinPrompt(&out, in, "Prompt: ")
	if err != nil {
		t.Fatalf("stdinPrompt: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestStdinPromptEOF(t *testing.T) {
	t.Helper()
	var out bytes.Buffer
	in := strings.NewReader("")

	_, err := stdinPrompt(&out, in, "Prompt: ")
	if err != io.EOF {
		t.Errorf("expected io.EOF, got %v", err)
	}
}

func TestWriteOpenWebUIProviderExample(t *testing.T) {
	t.Helper()
	dir := t.TempDir()

	if err := writeOpenWebUIProviderExample(dir, false); err != nil {
		t.Fatalf("writeOpenWebUIProviderExample: %v", err)
	}

	path := filepath.Join(dir, "examples", "openwebui-provider.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, `"name": "BubbleFish Nexus"`) {
		t.Error("missing name")
	}
	if !strings.Contains(content, `/inbound/openwebui`) {
		t.Error("missing write endpoint")
	}
	if !strings.Contains(content, `/retrieve`) {
		t.Error("missing read endpoint")
	}
	if !strings.Contains(content, `CHANGE_ME`) {
		t.Error("missing placeholder API key")
	}
}

func TestAllProfilesGenerateValidTOML(t *testing.T) {
	t.Helper()

	tests := []struct {
		name          string
		dest          string
		profile       string
		oauthTemplate string
		promptLines   []string
		wantFiles     []string
	}{
		{
			name: "sqlite default",
			dest: "sqlite",
			wantFiles: []string{
				"daemon.toml",
				filepath.Join("destinations", "sqlite.toml"),
				filepath.Join("sources", "default.toml"),
			},
		},
		{
			name:        "postgres with DSN",
			dest:        "postgres",
			promptLines: []string{"postgres://u:p@h/d"},
			wantFiles: []string{
				"daemon.toml",
				filepath.Join("destinations", "postgres.toml"),
				filepath.Join("sources", "default.toml"),
			},
		},
		{
			name:        "openbrain with credentials",
			dest:        "openbrain",
			promptLines: []string{"https://x.supabase.co", "key123"},
			wantFiles: []string{
				"daemon.toml",
				filepath.Join("destinations", "openbrain.toml"),
				filepath.Join("sources", "default.toml"),
			},
		},
		{
			name:    "openwebui profile",
			dest:    "sqlite",
			profile: "openwebui",
			wantFiles: []string{
				"daemon.toml",
				filepath.Join("destinations", "sqlite.toml"),
				filepath.Join("sources", "default.toml"),
				filepath.Join("sources", "openwebui.toml"),
				filepath.Join("examples", "openwebui-provider.json"),
			},
		},
		{
			name:          "caddy oauth template",
			dest:          "sqlite",
			oauthTemplate: "caddy",
			wantFiles: []string{
				"daemon.toml",
				filepath.Join("examples", "oauth", "Caddyfile"),
			},
		},
		{
			name:          "traefik oauth template",
			dest:          "sqlite",
			oauthTemplate: "traefik",
			wantFiles: []string{
				"daemon.toml",
				filepath.Join("examples", "oauth", "traefik.yml"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := testOpts(t, tt.promptLines)
			opts.dest = tt.dest
			if tt.profile != "" {
				opts.profile = tt.profile
			}
			if tt.oauthTemplate != "" {
				opts.oauthTemplate = tt.oauthTemplate
			}

			if err := doInstall(opts); err != nil {
				t.Fatalf("doInstall: %v", err)
			}

			for _, rel := range tt.wantFiles {
				path := filepath.Join(opts.configDir, rel)
				info, err := os.Stat(path)
				if err != nil {
					t.Errorf("expected %q to exist", rel)
					continue
				}
				if info.Size() == 0 {
					t.Errorf("expected %q to be non-empty", rel)
				}
			}
		})
	}
}

func TestResolveInstallHome(t *testing.T) {
	t.Helper()

	t.Run("FlagOverridesEnv", func(t *testing.T) {
		flagDir := t.TempDir()
		envDir := t.TempDir()
		t.Setenv("BUBBLEFISH_HOME", envDir)

		got, err := resolveInstallHome(flagDir)
		if err != nil {
			t.Fatalf("resolveInstallHome: %v", err)
		}
		if got != flagDir {
			t.Errorf("expected flag dir %q, got %q", flagDir, got)
		}
	})

	t.Run("EnvOverridesDefault", func(t *testing.T) {
		envDir := t.TempDir()
		t.Setenv("BUBBLEFISH_HOME", envDir)

		got, err := resolveInstallHome("")
		if err != nil {
			t.Fatalf("resolveInstallHome: %v", err)
		}
		if got != envDir {
			t.Errorf("expected env dir %q, got %q", envDir, got)
		}
	})

	t.Run("DefaultWhenNeitherSet", func(t *testing.T) {
		t.Setenv("BUBBLEFISH_HOME", "")

		got, err := resolveInstallHome("")
		if err != nil {
			t.Fatalf("resolveInstallHome: %v", err)
		}
		home, _ := os.UserHomeDir()
		want := filepath.Join(home, ".nexus", "Nexus")
		if got != want {
			t.Errorf("expected default %q, got %q", want, got)
		}
	})
}

func TestDoInstallRespectsConfigDir(t *testing.T) {
	t.Helper()

	t.Run("FlagOverridesEnv", func(t *testing.T) {
		flagDir := t.TempDir()
		envDir := t.TempDir()
		t.Setenv("BUBBLEFISH_HOME", envDir)

		opts := testOpts(t, nil)
		opts.configDir = flagDir

		if err := doInstall(opts); err != nil {
			t.Fatalf("doInstall: %v", err)
		}

		stdout := opts.stdout.(*bytes.Buffer).String()
		if !strings.Contains(stdout, flagDir) {
			t.Errorf("summary should show flag dir %q, got:\n%s", flagDir, stdout)
		}

		// Verify files written to flag dir, not env dir.
		if _, err := os.Stat(filepath.Join(flagDir, "daemon.toml")); err != nil {
			t.Error("daemon.toml should exist in flag dir")
		}
		if _, err := os.Stat(filepath.Join(envDir, "daemon.toml")); err == nil {
			t.Error("daemon.toml should NOT exist in env dir")
		}
	})

	t.Run("EnvOverridesDefault", func(t *testing.T) {
		envDir := t.TempDir()
		t.Setenv("BUBBLEFISH_HOME", envDir)

		got, err := resolveInstallHome("")
		if err != nil {
			t.Fatalf("resolveInstallHome: %v", err)
		}

		opts := testOpts(t, nil)
		opts.configDir = got

		if err := doInstall(opts); err != nil {
			t.Fatalf("doInstall: %v", err)
		}

		stdout := opts.stdout.(*bytes.Buffer).String()
		if !strings.Contains(stdout, envDir) {
			t.Errorf("summary should show env dir %q, got:\n%s", envDir, stdout)
		}
	})
}

func TestBuildDaemonTOML_PathsRespectConfigDir(t *testing.T) {
	t.Helper()

	type daemonPaths struct {
		Daemon struct {
			WAL struct {
				Path string `toml:"path"`
			} `toml:"wal"`
			Audit struct {
				LogFile string `toml:"log_file"`
			} `toml:"audit"`
		} `toml:"daemon"`
		SecurityEvents struct {
			LogFile string `toml:"log_file"`
		} `toml:"security_events"`
	}

	t.Run("DefaultPath", func(t *testing.T) {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Fatalf("UserHomeDir: %v", err)
		}
		defaultDir := filepath.Join(home, ".nexus", "Nexus")
		content, _ := buildDaemonTOML(defaultDir, "balanced", "test-key", "test-mcp-key", "")

		var parsed daemonPaths
		if _, err := toml.Decode(content, &parsed); err != nil {
			t.Fatalf("TOML decode: %v", err)
		}

		wantWAL := filepath.ToSlash(filepath.Join(defaultDir, "wal"))
		if parsed.Daemon.WAL.Path != wantWAL {
			t.Errorf("WAL path = %q, want %q", parsed.Daemon.WAL.Path, wantWAL)
		}
		wantLog := filepath.ToSlash(filepath.Join(defaultDir, "security.log"))
		if parsed.SecurityEvents.LogFile != wantLog {
			t.Errorf("security log = %q, want %q", parsed.SecurityEvents.LogFile, wantLog)
		}
		wantAudit := filepath.ToSlash(filepath.Join(defaultDir, "logs", "interactions.jsonl"))
		if parsed.Daemon.Audit.LogFile != wantAudit {
			t.Errorf("audit log = %q, want %q", parsed.Daemon.Audit.LogFile, wantAudit)
		}
	})

	t.Run("SandboxPath", func(t *testing.T) {
		sandbox := t.TempDir()
		content, _ := buildDaemonTOML(sandbox, "balanced", "test-key", "test-mcp-key", "")

		var parsed daemonPaths
		if _, err := toml.Decode(content, &parsed); err != nil {
			t.Fatalf("TOML decode: %v", err)
		}

		wantWAL := filepath.ToSlash(filepath.Join(sandbox, "wal"))
		if parsed.Daemon.WAL.Path != wantWAL {
			t.Errorf("WAL path = %q, want %q", parsed.Daemon.WAL.Path, wantWAL)
		}
		if strings.Contains(parsed.Daemon.WAL.Path, ".nexus/Nexus") {
			t.Error("WAL path still contains hardcoded default")
		}

		wantLog := filepath.ToSlash(filepath.Join(sandbox, "security.log"))
		if parsed.SecurityEvents.LogFile != wantLog {
			t.Errorf("security log = %q, want %q", parsed.SecurityEvents.LogFile, wantLog)
		}
		if strings.Contains(parsed.SecurityEvents.LogFile, ".nexus/Nexus") {
			t.Error("security log path still contains hardcoded default")
		}

		wantAudit := filepath.ToSlash(filepath.Join(sandbox, "logs", "interactions.jsonl"))
		if parsed.Daemon.Audit.LogFile != wantAudit {
			t.Errorf("audit log = %q, want %q", parsed.Daemon.Audit.LogFile, wantAudit)
		}
		if strings.Contains(parsed.Daemon.Audit.LogFile, ".nexus/Nexus") {
			t.Error("audit log path still contains hardcoded default")
		}
	})
}

func TestDoInstallOAuthIssuer(t *testing.T) {
	t.Helper()
	opts := testOpts(t, nil)
	opts.oauthIssuer = "https://example.com"

	if err := doInstall(opts); err != nil {
		t.Fatalf("doInstall: %v", err)
	}

	// Verify daemon.toml contains OAuth block.
	data, err := os.ReadFile(filepath.Join(opts.configDir, "daemon.toml"))
	if err != nil {
		t.Fatalf("read daemon.toml: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, `[daemon.oauth]`) {
		t.Error("daemon.toml missing [daemon.oauth] section")
	}
	if !strings.Contains(content, `enabled = true`) {
		t.Error("daemon.toml missing enabled = true")
	}
	if !strings.Contains(content, `issuer_url = "https://example.com"`) {
		t.Error("daemon.toml missing issuer_url")
	}
	if !strings.Contains(content, `private_key_file = "file:`) {
		t.Error("daemon.toml missing private_key_file with file: prefix")
	}
	if !strings.Contains(content, `client_id = "chatgpt"`) {
		t.Error("daemon.toml missing chatgpt client")
	}

	// Verify install summary includes OAuth info.
	stdout := opts.stdout.(*bytes.Buffer).String()
	if !strings.Contains(stdout, "OAuth enabled:") {
		t.Error("install summary should mention OAuth enabled")
	}
	if !strings.Contains(stdout, "https://example.com") {
		t.Error("install summary should mention OAuth issuer URL")
	}
	if !strings.Contains(stdout, "redirect_uris") {
		t.Error("install summary should mention updating redirect_uris")
	}
}

func TestDoInstallNoOAuthByDefault(t *testing.T) {
	t.Helper()
	opts := testOpts(t, nil)

	if err := doInstall(opts); err != nil {
		t.Fatalf("doInstall: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(opts.configDir, "daemon.toml"))
	if err != nil {
		t.Fatalf("read daemon.toml: %v", err)
	}
	content := string(data)
	if strings.Contains(content, `[daemon.oauth]`) {
		t.Error("daemon.toml should NOT contain [daemon.oauth] when --oauth-issuer not provided")
	}
}

func TestBuildDaemonTOML_IncludesAuditSection(t *testing.T) {
	t.Helper()

	type auditPaths struct {
		Daemon struct {
			Audit struct {
				LogFile string `toml:"log_file"`
			} `toml:"audit"`
		} `toml:"daemon"`
	}

	dir := t.TempDir()
	content, _ := buildDaemonTOML(dir, "simple", "test-key", "test-mcp-key", "")

	var parsed auditPaths
	if _, err := toml.Decode(content, &parsed); err != nil {
		t.Fatalf("TOML decode: %v", err)
	}

	want := filepath.ToSlash(filepath.Join(dir, "logs", "interactions.jsonl"))
	if parsed.Daemon.Audit.LogFile != want {
		t.Errorf("audit log_file = %q, want %q", parsed.Daemon.Audit.LogFile, want)
	}
	if strings.HasPrefix(parsed.Daemon.Audit.LogFile, "~/") {
		t.Error("audit log path should not contain tilde reference")
	}
}

func TestBuildSourceTOML_MatchesInstallExamplePayload(t *testing.T) {
	t.Helper()

	// Run a full install in simple mode to generate default.toml.
	opts := testOpts(t, nil)
	opts.mode = "simple"
	if err := doInstall(opts); err != nil {
		t.Fatalf("doInstall: %v", err)
	}

	// Read and parse the generated default source TOML.
	srcPath := filepath.Join(opts.configDir, "sources", "default.toml")
	data, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("read default.toml: %v", err)
	}

	type sourceTOML struct {
		Source struct {
			Mapping map[string]string `toml:"mapping"`
		} `toml:"source"`
	}
	var parsed sourceTOML
	if _, err := toml.Decode(string(data), &parsed); err != nil {
		t.Fatalf("TOML decode: %v", err)
	}

	if len(parsed.Source.Mapping) == 0 {
		t.Fatal("default source TOML has no [source.mapping] section")
	}

	// This is the exact payload from the install Next Steps example.
	payload := `{"content":"My first BubbleFish memory","role":"user","model":"test"}`

	tests := []struct {
		field string
		want  string
	}{
		{"content", "My first BubbleFish memory"},
		{"role", "user"},
		{"model", "test"},
	}

	for _, tt := range tests {
		gPath, ok := parsed.Source.Mapping[tt.field]
		if !ok {
			t.Errorf("mapping missing key %q", tt.field)
			continue
		}
		got := gjson.Get(payload, gPath).String()
		if got != tt.want {
			t.Errorf("mapping[%q]=%q: gjson extracted %q, want %q", tt.field, gPath, got, tt.want)
		}
	}
}
