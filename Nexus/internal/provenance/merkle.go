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
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// MerkleRoot is the signed daily Merkle root over audit chain entries.
//
// Reference: v0.1.3 Build Plan Phase 4 Subtask 4.8.
type MerkleRoot struct {
	Date       string `json:"date"`        // YYYY-MM-DD
	Root       string `json:"root"`        // hex SHA-256 Merkle root
	LeafCount  int    `json:"leaf_count"`
	DaemonSig  string `json:"daemon_signature"` // Ed25519 over Root
	DaemonKey  string `json:"daemon_pubkey"`
}

// ComputeMerkleRoot builds a balanced binary Merkle tree from the given
// leaf hashes and returns the root hash (hex-encoded SHA-256).
// Empty input returns the hash of an empty byte slice.
func ComputeMerkleRoot(leaves [][]byte) string {
	if len(leaves) == 0 {
		h := sha256.Sum256(nil)
		return hex.EncodeToString(h[:])
	}

	// Hash each leaf.
	hashes := make([][]byte, len(leaves))
	for i, leaf := range leaves {
		h := sha256.Sum256(leaf)
		hashes[i] = h[:]
	}

	// Build tree bottom-up.
	for len(hashes) > 1 {
		var next [][]byte
		for i := 0; i < len(hashes); i += 2 {
			if i+1 < len(hashes) {
				combined := append(hashes[i], hashes[i+1]...)
				h := sha256.Sum256(combined)
				next = append(next, h[:])
			} else {
				// Odd node — promote to next level.
				next = append(next, hashes[i])
			}
		}
		hashes = next
	}

	return hex.EncodeToString(hashes[0])
}

// SignMerkleRoot signs the Merkle root hash with the daemon's Ed25519 key
// and returns the hex-encoded signature.
func SignMerkleRoot(rootHash string, daemonKey ed25519.PrivateKey) string {
	sig := ed25519.Sign(daemonKey, []byte(rootHash))
	return hex.EncodeToString(sig)
}

// BuildDailyMerkleRoot constructs a MerkleRoot from audit entry payloads,
// signs it with the daemon key, and returns the result.
func BuildDailyMerkleRoot(date string, entries [][]byte, daemonKP *KeyPair) *MerkleRoot {
	root := ComputeMerkleRoot(entries)
	sig := SignMerkleRoot(root, daemonKP.PrivateKey)

	return &MerkleRoot{
		Date:      date,
		Root:      root,
		LeafCount: len(entries),
		DaemonSig: sig,
		DaemonKey: hex.EncodeToString(daemonKP.PublicKey),
	}
}

// SaveMerkleRoot writes the Merkle root JSON to data/merkle-roots/<date>.json.
func SaveMerkleRoot(dataDir string, mr *MerkleRoot) error {
	dir := filepath.Join(dataDir, "merkle-roots")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("provenance: create merkle-roots dir: %w", err)
	}

	data, err := json.MarshalIndent(mr, "", "  ")
	if err != nil {
		return fmt.Errorf("provenance: marshal merkle root: %w", err)
	}

	path := filepath.Join(dir, mr.Date+".json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("provenance: write merkle root: %w", err)
	}

	return nil
}

// LoadMerkleRoot reads a daily Merkle root from disk.
func LoadMerkleRoot(dataDir, date string) (*MerkleRoot, error) {
	path := filepath.Join(dataDir, "merkle-roots", date+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var mr MerkleRoot
	if err := json.Unmarshal(data, &mr); err != nil {
		return nil, fmt.Errorf("provenance: unmarshal merkle root: %w", err)
	}
	return &mr, nil
}

// VerifyMerkleRoot checks the daemon signature on a Merkle root.
func VerifyMerkleRoot(mr *MerkleRoot) (bool, error) {
	pubBytes, err := hex.DecodeString(mr.DaemonKey)
	if err != nil {
		return false, fmt.Errorf("provenance: invalid daemon key hex: %w", err)
	}
	if len(pubBytes) != ed25519.PublicKeySize {
		return false, fmt.Errorf("provenance: invalid daemon key size %d", len(pubBytes))
	}

	sigBytes, err := hex.DecodeString(mr.DaemonSig)
	if err != nil {
		return false, fmt.Errorf("provenance: invalid signature hex: %w", err)
	}

	pub := ed25519.PublicKey(pubBytes)
	return ed25519.Verify(pub, []byte(mr.Root), sigBytes), nil
}
