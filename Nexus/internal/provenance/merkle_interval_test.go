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

package provenance

import (
	"testing"
)

func TestChainState_EntryCountIncrements(t *testing.T) {
	t.Helper()
	cs := NewChainState()
	if cs.EntryCount() != 0 {
		t.Fatalf("expected 0 initial entries, got %d", cs.EntryCount())
	}
	cs.Extend([]byte("hash1"))
	if cs.EntryCount() != 1 {
		t.Fatalf("expected 1 entry after Extend, got %d", cs.EntryCount())
	}
	cs.Extend([]byte("hash2"))
	if cs.EntryCount() != 2 {
		t.Fatalf("expected 2 entries after second Extend, got %d", cs.EntryCount())
	}
}

func TestChainState_EntryCountThreshold(t *testing.T) {
	t.Helper()
	cs := NewChainState()
	threshold := int64(5)
	for i := int64(0); i < threshold; i++ {
		cs.Extend([]byte("entry"))
	}
	if cs.EntryCount() != threshold {
		t.Fatalf("expected %d entries, got %d", threshold, cs.EntryCount())
	}
}

func TestComputeMerkleRoot_DeterministicRepeated(t *testing.T) {
	t.Helper()
	leaves := [][]byte{[]byte("a"), []byte("b"), []byte("c")}
	r1 := ComputeMerkleRoot(leaves)
	r2 := ComputeMerkleRoot(leaves)
	if r1 != r2 {
		t.Fatal("expected deterministic root for same leaves")
	}
}
