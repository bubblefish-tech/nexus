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

package topology

import (
	"context"
	"fmt"
	"net"
	"time"
)

const portProbeTimeout = 500 * time.Millisecond

// probePort attempts a TCP connection to localhost:port and returns reachability.
// Uses context for cancellation; a cancelled context returns Reachable=false.
func probePort(ctx context.Context, port int) PortState {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	start := time.Now()
	dialer := &net.Dialer{Timeout: portProbeTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	latencyMs := int(time.Since(start).Milliseconds())
	if err != nil {
		return PortState{Port: port, Reachable: false, LatencyMs: latencyMs}
	}
	conn.Close()
	return PortState{Port: port, Reachable: true, LatencyMs: latencyMs}
}
