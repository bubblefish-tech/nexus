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

package crypto

import (
	"bytes"
	"errors"
	"testing"
)

func TestGenerateProof_SingleLeaf(t *testing.T) {
	leaves := [][]byte{[]byte("only-leaf")}
	proof, err := GenerateProof(leaves, 0)
	if err != nil {
		t.Fatalf("GenerateProof: %v", err)
	}
	if proof.LeafIndex != 0 {
		t.Errorf("leaf_index = %d, want 0", proof.LeafIndex)
	}
	if proof.TreeSize != 1 {
		t.Errorf("tree_size = %d, want 1", proof.TreeSize)
	}
	if len(proof.Siblings) != 0 {
		t.Errorf("siblings length = %d, want 0", len(proof.Siblings))
	}

	root, err := ComputeMerkleRoot(leaves)
	if err != nil {
		t.Fatalf("ComputeMerkleRoot: %v", err)
	}
	if err := VerifyProof(proof, root); err != nil {
		t.Fatalf("VerifyProof: %v", err)
	}
}

func TestGenerateProof_TwoLeaves(t *testing.T) {
	leaves := [][]byte{[]byte("leaf-0"), []byte("leaf-1")}
	root, err := ComputeMerkleRoot(leaves)
	if err != nil {
		t.Fatalf("ComputeMerkleRoot: %v", err)
	}

	for i := range leaves {
		proof, err := GenerateProof(leaves, i)
		if err != nil {
			t.Fatalf("GenerateProof(%d): %v", i, err)
		}
		if len(proof.Siblings) != 1 {
			t.Errorf("proof[%d].siblings = %d, want 1", i, len(proof.Siblings))
		}
		if err := VerifyProof(proof, root); err != nil {
			t.Fatalf("VerifyProof(%d): %v", i, err)
		}
	}
}

func TestGenerateProof_PowerOfTwo(t *testing.T) {
	leaves := make([][]byte, 8)
	for i := range leaves {
		leaves[i] = []byte{byte(i), byte(i + 1)}
	}

	root, err := ComputeMerkleRoot(leaves)
	if err != nil {
		t.Fatalf("ComputeMerkleRoot: %v", err)
	}

	for i := range leaves {
		proof, err := GenerateProof(leaves, i)
		if err != nil {
			t.Fatalf("GenerateProof(%d): %v", i, err)
		}
		if err := VerifyProof(proof, root); err != nil {
			t.Fatalf("VerifyProof(%d): %v", i, err)
		}
	}
}

func TestGenerateProof_OddLeafCount(t *testing.T) {
	leaves := make([][]byte, 5)
	for i := range leaves {
		leaves[i] = []byte{byte(i * 3)}
	}

	root, err := ComputeMerkleRoot(leaves)
	if err != nil {
		t.Fatalf("ComputeMerkleRoot: %v", err)
	}

	for i := range leaves {
		proof, err := GenerateProof(leaves, i)
		if err != nil {
			t.Fatalf("GenerateProof(%d): %v", i, err)
		}
		if err := VerifyProof(proof, root); err != nil {
			t.Fatalf("VerifyProof(%d): %v", i, err)
		}
	}
}

func TestGenerateProof_LargeTree(t *testing.T) {
	leaves := make([][]byte, 100)
	for i := range leaves {
		leaves[i] = []byte("leaf-content-" + string(rune('A'+i%26)))
	}

	root, err := ComputeMerkleRoot(leaves)
	if err != nil {
		t.Fatalf("ComputeMerkleRoot: %v", err)
	}

	// Verify a sample of proofs.
	for _, i := range []int{0, 1, 49, 50, 98, 99} {
		proof, err := GenerateProof(leaves, i)
		if err != nil {
			t.Fatalf("GenerateProof(%d): %v", i, err)
		}
		if err := VerifyProof(proof, root); err != nil {
			t.Fatalf("VerifyProof(%d): %v", i, err)
		}
	}
}

func TestGenerateProof_EmptyLeaves(t *testing.T) {
	_, err := GenerateProof(nil, 0)
	if !errors.Is(err, ErrMerkleNoLeaves) {
		t.Fatalf("expected ErrMerkleNoLeaves, got %v", err)
	}
}

