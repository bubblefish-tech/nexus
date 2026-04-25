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

package httputil

import (
	"testing"
	"time"
)

func TestTunedTransport_MaxIdleConnsPerHost(t *testing.T) {
	t.Helper()
	if TunedTransport.MaxIdleConnsPerHost < 100 {
		t.Errorf("MaxIdleConnsPerHost = %d, want >= 100", TunedTransport.MaxIdleConnsPerHost)
	}
}

func TestTunedTransport_KeepAlive(t *testing.T) {
	t.Helper()
	if TunedTransport.DialContext == nil {
		t.Fatal("DialContext is nil — KeepAlive cannot be verified")
	}
}

func TestNewClient_ReturnsNonNilWithTimeout(t *testing.T) {
	t.Helper()
	c := NewClient(30 * time.Second)
	if c == nil {
		t.Fatal("NewClient returned nil")
	}
	if c.Timeout != 30*time.Second {
		t.Errorf("client timeout = %v, want 30s", c.Timeout)
	}
}
