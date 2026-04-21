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
	"net/http"

	"github.com/bubblefish-tech/nexus/internal/version"
)

// Interceptor is the hook interface for modifying proxied requests and responses.
// Both methods must be non-blocking; heavy work belongs in goroutines.
type Interceptor interface {
	// InterceptRequest is called just before the outbound request is sent.
	// Implementations may modify headers, add query parameters, or inject body content.
	// Return a non-nil error to abort the proxy with 500.
	InterceptRequest(req *http.Request) error

	// InterceptResponse is called when the upstream response arrives.
	// Implementations may read or annotate response headers.
	// Return a non-nil error to abort with 502.
	InterceptResponse(resp *http.Response) error
}

// HeaderInterceptor adds identifying Nexus headers to every outbound request
// and the corresponding response. Downstream tools can use these headers to
// confirm they are speaking through the Nexus proxy.
type HeaderInterceptor struct{}

// NewHeaderInterceptor returns a HeaderInterceptor.
func NewHeaderInterceptor() *HeaderInterceptor { return &HeaderInterceptor{} }

// InterceptRequest stamps the outbound request with X-Nexus-Proxy and
// X-Nexus-Version so the upstream can identify Nexus-proxied traffic.
func (h *HeaderInterceptor) InterceptRequest(req *http.Request) error {
	req.Header.Set("X-Nexus-Proxy", "1")
	req.Header.Set("X-Nexus-Version", version.Version)
	return nil
}

// InterceptResponse stamps the response with X-Nexus-Proxy so callers of the
// proxy can verify that the response was routed through Nexus.
func (h *HeaderInterceptor) InterceptResponse(resp *http.Response) error {
	resp.Header.Set("X-Nexus-Proxy", "1")
	return nil
}

// MemoryInterceptor is a stub that will inject Nexus memory context into AI API
// requests once the memory sub-system (W10+) is wired to the proxy. Currently it
// adds a placeholder header so the injection point is visible in traces.
type MemoryInterceptor struct {
	// ContextFn, when non-nil, returns serialised memory context to inject.
	// If nil, the interceptor is a no-op beyond adding the stub header.
	ContextFn func(req *http.Request) (string, error)
}

// NewMemoryInterceptor returns a stub MemoryInterceptor.
func NewMemoryInterceptor() *MemoryInterceptor { return &MemoryInterceptor{} }

// InterceptRequest injects memory context into the X-Nexus-Memory-Context header.
// When ContextFn is nil the header is set to "stub" (visible in traces, harmless).
func (m *MemoryInterceptor) InterceptRequest(req *http.Request) error {
	ctx := "stub"
	if m.ContextFn != nil {
		var err error
		ctx, err = m.ContextFn(req)
		if err != nil {
			return err
		}
	}
	req.Header.Set("X-Nexus-Memory-Context", ctx)
	return nil
}

// InterceptResponse is a no-op for now; future versions will extract assistant
// responses and write them to the memory WAL.
func (m *MemoryInterceptor) InterceptResponse(resp *http.Response) error { return nil }
