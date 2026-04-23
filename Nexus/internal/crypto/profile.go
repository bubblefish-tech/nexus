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

// Package crypto provides the CryptoProfile abstraction used throughout Nexus.
// Community edition uses ClassicalProfile (SHA3-256 + AES-256-GCM).
// Enterprise and TS editions can swap in alternative profiles at init time.
package crypto

import (
	"crypto/cipher"
	"hash"
)

// CryptoProfile is the seam that lets Enterprise/TS swap the underlying
// cryptographic primitives without touching call sites.
type CryptoProfile interface {
	// Name returns a short identifier for logging and diagnostics.
	Name() string

	// HashNew returns a fresh hash.Hash (SHA3-256 in ClassicalProfile).
	HashNew() hash.Hash

	// HMACNew returns a fresh HMAC keyed with the provided key.
	HMACNew(key []byte) hash.Hash

	// HKDFExtract runs the HKDF-Extract step and returns the pseudorandom key.
	HKDFExtract(secret, salt []byte) []byte

	// HKDFExpand runs the HKDF-Expand step and returns length bytes of key material.
	HKDFExpand(prk, info []byte, length int) ([]byte, error)

	// AEADNew returns an AES-256-GCM cipher.AEAD keyed with the provided 32-byte key.
	AEADNew(key [32]byte) (cipher.AEAD, error)

	// HashSize returns the output size of HashNew() in bytes.
	HashSize() int
}

// ActiveProfile is the profile used by all Nexus components.
// Defaults to ClassicalProfile; overwritten by Enterprise/TS init.
var ActiveProfile CryptoProfile = &ClassicalProfile{}
