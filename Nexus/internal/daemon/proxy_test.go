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
	"net"
	"net/http"
	"testing"

	"github.com/bubblefish-tech/nexus/internal/config"
)

func TestParseTrustedProxies_ValidCIDRs(t *testing.T) {
	cfg := config.TrustedProxiesConfig{
		CIDRs:            []string{"127.0.0.1/32", "::1/128", "10.0.0.0/8"},
		ForwardedHeaders: []string{"X-Forwarded-For", "X-Real-IP"},
	}
	tp, err := parseTrustedProxies(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tp.networks) != 3 {
		t.Errorf("networks count = %d; want 3", len(tp.networks))
	}
	if len(tp.forwardedHeaders) != 2 {
		t.Errorf("headers count = %d; want 2", len(tp.forwardedHeaders))
	}
}

func TestParseTrustedProxies_InvalidCIDR(t *testing.T) {
	cfg := config.TrustedProxiesConfig{
		CIDRs: []string{"not-a-cidr"},
	}
	_, err := parseTrustedProxies(cfg)
	if err == nil {
		t.Fatal("expected error for invalid CIDR")
	}
}

func TestParseTrustedProxies_EmptyCIDRs(t *testing.T) {
	cfg := config.TrustedProxiesConfig{
		CIDRs:            []string{},
		ForwardedHeaders: []string{"X-Forwarded-For"},
	}
	tp, err := parseTrustedProxies(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tp.networks) != 0 {
		t.Errorf("networks count = %d; want 0", len(tp.networks))
	}
}

func TestParseTrustedProxies_SkipsEmptyStrings(t *testing.T) {
	cfg := config.TrustedProxiesConfig{
		CIDRs: []string{"127.0.0.1/32", "", "  "},
	}
	tp, err := parseTrustedProxies(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tp.networks) != 1 {
		t.Errorf("networks count = %d; want 1", len(tp.networks))
	}
}

func TestIsTrusted(t *testing.T) {
	tp, err := parseTrustedProxies(config.TrustedProxiesConfig{
		CIDRs: []string{"10.0.0.0/8", "::1/128"},
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	tests := []struct {
		ip   string
		want bool
	}{
		{"10.0.0.1", true},
		{"10.255.255.255", true},
		{"192.168.1.1", false},
		{"::1", true},
		{"::2", false},
		{"127.0.0.1", false},
	}

	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			parsed := net.ParseIP(tt.ip)
			if parsed == nil {
				t.Fatalf("invalid test IP: %s", tt.ip)
			}
			if got := tp.isTrusted(parsed); got != tt.want {
				t.Errorf("isTrusted(%s) = %v; want %v", tt.ip, got, tt.want)
			}
		})
	}
}

func TestEffectiveClientIP(t *testing.T) {
	tp, err := parseTrustedProxies(config.TrustedProxiesConfig{
		CIDRs:            []string{"10.0.0.0/8"},
		ForwardedHeaders: []string{"X-Forwarded-For", "X-Real-IP"},
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	tests := []struct {
		name       string
		remoteAddr string
		headers    map[string]string
		want       string
	}{
		{
			name:       "untrusted source ignores forwarded headers",
			remoteAddr: "192.168.1.1:12345",
			headers:    map[string]string{"X-Forwarded-For": "1.2.3.4"},
			want:       "192.168.1.1",
		},
		{
			name:       "trusted source reads X-Forwarded-For",
			remoteAddr: "10.0.0.1:12345",
			headers:    map[string]string{"X-Forwarded-For": "203.0.113.50"},
			want:       "203.0.113.50",
		},
		{
			name:       "trusted source reads leftmost X-Forwarded-For",
			remoteAddr: "10.0.0.1:12345",
			headers:    map[string]string{"X-Forwarded-For": "203.0.113.50, 10.0.0.1"},
			want:       "203.0.113.50",
		},
		{
			name:       "trusted source falls back to X-Real-IP",
			remoteAddr: "10.0.0.1:12345",
			headers:    map[string]string{"X-Real-IP": "198.51.100.1"},
			want:       "198.51.100.1",
		},
		{
			name:       "trusted source prefers first header in order",
			remoteAddr: "10.0.0.1:12345",
			headers: map[string]string{
				"X-Forwarded-For": "203.0.113.50",
				"X-Real-IP":       "198.51.100.1",
			},
			want: "203.0.113.50",
		},
		{
			name:       "trusted source no headers falls back to TCP",
			remoteAddr: "10.0.0.1:12345",
			headers:    map[string]string{},
			want:       "10.0.0.1",
		},
		{
			name:       "no port in remote addr",
			remoteAddr: "192.168.1.1",
			headers:    map[string]string{},
			want:       "192.168.1.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _ := http.NewRequest("GET", "/", nil)
			r.RemoteAddr = tt.remoteAddr
			for k, v := range tt.headers {
				r.Header.Set(k, v)
			}
			got := tp.effectiveClientIP(r)
			if got != tt.want {
				t.Errorf("effectiveClientIP() = %q; want %q", got, tt.want)
			}
		})
	}
}

func TestEffectiveClientIP_NilProxies(t *testing.T) {
	var tp *trustedProxies
	r, _ := http.NewRequest("GET", "/", nil)
	r.RemoteAddr = "1.2.3.4:5678"
	got := tp.effectiveClientIP(r)
	if got != "1.2.3.4" {
		t.Errorf("effectiveClientIP() = %q; want %q", got, "1.2.3.4")
	}
}

func TestEffectiveClientIP_EmptyNetworks(t *testing.T) {
	tp := &trustedProxies{
		forwardedHeaders: []string{"X-Forwarded-For"},
	}
	r, _ := http.NewRequest("GET", "/", nil)
	r.RemoteAddr = "1.2.3.4:5678"
	r.Header.Set("X-Forwarded-For", "spoofed")
	got := tp.effectiveClientIP(r)
	if got != "1.2.3.4" {
		t.Errorf("effectiveClientIP() = %q; want %q (should ignore headers with no networks)", got, "1.2.3.4")
	}
}

func TestExtractIP(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"127.0.0.1:8080", "127.0.0.1"},
		{"[::1]:8080", "::1"},
		{"127.0.0.1", "127.0.0.1"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := extractIP(tt.input); got != tt.want {
				t.Errorf("extractIP(%q) = %q; want %q", tt.input, got, tt.want)
			}
		})
	}
}
