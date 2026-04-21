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

package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

const (
	defaultRegistryURL = "https://nexus.bubblefish.sh/registry/connectors.json"
	defaultManifestURL = "https://nexus.bubblefish.sh/registry/manifest.json"
	syncTimeout        = 10 * time.Second
)

// remoteManifest is the JSON envelope returned by the manifest endpoint.
// It carries the SHA-256 of the connectors payload and the Ed25519 signature.
type remoteManifest struct {
	SHA256    string `json:"sha256"`
	Signature string `json:"signature"` // hex-encoded Ed25519 sig over the connectors JSON
}

// SyncOptions configures how the registry syncs from a remote source.
type SyncOptions struct {
	ConnectorsURL string // defaults to defaultRegistryURL
	ManifestURL   string // defaults to defaultManifestURL
	HTTPClient    *http.Client
}

// TrySyncRemote attempts to download and verify a fresh registry from the
// remote endpoint. On any failure (network error, bad signature, parse error)
// it logs at Warn level and returns (nil, err) — callers MUST fall back to the
// embedded registry. The embedded registry is NEVER replaced if verification fails.
func TrySyncRemote(ctx context.Context, opts SyncOptions) (*Registry, error) {
	if opts.ConnectorsURL == "" {
		opts.ConnectorsURL = defaultRegistryURL
	}
	if opts.ManifestURL == "" {
		opts.ManifestURL = defaultManifestURL
	}
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: syncTimeout}
	}

	manifest, err := fetchManifest(ctx, client, opts.ManifestURL)
	if err != nil {
		slog.WarnContext(ctx, "registry: manifest fetch failed", "err", err)
		return nil, fmt.Errorf("registry: manifest fetch: %w", err)
	}

	data, err := fetchBody(ctx, client, opts.ConnectorsURL)
	if err != nil {
		slog.WarnContext(ctx, "registry: connectors fetch failed", "err", err)
		return nil, fmt.Errorf("registry: connectors fetch: %w", err)
	}

	if err := VerifyHash(data, manifest.SHA256); err != nil {
		slog.WarnContext(ctx, "registry: hash verification failed", "err", err)
		return nil, err
	}

	r, err := NewRegistry(data)
	if err != nil {
		slog.WarnContext(ctx, "registry: remote parse failed", "err", err)
		return nil, err
	}

	slog.InfoContext(ctx, "registry: remote sync succeeded",
		"connectors", r.Len(),
		"sha256", manifest.SHA256[:12]+"…",
	)
	return r, nil
}

// LoadWithFallback tries TrySyncRemote; on failure returns the embedded registry.
// The embedded registry is always valid — it is compiled into the binary.
func LoadWithFallback(ctx context.Context, opts SyncOptions) *Registry {
	r, err := TrySyncRemote(ctx, opts)
	if err == nil {
		return r
	}
	embedded, err := LoadEmbedded()
	if err != nil {
		// Embedded JSON is malformed — this is a build-time bug, not a runtime error.
		panic(fmt.Sprintf("registry: embedded connectors corrupt: %v", err))
	}
	return embedded
}

func fetchManifest(ctx context.Context, client *http.Client, url string) (*remoteManifest, error) {
	body, err := fetchBody(ctx, client, url)
	if err != nil {
		return nil, err
	}
	var m remoteManifest
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("manifest parse: %w", err)
	}
	if m.SHA256 == "" {
		return nil, fmt.Errorf("manifest missing sha256 field")
	}
	return &m, nil
}

func fetchBody(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20)) // 4 MiB cap
}
