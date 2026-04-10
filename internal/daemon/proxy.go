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
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/bubblefish-tech/nexus/internal/config"
)

// trustedProxies holds parsed CIDR networks and the list of forwarded headers
// to check when determining the effective client IP.
//
// Reference: Tech Spec Section 6.3.
type trustedProxies struct {
	networks         []*net.IPNet
	forwardedHeaders []string
}

// parseTrustedProxies parses the trusted proxy configuration into a
// trustedProxies struct. Returns an error if any CIDR is invalid.
func parseTrustedProxies(cfg config.TrustedProxiesConfig) (*trustedProxies, error) {
	tp := &trustedProxies{
		forwardedHeaders: cfg.ForwardedHeaders,
	}

	for _, cidr := range cfg.CIDRs {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" {
			continue
		}
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			return nil, fmt.Errorf("trusted_proxies: invalid CIDR %q: %w", cidr, err)
		}
		tp.networks = append(tp.networks, network)
	}

	return tp, nil
}

// isTrusted returns true if the given IP is within any of the trusted CIDR
// networks.
func (tp *trustedProxies) isTrusted(ip net.IP) bool {
	for _, network := range tp.networks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// effectiveClientIP determines the effective client IP for a request.
//
// If the TCP source IP is in a trusted CIDR, the forwarded headers are checked
// in order. The first non-empty value is used. For X-Forwarded-For, the
// leftmost (client) IP is extracted.
//
// If the TCP source IP is NOT in a trusted CIDR, the TCP source IP is returned
// directly. This prevents header spoofing from untrusted clients.
//
// Reference: Tech Spec Section 6.3.
func (tp *trustedProxies) effectiveClientIP(r *http.Request) string {
	tcpIP := extractIP(r.RemoteAddr)
	if tp == nil || len(tp.networks) == 0 {
		return tcpIP
	}

	parsed := net.ParseIP(tcpIP)
	if parsed == nil {
		return tcpIP
	}

	if !tp.isTrusted(parsed) {
		return tcpIP
	}

	// TCP source is trusted — check forwarded headers in order.
	for _, header := range tp.forwardedHeaders {
		val := r.Header.Get(header)
		if val == "" {
			continue
		}
		// X-Forwarded-For can contain a comma-separated list.
		// The leftmost entry is the original client IP.
		if strings.Contains(val, ",") {
			val = strings.TrimSpace(strings.SplitN(val, ",", 2)[0])
		}
		ip := strings.TrimSpace(val)
		if ip != "" {
			return ip
		}
	}

	// No forwarded header found — fall back to TCP source.
	return tcpIP
}

// extractIP extracts the IP address from a "host:port" string (RemoteAddr).
// If no port is present, the raw string is returned.
func extractIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		// remoteAddr might not have a port (e.g. unix socket).
		return remoteAddr
	}
	return host
}
