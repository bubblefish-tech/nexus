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

package registry

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"

	"golang.org/x/crypto/ed25519"
)

// nexusRegistryPublicKey is the Ed25519 public key used to sign official
// BubbleFish Nexus registry releases. Replace this with the real key before
// shipping; the zero key is intentionally invalid so tests must use test keys.
//
// Key rotation: bump this constant and ship a new binary. Old binaries will
// reject new registry updates until upgraded — acceptable for a local daemon.
var nexusRegistryPublicKey = ed25519.PublicKey(make([]byte, ed25519.PublicKeySize))

// SetRegistryPublicKey replaces the active public key. Call once during init
// from the real embedded key bytes; used by tests to inject a test keypair.
func SetRegistryPublicKey(pub ed25519.PublicKey) {
	nexusRegistryPublicKey = pub
}

// VerifyPayload verifies that data was signed by the BubbleFish registry
// signing key, then checks that the SHA-256 hash of data matches expectedHash.
// Returns an error if either check fails.
func VerifyPayload(data []byte, sig []byte, expectedHash string) error {
	if len(nexusRegistryPublicKey) != ed25519.PublicKeySize {
		return errors.New("registry: public key not initialised")
	}
	if !ed25519.Verify(nexusRegistryPublicKey, data, sig) {
		return errors.New("registry: Ed25519 signature invalid")
	}
	return VerifyHash(data, expectedHash)
}

// VerifyHash returns an error if the SHA-256 of data does not match expectedHash
// (hex-encoded). The comparison is constant-time to prevent oracle attacks.
func VerifyHash(data []byte, expectedHash string) error {
	want, err := hex.DecodeString(expectedHash)
	if err != nil {
		return fmt.Errorf("registry: invalid expected hash %q: %w", expectedHash, err)
	}
	sum := sha256.Sum256(data)
	if subtle.ConstantTimeCompare(sum[:], want) != 1 {
		return fmt.Errorf("registry: SHA-256 mismatch (got %x)", sum)
	}
	return nil
}

// ContentHash returns the hex-encoded SHA-256 of data. Used to generate the
// expectedHash when publishing a signed registry update.
func ContentHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
