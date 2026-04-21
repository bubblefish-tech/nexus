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

// Package fingerprint identifies the API protocol spoken by a running AI tool
// by sending lightweight probes and pattern-matching the responses. No model
// inference is required — only API shape detection (field presence, status codes).
//
// Probe ordering matters: tool-specific endpoints are checked before the generic
// OpenAI-compat endpoint so that Ollama (which also serves /v1/models) is
// correctly identified as OllamaNative rather than OpenAICompat.
package fingerprint

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Protocol identifies an AI tool's API wire format.
type Protocol string

const (
	ProtocolOpenAICompat Protocol = "openai_compat"  // OpenAI /v1/* API shape
	ProtocolOllamaNative Protocol = "ollama_native"  // Ollama /api/* endpoints
	ProtocolTGI          Protocol = "tgi"            // HuggingFace Text Generation Inference
	ProtocolKoboldCpp    Protocol = "koboldcpp"      // KoboldCpp /api/v1/info
	ProtocolTabby        Protocol = "tabby"          // Tabby ML /v1/health
	ProtocolUnknown      Protocol = "unknown"        // no probe matched
)

// Evidence records one probe attempt and its outcome.
type Evidence struct {
	ProbeName  string
	Path       string
	StatusCode int
	LatencyMs  int
	Matched    bool
}

// Fingerprint is the complete result of probing one endpoint.
// When Confirmed is false the Protocol is ProtocolUnknown and no probe matched.
type Fingerprint struct {
	Endpoint  string
	Protocol  Protocol
	Confirmed bool       // true when at least one probe matched
	Evidence  []Evidence // one entry per probe that was attempted
}

// String returns a short human-readable description.
func (f Fingerprint) String() string {
	return fmt.Sprintf("Fingerprint{endpoint=%s protocol=%s confirmed=%v evidence=%d}",
		f.Endpoint, f.Protocol, f.Confirmed, len(f.Evidence))
}

// Probe is one test: a path to GET and a matcher that decides whether the
// response body identifies a specific protocol.
type Probe struct {
	Name  string
	Path  string
	Proto Protocol
	// Match receives the HTTP status code and the response body (capped at 64 KiB).
	// It returns true when the response positively identifies Proto.
	Match func(status int, body []byte) bool
}

const (
	probeTimeout  = 3 * time.Second
	maxBodyBytes  = 64 * 1024
)

// Prober runs a prioritised list of Probes against an AI tool endpoint.
// Probes are executed in order; the first match short-circuits the remaining probes.
type Prober struct {
	client *http.Client
	probes []Probe
}

// NewProber returns a Prober loaded with the default probe set (see probes.go).
func NewProber() *Prober {
	return &Prober{
		client: &http.Client{Timeout: probeTimeout},
		probes: defaultProbes(),
	}
}

// NewProberWithProbes returns a Prober using a custom probe set (for testing).
func NewProberWithProbes(probes []Probe) *Prober {
	return &Prober{
		client: &http.Client{Timeout: probeTimeout},
		probes: probes,
	}
}

// Fingerprint probes baseURL and returns the identified protocol.
// Probes are run in priority order; the first match wins.
// ctx cancellation causes the current probe to abort; remaining probes are skipped
// and ProtocolUnknown is returned.
func (p *Prober) Fingerprint(ctx context.Context, baseURL string) Fingerprint {
	base := strings.TrimRight(baseURL, "/")
	fp := Fingerprint{
		Endpoint: base,
		Protocol: ProtocolUnknown,
	}

	for _, probe := range p.probes {
		ev, matched := p.runProbe(ctx, base, probe)
		fp.Evidence = append(fp.Evidence, ev)
		if matched {
			fp.Protocol = probe.Proto
			fp.Confirmed = true
			break
		}
		// If context is cancelled, stop early.
		if ctx.Err() != nil {
			break
		}
	}
	return fp
}

// runProbe executes one probe and returns (Evidence, matched).
func (p *Prober) runProbe(ctx context.Context, base string, probe Probe) (Evidence, bool) {
	url := base + probe.Path
	start := time.Now()
	ev := Evidence{
		ProbeName: probe.Name,
		Path:      probe.Path,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ev, false
	}
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	ev.LatencyMs = int(time.Since(start).Milliseconds())
	if err != nil {
		return ev, false
	}
	defer resp.Body.Close()

	ev.StatusCode = resp.StatusCode
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))

	matched := probe.Match(resp.StatusCode, body)
	ev.Matched = matched
	return ev, matched
}
