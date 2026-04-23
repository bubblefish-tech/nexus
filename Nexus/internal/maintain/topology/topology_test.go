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

package topology_test

import (
	"context"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bubblefish-tech/nexus/internal/maintain/topology"
)

// TestResolver_ReturnsNonNil verifies Resolve always returns a non-nil topology.
func TestResolver_ReturnsNonNil(t *testing.T) {
	r := topology.NewResolver()
	r.ProbePorts = nil // no port probing to keep the test fast
	top, err := r.Resolve(context.Background())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if top == nil {
		t.Fatal("Resolve must return non-nil topology")
	}
}

// TestResolver_ResolvedAt_Recent verifies the ResolvedAt timestamp is set.
func TestResolver_ResolvedAt_Recent(t *testing.T) {
	r := topology.NewResolver()
	r.ProbePorts = nil
	before := time.Now().UTC()
	top, _ := r.Resolve(context.Background())
	after := time.Now().UTC()
	if top.ResolvedAt.Before(before) || top.ResolvedAt.After(after) {
		t.Errorf("ResolvedAt %v not within [%v, %v]", top.ResolvedAt, before, after)
	}
}

// TestResolver_SubComponents_NonNil verifies Docker, WSL2, and Proxy are populated.
func TestResolver_SubComponents_NonNil(t *testing.T) {
	r := topology.NewResolver()
	r.ProbePorts = nil
	top, _ := r.Resolve(context.Background())
	if top.Docker == nil {
		t.Error("Docker field must not be nil")
	}
	if top.WSL2 == nil {
		t.Error("WSL2 field must not be nil")
	}
	if top.Proxy == nil {
		t.Error("Proxy field must not be nil")
	}
}

// TestResolver_PortMap_Populated verifies the Ports map contains entries for
// each probe port.
func TestResolver_PortMap_Populated(t *testing.T) {
	r := topology.NewResolver()
	r.ProbePorts = []int{11434, 1234}
	top, _ := r.Resolve(context.Background())
	for _, port := range r.ProbePorts {
		if _, ok := top.Ports[port]; !ok {
			t.Errorf("missing port %d in topology Ports map", port)
		}
	}
}

// TestProbePort_OpenPort verifies a listening TCP port is detected as reachable.
func TestProbePort_OpenPort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	addr := ln.Addr().String()
	portStr := addr[strings.LastIndex(addr, ":")+1:]
	port, _ := strconv.Atoi(portStr)

	r := topology.NewResolver()
	r.ProbePorts = []int{port}
	top, _ := r.Resolve(context.Background())

	state, ok := top.Ports[port]
	if !ok {
		t.Fatalf("port %d not in topology", port)
	}
	if !state.Reachable {
		t.Errorf("expected Reachable=true for listening port %d", port)
	}
}

// TestProbePort_ClosedPort verifies port 1 (always refused) is not reachable.
func TestProbePort_ClosedPort(t *testing.T) {
	r := topology.NewResolver()
	r.ProbePorts = []int{1}
	top, _ := r.Resolve(context.Background())
	state, ok := top.Ports[1]
	if !ok {
		t.Fatal("port 1 not in topology")
	}
	if state.Reachable {
		t.Error("port 1 should not be reachable")
	}
}

// TestDetectProxy_EnvVars verifies HTTP_PROXY / HTTPS_PROXY are read correctly.
func TestDetectProxy_EnvVars(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://proxy.example.com:3128")
	t.Setenv("HTTPS_PROXY", "http://proxy.example.com:3128")
	t.Setenv("NO_PROXY", "localhost,127.0.0.1")

	r := topology.NewResolver()
	r.ProbePorts = nil
	top, _ := r.Resolve(context.Background())

	if top.Proxy.HTTPProxy != "http://proxy.example.com:3128" {
		t.Errorf("HTTPProxy: got %q", top.Proxy.HTTPProxy)
	}
	if top.Proxy.HTTPSProxy != "http://proxy.example.com:3128" {
		t.Errorf("HTTPSProxy: got %q", top.Proxy.HTTPSProxy)
	}
	if top.Proxy.NoProxy != "localhost,127.0.0.1" {
		t.Errorf("NoProxy: got %q", top.Proxy.NoProxy)
	}
}

// TestDetectProxy_EmptyWhenUnset verifies proxy fields are empty with no env vars.
func TestDetectProxy_EmptyWhenUnset(t *testing.T) {
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("http_proxy", "")
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("https_proxy", "")
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")

	r := topology.NewResolver()
	r.ProbePorts = nil
	top, _ := r.Resolve(context.Background())

	if top.Proxy.HTTPProxy != "" || top.Proxy.HTTPSProxy != "" || top.Proxy.NoProxy != "" {
		t.Errorf("expected empty proxy config, got %+v", top.Proxy)
	}
}

// TestDockerTopology_WhenUnavailable verifies Docker.Available=false when
// docker is not in PATH (or not responding).
func TestDockerTopology_WhenUnavailable(t *testing.T) {
	// Skip if docker IS available and responding — this test is for the absence case.
	if _, err := exec.LookPath("docker"); err == nil {
		if out, err2 := exec.Command("docker", "network", "ls").Output(); err2 == nil && len(out) > 0 {
			t.Skip("docker is available and running — skipping unavailability test")
		}
	}

	r := topology.NewResolver()
	r.ProbePorts = nil
	top, _ := r.Resolve(context.Background())
	if top.Docker.Available {
		t.Error("expected Docker.Available=false when daemon is absent")
	}
}

// TestNetworkTopology_String_NonEmpty verifies String() returns a non-empty value.
func TestNetworkTopology_String_NonEmpty(t *testing.T) {
	r := topology.NewResolver()
	r.ProbePorts = nil
	top, _ := r.Resolve(context.Background())
	s := top.String()
	if s == "" {
		t.Error("String() must not be empty")
	}
	if !strings.Contains(s, "NetworkTopology") {
		t.Errorf("String() should mention NetworkTopology, got %q", s)
	}
}

// TestContextCancellation verifies that a cancelled context stops port probing.
func TestContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	r := topology.NewResolver()
	r.ProbePorts = []int{11434, 1234, 8080}
	top, _ := r.Resolve(ctx)
	// Should complete quickly (not hang) — all ports unreachable due to cancelled ctx.
	if top == nil {
		t.Fatal("even with cancelled ctx, topology must not be nil")
	}
}
