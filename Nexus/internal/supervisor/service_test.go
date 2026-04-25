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

package supervisor

import (
	"testing"
)

// ── stringBuffer Tests ─────────────────────────────────────────────────────

func TestStringBuffer_Write(t *testing.T) {
	var buf stringBuffer
	n, err := buf.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != 5 {
		t.Errorf("want 5 bytes, got %d", n)
	}
	if buf.String() != "hello" {
		t.Errorf("want 'hello', got %q", buf.String())
	}
}

func TestStringBuffer_MultipleWrites(t *testing.T) {
	var buf stringBuffer
	_, _ = buf.Write([]byte("hello "))
	_, _ = buf.Write([]byte("world"))
	if buf.String() != "hello world" {
		t.Errorf("want 'hello world', got %q", buf.String())
	}
}
