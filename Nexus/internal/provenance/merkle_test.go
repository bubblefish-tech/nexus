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
	"path/filepath"
	"testing"
)

func TestComputeMerkleRoot_Empty(t *testing.T) {
	root := ComputeMerkleRoot(nil)
	if root == "" {
		t.Error("empty merkle root should not be empty string")
	}
}

func TestComputeMerkleRoot_SingleLeaf(t *testing.T) {
	leaf := []byte("hello")
	root := ComputeMerkleRoot([][]byte{leaf})
	if root == "" {
		t.Error("single-leaf root should not be empty")
	}
}

func TestComputeMerkleRoot_Deterministic(t *testing.T) {
	leaves := [][]byte{
		[]byte("entry-1"),
		[]byte("entry-2"),
		[]byte("entry-3"),
	}
	r1 := ComputeMerkleRoot(leaves)
	r2 := ComputeMerkleRoot(leaves)
	if r1 != r2 {
		t.Errorf("merkle root not deterministic: %q != %q", r1, r2)
	}
}

func TestComputeMerkleRoot_DifferentContent(t *testing.T) {
	leaves1 := [][]byte{[]byte("a"), []byte("b")}
	leaves2 := [][]byte{[]byte("a"), []byte("c")}
	r1 := ComputeMerkleRoot(leaves1)
	r2 := ComputeMerkleRoot(leaves2)
	if r1 == r2 {
		t.Error("different content should produce different roots")
	}
}

func TestComputeMerkleRoot_PowerOfTwo(t *testing.T) {
	leaves := make([][]byte, 8)
	for i := range leaves {
		leaves[i] = []byte{byte(i)}
	}
	root := ComputeMerkleRoot(leaves)
	if len(root) != 64 { // SHA-256 hex
		t.Errorf("root length = %d, want 64", len(root))
	}
}

func TestComputeMerkleRoot_NonPowerOfTwo(t *testing.T) {
	leaves := make([][]byte, 7)
	for i := range leaves {
		leaves[i] = []byte{byte(i)}
	}
	root := ComputeMerkleRoot(leaves)
	if len(root) != 64 {
		t.Errorf("root length = %d, want 64", len(root))
	}
}

func TestBuildDailyMerkleRoot(t *testing.T) {
	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	entries := [][]byte{
		[]byte(`{"seq":1}`),
		[]byte(`{"seq":2}`),
		[]byte(`{"seq":3}`),
	}

	mr := BuildDailyMerkleRoot("2026-04-12", entries, kp)
	if mr.Date != "2026-04-12" {
		t.Errorf("date = %q", mr.Date)
	}
	if mr.LeafCount != 3 {
		t.Errorf("leaf_count = %d, want 3", mr.LeafCount)
	}
	if mr.Root == "" || mr.DaemonSig == "" || mr.DaemonKey == "" {
		t.Error("missing fields in merkle root")
	}

	// Verify signature.
	valid, err := VerifyMerkleRoot(mr)
	if err != nil {
		t.Fatalf("VerifyMerkleRoot: %v", err)
	}
	if !valid {
		t.Error("merkle root signature should be valid")
	}
}

func TestSaveAndLoadMerkleRoot(t *testing.T) {
	kp, _ := GenerateKeyPair()
	entries := [][]byte{[]byte("test")}
	mr := BuildDailyMerkleRoot("2026-04-12", entries, kp)

	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")

	if err := SaveMerkleRoot(dataDir, mr); err != nil {
		t.Fatalf("SaveMerkleRoot: %v", err)
	}

	loaded, err := LoadMerkleRoot(dataDir, "2026-04-12")
	if err != nil {
		t.Fatalf("LoadMerkleRoot: %v", err)
	}
	if loaded.Root != mr.Root {
		t.Errorf("loaded root = %q, want %q", loaded.Root, mr.Root)
	}
	if loaded.DaemonSig != mr.DaemonSig {
		t.Error("signature mismatch after load")
	}
}

func TestVerifyMerkleRoot_TamperedSignature(t *testing.T) {
	kp, _ := GenerateKeyPair()
	mr := BuildDailyMerkleRoot("2026-04-12", [][]byte{[]byte("x")}, kp)

	// Tamper with signature.
	mr.DaemonSig = "deadbeef" + mr.DaemonSig[8:]

	valid, err := VerifyMerkleRoot(mr)
	if err != nil {
		t.Fatal(err)
	}
	if valid {
		t.Error("tampered signature should fail")
	}
}
