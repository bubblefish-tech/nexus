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

package audit

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"fmt"
)

// Encryption format constants. Reference: Update U1.2.
const (
	encryptionVersion byte   = 1
	encryptionKeyID   uint32 = 0x00000001
	nonceSize                = 12 // AES-256-GCM nonce size
)

// encryptRecord encrypts plaintext using AES-256-GCM with a fresh 12-byte nonce.
// Returns: version(1) + key_id(4) + nonce(12) + ciphertext(variable).
// The encryption key MUST be 32 bytes (AES-256). This key is SEPARATE from the WAL key.
//
// Reference: Update U1.2.
func encryptRecord(plaintext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("audit: aes.NewCipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("audit: cipher.NewGCM: %w", err)
	}

	nonce := make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("audit: generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)

	// Build: version(1) + key_id(4) + nonce(12) + ciphertext
	header := make([]byte, 1+4+nonceSize)
	header[0] = encryptionVersion
	binary.BigEndian.PutUint32(header[1:5], encryptionKeyID)
	copy(header[5:], nonce)

	result := make([]byte, 0, len(header)+len(ciphertext))
	result = append(result, header...)
	result = append(result, ciphertext...)

	return result, nil
}

// decryptRecord decrypts data produced by encryptRecord.
// The encryption key MUST be the same 32-byte key used during encryption.
// This key is SEPARATE from the WAL key.
//
// Reference: Update U1.2.
func decryptRecord(data, key []byte) ([]byte, error) {
	minLen := 1 + 4 + nonceSize + 1 // version + key_id + nonce + at least 1 byte ciphertext
	if len(data) < minLen {
		return nil, fmt.Errorf("audit: encrypted data too short (%d bytes)", len(data))
	}

	version := data[0]
	if version != encryptionVersion {
		return nil, fmt.Errorf("audit: unsupported encryption version %d", version)
	}

	nonce := data[5 : 5+nonceSize]
	ciphertext := data[5+nonceSize:]

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("audit: aes.NewCipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("audit: cipher.NewGCM: %w", err)
	}

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("audit: gcm.Open: %w", err)
	}

	return plaintext, nil
}
