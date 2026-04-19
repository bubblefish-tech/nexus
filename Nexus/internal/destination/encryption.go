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

package destination

import (
	cryptorand "crypto/rand"
	"fmt"

	nexuscrypto "github.com/bubblefish-tech/nexus/internal/crypto"
)

// derivePerRowKey derives a 32-byte AES-256-GCM key for a single memory row.
// key = HKDF-Expand(HKDF-Extract(subKey, payloadID), "memory-content", 32)
// The payloadID is used as the HKDF salt, binding the key to this specific row.
func derivePerRowKey(subKey [32]byte, payloadID string) ([32]byte, error) {
	prk := nexuscrypto.ActiveProfile.HKDFExtract(subKey[:], []byte(payloadID))
	expanded, err := nexuscrypto.ActiveProfile.HKDFExpand(prk, []byte("memory-content"), 32)
	if err != nil {
		return [32]byte{}, fmt.Errorf("destination: derive per-row key: %w", err)
	}
	var key [32]byte
	copy(key[:], expanded)
	return key, nil
}

// sealAES256GCM encrypts plaintext under key with the given AAD using
// AES-256-GCM. The returned blob is: nonce (12 bytes) || ciphertext || tag.
// A fresh random nonce is generated for each call.
func sealAES256GCM(key [32]byte, plaintext, aad []byte) ([]byte, error) {
	aead, err := nexuscrypto.ActiveProfile.AEADNew(key)
	if err != nil {
		return nil, fmt.Errorf("destination: create AEAD: %w", err)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := cryptorand.Read(nonce); err != nil {
		return nil, fmt.Errorf("destination: generate nonce: %w", err)
	}
	// Seal appends ciphertext+tag to nonce, producing nonce||ciphertext||tag.
	return aead.Seal(nonce, nonce, plaintext, aad), nil
}

// openAES256GCM decrypts a blob produced by sealAES256GCM. Returns the
// plaintext or an error if authentication fails or the blob is malformed.
func openAES256GCM(key [32]byte, blob, aad []byte) ([]byte, error) {
	aead, err := nexuscrypto.ActiveProfile.AEADNew(key)
	if err != nil {
		return nil, fmt.Errorf("destination: create AEAD: %w", err)
	}
	ns := aead.NonceSize()
	if len(blob) < ns {
		return nil, fmt.Errorf("destination: ciphertext too short (%d bytes, need >%d)", len(blob), ns)
	}
	plain, err := aead.Open(nil, blob[:ns], blob[ns:], aad)
	if err != nil {
		return nil, fmt.Errorf("destination: decrypt: %w", err)
	}
	return plain, nil
}
