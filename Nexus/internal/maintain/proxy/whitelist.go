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

package proxy

import (
	"net"
	"net/url"
	"sync"
)

// AllowList is a thread-safe set of permitted upstream base URLs (scheme+host).
// Two invariants are enforced at Add time:
//  1. Only loopback addresses are permitted — prevents SSRF to external hosts.
//  2. Membership is matched by normalised scheme+host, not by full URL — a
//     registered base URL covers all paths on that host.
type AllowList struct {
	mu      sync.RWMutex
	allowed map[string]struct{} // normalised "scheme://host" keys
}

// NewAllowList returns an AllowList pre-seeded with the given URLs.
// Non-loopback or unparseable URLs are silently dropped.
func NewAllowList(urls []string) *AllowList {
	al := &AllowList{allowed: make(map[string]struct{})}
	for _, u := range urls {
		al.Add(u)
	}
	return al
}

// Add registers rawURL in the allowlist. Non-loopback hosts are rejected silently
// because forwarding to non-local addresses would expose the proxy as an SSRF vector.
func (al *AllowList) Add(rawURL string) {
	key := normaliseKey(rawURL)
	if key == "" {
		return
	}
	al.mu.Lock()
	al.allowed[key] = struct{}{}
	al.mu.Unlock()
}

// IsAllowed returns true when rawURL's scheme+host appears in the allowlist
// AND the host resolves to a loopback address.
func (al *AllowList) IsAllowed(rawURL string) bool {
	key := normaliseKey(rawURL)
	if key == "" {
		return false
	}
	al.mu.RLock()
	_, ok := al.allowed[key]
	al.mu.RUnlock()
	return ok
}

// Snapshot returns all currently allowed keys (for diagnostics/logging only).
func (al *AllowList) Snapshot() []string {
	al.mu.RLock()
	defer al.mu.RUnlock()
	out := make([]string, 0, len(al.allowed))
	for k := range al.allowed {
		out = append(out, k)
	}
	return out
}

// normaliseKey parses rawURL and returns "scheme://host" if the host is a
// loopback address, or "" if invalid or not loopback.
func normaliseKey(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return ""
	}
	host, _, err := net.SplitHostPort(u.Host)
	if err != nil {
		host = u.Host // no port in the URL
	}
	ip := net.ParseIP(host)
	if ip == nil {
		// hostname — only "localhost" is allowed (resolves to loopback)
		if host != "localhost" {
			return ""
		}
	} else if !ip.IsLoopback() {
		return ""
	}
	// Normalise: lowercase scheme + host (with port if present)
	return u.Scheme + "://" + u.Host
}
