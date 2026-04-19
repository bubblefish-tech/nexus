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
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"hash"
	"io"

	"golang.org/x/crypto/hkdf"
	"golang.org/x/crypto/sha3"
)

// ClassicalProfile implements CryptoProfile using SHA3-256 and AES-256-GCM.
type ClassicalProfile struct{}

func (p *ClassicalProfile) Name() string { return "classical-sha3-256-aes256gcm" }

func (p *ClassicalProfile) HashNew() hash.Hash { return sha3.New256() }

func (p *ClassicalProfile) HMACNew(key []byte) hash.Hash {
	return hmac.New(sha3.New256, key)
}

// HKDFExtract runs HKDF-Extract(salt, secret) → PRK using HMAC-SHA3-256.
// If salt is nil the hash-length zero string is used (per RFC 5869 §2.2).
func (p *ClassicalProfile) HKDFExtract(secret, salt []byte) []byte {
	return hkdf.Extract(sha3.New256, secret, salt)
}

// HKDFExpand runs HKDF-Expand(prk, info, length) using HMAC-SHA3-256.
func (p *ClassicalProfile) HKDFExpand(prk, info []byte, length int) ([]byte, error) {
	r := hkdf.Expand(sha3.New256, prk, info)
	out := make([]byte, length)
	if _, err := io.ReadFull(r, out); err != nil {
		return nil, err
	}
	return out, nil
}

// AEADNew returns an AES-256-GCM cipher.AEAD for the provided 32-byte key.
func (p *ClassicalProfile) AEADNew(key [32]byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// HashSize returns 32 (SHA3-256 produces 32-byte digests).
func (p *ClassicalProfile) HashSize() int { return 32 }
