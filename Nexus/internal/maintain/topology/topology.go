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

// Package topology resolves the local network environment: Docker networks,
// WSL2 bridge, HTTP proxy settings, and port reachability for all known AI tool
// ports. The result is stored in the EnvironmentTwin so the convergence loop and
// proxy layer have a consistent view of the network without re-probing on each use.
package topology

import (
	"context"
	"fmt"
	"time"
)

// DockerNetwork describes one Docker network visible on this host.
type DockerNetwork struct {
	Name   string
	Driver string // "bridge", "host", "overlay", "none"
	Subnet string // CIDR if discoverable, otherwise ""
}

// DockerTopology captures the state of Docker networking on this host.
type DockerTopology struct {
	Available bool            // false when docker is not installed or not running
	Networks  []DockerNetwork // empty when Available is false
}

// WSL2Topology captures the WSL2 bridge adapter state (Windows-only).
// On non-Windows platforms Available is always false.
type WSL2Topology struct {
	Available   bool
	BridgeIP    string   // IP address of the vEthernet (WSL) adapter on the Windows side
	DistroNames []string // running distros (populated when wsl.exe is available)
}

// ProxyConfig captures HTTP proxy settings resolved from environment variables.
// Windows registry proxy settings are not yet reflected here.
type ProxyConfig struct {
	HTTPProxy  string // HTTP_PROXY / http_proxy
	HTTPSProxy string // HTTPS_PROXY / https_proxy
	NoProxy    string // NO_PROXY / no_proxy
}

// PortState records the TCP reachability of one localhost port.
type PortState struct {
	Port      int
	Reachable bool
	LatencyMs int
}

// NetworkTopology is a point-in-time snapshot of the local network environment.
// It is populated by Resolver.Resolve and stored in the EnvironmentTwin via
// SetTopology, replacing the compile-time placeholder defined in W1.
type NetworkTopology struct {
	Docker     *DockerTopology
	WSL2       *WSL2Topology
	Proxy      *ProxyConfig
	Ports      map[int]PortState
	ResolvedAt time.Time
}

// String returns a one-line summary for structured logging.
func (t *NetworkTopology) String() string {
	if t == nil {
		return "NetworkTopology{nil}"
	}
	dockerAvail := t.Docker != nil && t.Docker.Available
	wsl2Avail := t.WSL2 != nil && t.WSL2.Available
	return fmt.Sprintf("NetworkTopology{docker=%v wsl2=%v ports=%d resolved=%s}",
		dockerAvail, wsl2Avail, len(t.Ports), t.ResolvedAt.Format("15:04:05"))
}

// defaultProbePorts lists the well-known ports for AI tools in the connector registry.
var defaultProbePorts = []int{
	11434, // Ollama
	1234,  // LM Studio
	1337,  // Jan
	4891,  // GPT4All
	8000,  // vLLM
	8080,  // LocalAI / Tabby
	3000,  // TGI / Open WebUI
	3001,  // AnythingLLM
	3080,  // LibreChat
	9090,  // OpenCode
}

// Resolver discovers the local network topology. Probe ports can be customised
// before calling Resolve; the zero value is not useful — use NewResolver.
type Resolver struct {
	ProbePorts []int // ports to check via TCP dial; defaults to defaultProbePorts
}

// NewResolver returns a Resolver configured with the default probe-port list.
func NewResolver() *Resolver {
	ports := make([]int, len(defaultProbePorts))
	copy(ports, defaultProbePorts)
	return &Resolver{ProbePorts: ports}
}

// Resolve gathers a full topology snapshot. All sub-probes are best-effort:
// failures (e.g. Docker not installed, WSL2 absent) set the corresponding
// Available=false rather than returning an error. The returned topology is
// always non-nil.
func (r *Resolver) Resolve(ctx context.Context) (*NetworkTopology, error) {
	top := &NetworkTopology{
		Ports:      make(map[int]PortState, len(r.ProbePorts)),
		ResolvedAt: time.Now().UTC(),
	}
	top.Docker = detectDocker(ctx)
	top.WSL2 = detectWSL2()
	top.Proxy = detectProxy()
	for _, port := range r.ProbePorts {
		top.Ports[port] = probePort(ctx, port)
	}
	return top, nil
}
