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
	cryptorand "crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
)

// EncryptedPrefix is the fixed header that marks an encrypted config field value.
// Format: "ENC:v1:<base64(nonce||ciphertext||tag)>"
const EncryptedPrefix = "ENC:v1:"

// EncryptField encrypts plaintext using AES-256-GCM with the provided key.
// Returns a string of the form "ENC:v1:<base64(nonce||ciphertext||tag)>".
// If plaintext already has the ENC:v1: prefix it is returned unchanged.
func EncryptField(plaintext string, key [32]byte) (string, error) {
	if IsEncrypted(plaintext) {
		return plaintext, nil
	}
	aead, err := ActiveProfile.AEADNew(key)
	if err != nil {
		return "", fmt.Errorf("configcrypt: create AEAD: %w", err)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := cryptorand.Read(nonce); err != nil {
		return "", fmt.Errorf("configcrypt: generate nonce: %w", err)
	}
	// Seal appends ciphertext+tag to nonce, producing nonce||ciphertext||tag.
	blob := aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return EncryptedPrefix + base64.StdEncoding.EncodeToString(blob), nil
}

// DecryptField decrypts a value produced by EncryptField. If s does not have
// the ENC:v1: prefix it is returned unchanged (already plaintext). Returns an
// error if decryption fails (wrong key, truncated blob, corrupted ciphertext).
func DecryptField(s string, key [32]byte) (string, error) {
	if !IsEncrypted(s) {
		return s, nil
	}
	encoded := strings.TrimPrefix(s, EncryptedPrefix)
	blob, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("configcrypt: base64 decode: %w", err)
	}
	aead, err := ActiveProfile.AEADNew(key)
	if err != nil {
		return "", fmt.Errorf("configcrypt: create AEAD: %w", err)
	}
	ns := aead.NonceSize()
	if len(blob) < ns {
		return "", fmt.Errorf("configcrypt: ciphertext too short (%d bytes, need >%d)", len(blob), ns)
	}
	plain, err := aead.Open(nil, blob[:ns], blob[ns:], nil)
	if err != nil {
		return "", fmt.Errorf("configcrypt: decrypt: %w", err)
	}
	return string(plain), nil
}

// IsEncrypted reports whether s was encrypted by EncryptField (has ENC:v1: prefix).
func IsEncrypted(s string) bool {
	return strings.HasPrefix(s, EncryptedPrefix)
}

// IsSensitiveFieldName reports whether a TOML field name should be considered
// sensitive and eligible for encryption. Returns true if the lowercased name
// contains any of: key, secret, password, token.
func IsSensitiveFieldName(name string) bool {
	lower := strings.ToLower(name)
	for _, kw := range []string{"key", "secret", "password", "token"} {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}
