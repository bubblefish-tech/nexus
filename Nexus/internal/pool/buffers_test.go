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

package pool

import (
	"testing"
)

func TestGetJSONBuf_ReturnsResetBuffer(t *testing.T) {
	t.Helper()
	buf := GetJSONBuf()
	if buf.Len() != 0 {
		t.Errorf("expected reset buffer, got len=%d", buf.Len())
	}
	PutJSONBuf(buf)
}

func TestPutJSONBuf_ReusesBuffer(t *testing.T) {
	t.Helper()
	buf := GetJSONBuf()
	buf.WriteString("test data")
	origCap := buf.Cap()
	PutJSONBuf(buf)

	buf2 := GetJSONBuf()
	if buf2.Cap() < origCap {
		t.Log("buffer may not have been reused (GC collected it)")
	}
	if buf2.Len() != 0 {
		t.Errorf("reused buffer should be reset, got len=%d", buf2.Len())
	}
	PutJSONBuf(buf2)
}

func TestPutJSONBuf_OversizedNotPooled(t *testing.T) {
	t.Helper()
	buf := GetJSONBuf()
	// Grow beyond 1MiB.
	buf.Grow(2 << 20)
	buf.WriteString("x")
	PutJSONBuf(buf) // Should silently discard.
}

func TestGetCopyBuf_Returns32KB(t *testing.T) {
	t.Helper()
	b := GetCopyBuf()
	if len(b) != 32*1024 {
		t.Errorf("copy buf len = %d, want %d", len(b), 32*1024)
	}
	PutCopyBuf(b)
}
