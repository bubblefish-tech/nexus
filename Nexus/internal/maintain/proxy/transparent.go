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

// Package proxy implements the transparent AI API proxy: an HTTP reverse proxy
// that sits between AI clients and their inference backends. Every request is
// validated against a loopback-only allowlist (SSRF prevention), then passed
// through the interceptor chain (header injection, memory context stub) before
// being forwarded to the upstream tool.
//
// URL scheme:   /proxy/{tool-name}/{upstream-path...}
// Example:      POST /proxy/ollama/api/generate → http://127.0.0.1:11434/api/generate
package proxy

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
)

// Config holds Proxy configuration.
type Config struct {
	ListenAddr string // TCP address to bind, e.g. "127.0.0.1:9000"
}

// Proxy is the transparent AI API proxy.
type Proxy struct {
	mu           sync.RWMutex
	config       Config
	routes       map[string]*url.URL // tool name → upstream base URL
	allowList    *AllowList
	interceptors []Interceptor
	server       *http.Server
}

// NewProxy creates a Proxy with the given config. No routes are registered
// until AddRoute is called.
func NewProxy(cfg Config) *Proxy {
	p := &Proxy{
		config:    cfg,
		routes:    make(map[string]*url.URL),
		allowList: NewAllowList(nil),
		interceptors: []Interceptor{
			NewHeaderInterceptor(),
		},
	}
	p.server = &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: p,
	}
	return p
}

// AddRoute registers toolName → rawBaseURL in the routing table and allowlist.
// Non-loopback URLs are rejected because the proxy enforces loopback-only upstreams.
func (p *Proxy) AddRoute(toolName, rawBaseURL string) error {
	u, err := url.Parse(rawBaseURL)
	if err != nil {
		return fmt.Errorf("proxy: invalid upstream URL %q: %w", rawBaseURL, err)
	}
	if !isLoopback(u.Host) {
		return fmt.Errorf("proxy: upstream %q is not a loopback address (SSRF protection)", rawBaseURL)
	}
	p.mu.Lock()
	p.routes[toolName] = u
	p.mu.Unlock()
	p.allowList.Add(rawBaseURL)
	return nil
}

// AddInterceptor appends ic to the end of the interceptor chain.
func (p *Proxy) AddInterceptor(ic Interceptor) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.interceptors = append(p.interceptors, ic)
}

// Start binds the listener and serves until ctx is cancelled.
// Returns nil on clean shutdown (ctx.Done), or an error on bind/serve failure.
func (p *Proxy) Start(ctx context.Context) error {
	ln, err := net.Listen("tcp", p.config.ListenAddr)
	if err != nil {
		return fmt.Errorf("proxy: listen %s: %w", p.config.ListenAddr, err)
	}
	slog.InfoContext(ctx, "proxy: listening", "addr", ln.Addr().String())
	go func() {
		<-ctx.Done()
		_ = p.server.Close()
	}()
	if err := p.server.Serve(ln); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("proxy: serve: %w", err)
	}
	return nil
}

// ServeHTTP implements http.Handler. Paths must start with /proxy/{tool-name}/.
// Requests with unknown tool names return 404. Requests whose resolved upstream
// URL is not in the allowlist return 403. All other requests are forwarded.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	toolName, upstreamPath := parsePath(r.URL.Path)
	if toolName == "" {
		http.Error(w, "proxy: path must start with /proxy/{tool}/", http.StatusBadRequest)
		return
	}

	p.mu.RLock()
	upstream, ok := p.routes[toolName]
	interceptors := append([]Interceptor(nil), p.interceptors...)
	p.mu.RUnlock()

	if !ok {
		http.Error(w, fmt.Sprintf("proxy: unknown tool %q", toolName), http.StatusNotFound)
		return
	}

	target := buildTargetURL(upstream, upstreamPath, r.URL.RawQuery)
	if !p.allowList.IsAllowed(target.String()) {
		http.Error(w, "proxy: upstream not in allowlist", http.StatusForbidden)
		return
	}

	rp := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL = target
			req.Host = upstream.Host
			// Remove X-Forwarded-For — we're a local proxy, not an edge.
			req.Header.Del("X-Forwarded-For")
			for _, ic := range interceptors {
				if err := ic.InterceptRequest(req); err != nil {
					// Director cannot return errors; log and continue.
					slog.Warn("proxy: interceptor request error", "err", err)
				}
			}
		},
		ModifyResponse: func(resp *http.Response) error {
			for _, ic := range interceptors {
				if err := ic.InterceptResponse(resp); err != nil {
					return err
				}
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			slog.WarnContext(r.Context(), "proxy: upstream error",
				"tool", toolName, "target", target.String(), "err", err)
			http.Error(w, "proxy: upstream error: "+err.Error(), http.StatusBadGateway)
		},
	}
	rp.ServeHTTP(w, r)
}

// parsePath extracts (toolName, upstreamPath) from /proxy/{tool}/{path...}.
// Returns ("", "") for paths that do not start with /proxy/.
func parsePath(rawPath string) (toolName, upstreamPath string) {
	tail := strings.TrimPrefix(rawPath, "/proxy/")
	if tail == rawPath {
		return "", "" // no /proxy/ prefix
	}
	idx := strings.IndexByte(tail, '/')
	if idx < 0 {
		return tail, "/"
	}
	return tail[:idx], tail[idx:]
}

// buildTargetURL joins the upstream base URL with the upstream path and query.
func buildTargetURL(base *url.URL, path, rawQuery string) *url.URL {
	t := *base
	t.Path = strings.TrimRight(base.Path, "/") + path
	t.RawQuery = rawQuery
	return &t
}

// isLoopback returns true when host (with optional port) resolves to a loopback
// address or is the literal string "localhost".
func isLoopback(host string) bool {
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		h = host
	}
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}
