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
	"fmt"
)

// MerkleProof is a list of sibling hashes from leaf to root that proves
// inclusion of a leaf in a Merkle tree.
//
// Reference: Tech Spec MT.13 — Merkle Inclusion Proofs in Queries.
type MerkleProof struct {
	// LeafHash is the hash of the leaf being proved.
	LeafHash []byte `json:"leaf_hash"`

	// Siblings is the ordered list of sibling hashes from leaf to root.
	// Each entry is a raw hash (ActiveProfile.HashSize() bytes).
	Siblings [][]byte `json:"siblings"`

	// LeafIndex is the zero-based index of the leaf in the original set.
	LeafIndex int `json:"leaf_index"`

	// TreeSize is the total number of leaves in the tree.
	TreeSize int `json:"tree_size"`
}

// Errors returned by Merkle proof operations.
var (
	ErrMerkleNoLeaves    = errors.New("crypto: merkle: no leaves provided")
	ErrMerkleIndexOOB    = errors.New("crypto: merkle: leaf index out of bounds")
	ErrMerkleProofFailed = errors.New("crypto: merkle: proof verification failed")
)

// GenerateProof builds a Merkle inclusion proof for the leaf at the given
// index from the provided set of leaf data. Each leaf is hashed using the
// active crypto profile (SHA3-256 by default).
//
// Returns the proof containing sibling hashes from leaf to root.
func GenerateProof(leaves [][]byte, index int) (*MerkleProof, error) {
	if len(leaves) == 0 {
		return nil, ErrMerkleNoLeaves
	}
	if index < 0 || index >= len(leaves) {
		return nil, ErrMerkleIndexOOB
	}

	// Hash all leaves.
	hashes := make([][]byte, len(leaves))
	for i, leaf := range leaves {
		h := ActiveProfile.HashNew()
		h.Write(leaf)
		hashes[i] = h.Sum(nil)
	}

	leafHash := make([]byte, len(hashes[index]))
	copy(leafHash, hashes[index])

	siblings := merkleProofForLeaf(hashes, index)

	return &MerkleProof{
		LeafHash:  leafHash,
		Siblings:  siblings,
		LeafIndex: index,
		TreeSize:  len(leaves),
	}, nil
}

// GenerateProofFromHashes builds a Merkle inclusion proof from pre-computed
// leaf hashes. Useful when the caller has already hashed the data.
func GenerateProofFromHashes(hashes [][]byte, index int) (*MerkleProof, error) {
	if len(hashes) == 0 {
		return nil, ErrMerkleNoLeaves
	}
	if index < 0 || index >= len(hashes) {
		return nil, ErrMerkleIndexOOB
	}

	leafHash := make([]byte, len(hashes[index]))
	copy(leafHash, hashes[index])

	siblings := merkleProofForLeaf(hashes, index)

	return &MerkleProof{
		LeafHash:  leafHash,
		Siblings:  siblings,
		LeafIndex: index,
		TreeSize:  len(hashes),
	}, nil
}

// VerifyProof verifies a Merkle inclusion proof against the expected root hash.
// Returns nil if the proof is valid.
func VerifyProof(proof *MerkleProof, expectedRoot []byte) error {
	if proof == nil {
		return ErrMerkleProofFailed
	}

	// For a single-leaf tree, the leaf hash IS the root.
	if proof.TreeSize == 1 {
		if bytes.Equal(proof.LeafHash, expectedRoot) {
			return nil
		}
		return ErrMerkleProofFailed
	}

	current := proof.LeafHash
	idx := proof.LeafIndex

	for _, sibling := range proof.Siblings {
		if idx%2 == 0 {
			current = hashPair(current, sibling)
		} else {
			current = hashPair(sibling, current)
		}
		idx /= 2
	}

	if !bytes.Equal(current, expectedRoot) {
		return fmt.Errorf("%w: computed root does not match expected", ErrMerkleProofFailed)
	}
	return nil
}

// ComputeMerkleRoot computes the Merkle root hash from a set of leaf data.
// Each leaf is hashed using the active crypto profile.
func ComputeMerkleRoot(leaves [][]byte) ([]byte, error) {
	if len(leaves) == 0 {
		return nil, ErrMerkleNoLeaves
	}

	hashes := make([][]byte, len(leaves))
	for i, leaf := range leaves {
		h := ActiveProfile.HashNew()
		h.Write(leaf)
		hashes[i] = h.Sum(nil)
	}

	return merkleRoot(hashes), nil
}

// ComputeMerkleRootFromHashes computes the Merkle root from pre-computed
// leaf hashes.
func ComputeMerkleRootFromHashes(hashes [][]byte) ([]byte, error) {
	if len(hashes) == 0 {
		return nil, ErrMerkleNoLeaves
	}
	// Copy to avoid mutating the caller's slice.
	cp := make([][]byte, len(hashes))
	copy(cp, hashes)
	return merkleRoot(cp), nil
}
