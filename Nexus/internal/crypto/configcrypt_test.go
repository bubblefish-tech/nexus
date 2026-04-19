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

package crypto_test

import (
	"strings"
	"testing"

	"github.com/bubblefish-tech/nexus/internal/crypto"
)

var testKey = [32]byte{
	1, 2, 3, 4, 5, 6, 7, 8,
	9, 10, 11, 12, 13, 14, 15, 16,
	17, 18, 19, 20, 21, 22, 23, 24,
	25, 26, 27, 28, 29, 30, 31, 32,
}

func TestEncryptField_RoundTrip(t *testing.T) {
	t.Helper()
	cases := []struct{ name, plaintext string }{
		{"api key", "bfn_admin_abc123"},
		{"long value", strings.Repeat("x", 200)},
		{"special chars", "pass!w0rd@#$%^&*()"},
		{"unicode", "пароль123"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			enc, err := crypto.EncryptField(tc.plaintext, testKey)
			if err != nil {
				t.Fatalf("EncryptField: %v", err)
			}
			if !crypto.IsEncrypted(enc) {
				t.Fatal("expected ENC:v1: prefix")
			}
			got, err := crypto.DecryptField(enc, testKey)
			if err != nil {
				t.Fatalf("DecryptField: %v", err)
			}
			if got != tc.plaintext {
				t.Errorf("got %q, want %q", got, tc.plaintext)
			}
		})
	}
}

func TestEncryptField_AlreadyEncrypted(t *testing.T) {
	t.Helper()
	enc, err := crypto.EncryptField("secret", testKey)
	if err != nil {
		t.Fatalf("first encrypt: %v", err)
	}
	enc2, err := crypto.EncryptField(enc, testKey)
	if err != nil {
		t.Fatalf("second encrypt: %v", err)
	}
	if enc != enc2 {
		t.Error("encrypting already-encrypted value should return unchanged")
	}
}

func TestDecryptField_Plaintext(t *testing.T) {
	t.Helper()
	plain := "not-encrypted-value"
	got, err := crypto.DecryptField(plain, testKey)
	if err != nil {
		t.Fatalf("DecryptField on plaintext: %v", err)
	}
	if got != plain {
		t.Errorf("got %q, want %q", got, plain)
	}
}

func TestDecryptField_WrongKey(t *testing.T) {
	t.Helper()
	enc, err := crypto.EncryptField("secret-value", testKey)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	var wrongKey [32]byte
	_, err = crypto.DecryptField(enc, wrongKey)
	if err == nil {
		t.Fatal("expected error with wrong key, got nil")
	}
}

func TestDecryptField_TruncatedBlob(t *testing.T) {
	t.Helper()
	// Construct a value with ENC:v1: prefix but a blob that is too short.
	import64 := "AAAA" // only 3 bytes decoded — less than 12-byte nonce
	s := crypto.EncryptedPrefix + import64
	_, err := crypto.DecryptField(s, testKey)
	if err == nil {
		t.Fatal("expected error for truncated blob")
	}
}

func TestDecryptField_InvalidBase64(t *testing.T) {
	t.Helper()
	s := crypto.EncryptedPrefix + "not!valid!base64!!!"
	_, err := crypto.DecryptField(s, testKey)
	if err == nil {
		t.Fatal("expected error for invalid base64")
	}
}

func TestDecryptField_CorruptedCiphertext(t *testing.T) {
	t.Helper()
	enc, err := crypto.EncryptField("original", testKey)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	// Flip the last byte of the base64-encoded blob.
	payload := strings.TrimPrefix(enc, crypto.EncryptedPrefix)
	b := []byte(payload)
	b[len(b)-2] ^= 0xFF // corrupt near the end
	corrupted := crypto.EncryptedPrefix + string(b)
	_, err = crypto.DecryptField(corrupted, testKey)
	if err == nil {
		t.Fatal("expected error for corrupted ciphertext")
	}
}

func TestIsEncrypted(t *testing.T) {
	t.Helper()
	cases := []struct {
		s    string
		want bool
	}{
		{"ENC:v1:AAAA", true},
		{"plaintext", false},
		{"", false},
		{"ENC:v2:AAAA", false},
		{"enc:v1:AAAA", false}, // case-sensitive
	}
	for _, tc := range cases {
		if got := crypto.IsEncrypted(tc.s); got != tc.want {
			t.Errorf("IsEncrypted(%q) = %v, want %v", tc.s, got, tc.want)
		}
	}
}

func TestIsSensitiveFieldName(t *testing.T) {
	t.Helper()
	cases := []struct {
		name string
		want bool
	}{
		{"api_key", true},
		{"admin_token", true},
		{"password", true},
		{"db_secret", true},
		{"mac_key_file", true},
		{"jwt_secret_key", true},
		{"port", false},
		{"bind", false},
		{"log_level", false},
		{"enabled", false},
		{"API_KEY", true}, // case-insensitive
	}
	for _, tc := range cases {
		if got := crypto.IsSensitiveFieldName(tc.name); got != tc.want {
			t.Errorf("IsSensitiveFieldName(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestEncryptField_DifferentNonces(t *testing.T) {
	t.Helper()
	enc1, err := crypto.EncryptField("same-plaintext", testKey)
	if err != nil {
		t.Fatalf("first encrypt: %v", err)
	}
	enc2, err := crypto.EncryptField("same-plaintext", testKey)
	if err != nil {
		t.Fatalf("second encrypt: %v", err)
	}
	if enc1 == enc2 {
		t.Error("two encryptions of the same plaintext should differ (random nonce)")
	}
	// But both must decrypt to the same value.
	got1, _ := crypto.DecryptField(enc1, testKey)
	got2, _ := crypto.DecryptField(enc2, testKey)
	if got1 != "same-plaintext" || got2 != "same-plaintext" {
		t.Errorf("decrypted: %q, %q; want same-plaintext", got1, got2)
	}
}

func TestEncryptField_EmptyPlaintext(t *testing.T) {
	t.Helper()
	enc, err := crypto.EncryptField("", testKey)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	got, err := crypto.DecryptField(enc, testKey)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}