func TestGenerateProof_IndexOutOfBounds(t *testing.T) {
	leaves := [][]byte{[]byte("a"), []byte("b")}

	_, err := GenerateProof(leaves, 2)
	if !errors.Is(err, ErrMerkleIndexOOB) {
		t.Fatalf("expected ErrMerkleIndexOOB for i=2, got %v", err)
	}

	_, err = GenerateProof(leaves, -1)
	if !errors.Is(err, ErrMerkleIndexOOB) {
		t.Fatalf("expected ErrMerkleIndexOOB for i=-1, got %v", err)
	}
}

func TestVerifyProof_WrongRoot(t *testing.T) {
	leaves := [][]byte{[]byte("a"), []byte("b")}
	proof, err := GenerateProof(leaves, 0)
	if err != nil {
		t.Fatalf("GenerateProof: %v", err)
	}

	wrongRoot := make([]byte, ActiveProfile.HashSize())
	wrongRoot[0] = 0xff
	if err := VerifyProof(proof, wrongRoot); !errors.Is(err, ErrMerkleProofFailed) {
		t.Fatalf("expected ErrMerkleProofFailed, got %v", err)
	}
}

func TestVerifyProof_NilProof(t *testing.T) {
	if err := VerifyProof(nil, []byte{0x01}); !errors.Is(err, ErrMerkleProofFailed) {
		t.Fatalf("expected ErrMerkleProofFailed for nil proof, got %v", err)
	}
}

func TestGenerateProofFromHashes(t *testing.T) {
	leaves := [][]byte{[]byte("x"), []byte("y"), []byte("z")}
	hashes := make([][]byte, len(leaves))
	for i, l := range leaves {
		h := ActiveProfile.HashNew()
		h.Write(l)
		hashes[i] = h.Sum(nil)
	}

	root, err := ComputeMerkleRootFromHashes(hashes)
	if err != nil {
		t.Fatalf("ComputeMerkleRootFromHashes: %v", err)
	}

	proof, err := GenerateProofFromHashes(hashes, 1)
	if err != nil {
		t.Fatalf("GenerateProofFromHashes: %v", err)
	}
	if err := VerifyProof(proof, root); err != nil {
		t.Fatalf("VerifyProof: %v", err)
	}
}

func TestComputeMerkleRoot_Deterministic(t *testing.T) {
	leaves := [][]byte{[]byte("a"), []byte("b"), []byte("c")}
	root1, err := ComputeMerkleRoot(leaves)
	if err != nil {
		t.Fatalf("ComputeMerkleRoot (1): %v", err)
	}
	root2, err := ComputeMerkleRoot(leaves)
	if err != nil {
		t.Fatalf("ComputeMerkleRoot (2): %v", err)
	}
	if !bytes.Equal(root1, root2) {
		t.Error("ComputeMerkleRoot is not deterministic")
	}
}

func TestComputeMerkleRoot_EmptyLeaves(t *testing.T) {
	_, err := ComputeMerkleRoot(nil)
	if !errors.Is(err, ErrMerkleNoLeaves) {
		t.Fatalf("expected ErrMerkleNoLeaves, got %v", err)
	}
}

func TestComputeMerkleRootFromHashes_EmptyHashes(t *testing.T) {
	_, err := ComputeMerkleRootFromHashes(nil)
	if !errors.Is(err, ErrMerkleNoLeaves) {
		t.Fatalf("expected ErrMerkleNoLeaves, got %v", err)
	}
}

func TestGenerateProofFromHashes_EmptyHashes(t *testing.T) {
	_, err := GenerateProofFromHashes(nil, 0)
	if !errors.Is(err, ErrMerkleNoLeaves) {
		t.Fatalf("expected ErrMerkleNoLeaves, got %v", err)
	}
}

func TestGenerateProofFromHashes_IndexOOB(t *testing.T) {
	hashes := [][]byte{{0x01}}
	_, err := GenerateProofFromHashes(hashes, 5)
	if !errors.Is(err, ErrMerkleIndexOOB) {
		t.Fatalf("expected ErrMerkleIndexOOB, got %v", err)
	}
}
