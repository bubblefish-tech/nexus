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

package substrate

import (
	"crypto/hkdf"
	"crypto/sha256"
	"errors"
)

// Domain separation constants for HKDF-SHA-256 key derivation.
// Every context where a key is derived has a unique info string to prevent
// cross-context key reuse even if the ikm and salt collide.
//
// These strings are part of the on-disk format. Changing them is a
// wire-format break and requires a version bump.
const (
	// domainEmbeddingKeyV1 derives per-memory embedding encryption keys.
	domainEmbeddingKeyV1 = "nexus-embedding-key-v1"
)

// DeriveEmbeddingKey derives a 32-byte AES-256 key from the current ratchet
// state, a memory ID, and the embedding domain separation constant.
//
// Construction:
//
//	key = HKDF-SHA-256(
//	    secret = stateBytes,
//	    salt   = memoryID bytes,
//	    info   = "nexus-embedding-key-v1",
//	    L      = 32,
//	)
//
// Per RFC 5869, HKDF is secure as a PRF when the secret has at least one
// block of entropy. Our stateBytes is 32 bytes from an HMAC-SHA-256 chain
// seeded with crypto/rand, which satisfies this requirement.
//
// The memory ID as salt provides per-memory key separation: two memories
// encrypted under the same ratchet state get different keys because their
// salts differ.
func DeriveEmbeddingKey(stateBytes [32]byte, memoryID string) ([32]byte, error) {
	var key [32]byte
	if memoryID == "" {
		return key, errors.New("derive embedding key: empty memory ID")
	}

	derived, err := hkdf.Key(sha256.New, stateBytes[:], []byte(memoryID), domainEmbeddingKeyV1, 32)
	if err != nil {
		return key, err
	}
	copy(key[:], derived)
	return key, nil
}

// ZeroizeKey overwrites a key buffer with zeros. Best-effort: Go does not
// guarantee that the compiler will not retain a copy of the key in a
// register or a temporary. This is defense in depth, not a hard guarantee.
func ZeroizeKey(key *[32]byte) {
	for i := range key {
		key[i] = 0
	}
}
