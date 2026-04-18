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
	"bytes"
	"crypto/rand"
	"errors"
	"sync"
	"testing"
)

// ─── HKDF Key Derivation Tests ──────────────────────────────────────────────

func TestDeriveEmbeddingKeyDeterminism(t *testing.T) {
	state := [32]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16,
		17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32}

	k1, err := DeriveEmbeddingKey(state, "memory-001")
	if err != nil {
		t.Fatal(err)
	}
	k2, err := DeriveEmbeddingKey(state, "memory-001")
	if err != nil {
		t.Fatal(err)
	}
	if k1 != k2 {
		t.Fatal("same inputs should produce same key")
	}
}

func TestDeriveEmbeddingKeyDifferentMemoryIDs(t *testing.T) {
	state := [32]byte{42}
	k1, _ := DeriveEmbeddingKey(state, "memory-001")
	k2, _ := DeriveEmbeddingKey(state, "memory-002")
	if k1 == k2 {
		t.Fatal("different memory IDs should produce different keys")
	}
}

func TestDeriveEmbeddingKeyDifferentStates(t *testing.T) {
	k1, _ := DeriveEmbeddingKey([32]byte{1}, "mem-1")
	k2, _ := DeriveEmbeddingKey([32]byte{2}, "mem-1")
	if k1 == k2 {
		t.Fatal("different states should produce different keys")
	}
}

func TestDeriveEmbeddingKeyEmptyID(t *testing.T) {
	_, err := DeriveEmbeddingKey([32]byte{1}, "")
	if err == nil {
		t.Fatal("empty memory ID should return error")
	}
}

func TestDeriveEmbeddingKeyNonZero(t *testing.T) {
	key, _ := DeriveEmbeddingKey([32]byte{99}, "test")
	allZero := true
	for _, b := range key {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Fatal("derived key should not be all zeros")
	}
}

func TestZeroizeKey(t *testing.T) {
	key := [32]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16,
		17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32}
	ZeroizeKey(&key)
	for i, b := range key {
		if b != 0 {
			t.Fatalf("key[%d] not zeroed: %d", i, b)
		}
	}
}

// ─── AES-GCM Encryption Tests ──────────────────────────────────────────────

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key, _ := DeriveEmbeddingKey([32]byte{42}, "test-memory")
	plaintext := []byte("hello world embedding data 1234567890")

	enc, err := EncryptEmbedding(key, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if len(enc.Nonce) != 12 {
		t.Fatalf("expected 12-byte nonce, got %d", len(enc.Nonce))
	}
	// Ciphertext should be longer than plaintext (includes 16-byte GCM tag)
	if len(enc.Ciphertext) != len(plaintext)+16 {
		t.Fatalf("ciphertext should be plaintext+16, got %d (plaintext=%d)", len(enc.Ciphertext), len(plaintext))
	}

	decrypted, err := DecryptEmbedding(key, enc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decrypted, plaintext) {
		t.Fatal("decrypted does not match plaintext")
	}
}

func TestEncryptDecryptEmptyPlaintext(t *testing.T) {
	key, _ := DeriveEmbeddingKey([32]byte{1}, "empty-test")
	enc, err := EncryptEmbedding(key, []byte{})
	if err != nil {
		t.Fatal(err)
	}
	decrypted, err := DecryptEmbedding(key, enc)
	if err != nil {
		t.Fatal(err)
	}
	if len(decrypted) != 0 {
		t.Fatalf("expected empty plaintext, got %d bytes", len(decrypted))
	}
}

func TestEncryptDecryptLargePlaintext(t *testing.T) {
	key, _ := DeriveEmbeddingKey([32]byte{77}, "large-test")
	// 10 KB — typical embedding size (1024 × 8 bytes float64 + overhead)
	plaintext := make([]byte, 10*1024)
	rand.Read(plaintext)

	enc, err := EncryptEmbedding(key, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	decrypted, err := DecryptEmbedding(key, enc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decrypted, plaintext) {
		t.Fatal("large plaintext round-trip failed")
	}
}

func TestDecryptWrongKey(t *testing.T) {
	key1, _ := DeriveEmbeddingKey([32]byte{1}, "test")
	key2, _ := DeriveEmbeddingKey([32]byte{2}, "test")

	enc, _ := EncryptEmbedding(key1, []byte("secret data"))
	_, err := DecryptEmbedding(key2, enc)
	if !errors.Is(err, ErrEmbeddingUnreachable) {
		t.Fatalf("wrong key should return ErrEmbeddingUnreachable, got %v", err)
	}
}

func TestDecryptTamperedCiphertext(t *testing.T) {
	key, _ := DeriveEmbeddingKey([32]byte{42}, "tamper-test")
	enc, _ := EncryptEmbedding(key, []byte("important data"))

	// Flip a bit in the ciphertext
	enc.Ciphertext[0] ^= 0xFF

	_, err := DecryptEmbedding(key, enc)
	if !errors.Is(err, ErrEmbeddingUnreachable) {
		t.Fatalf("tampered ciphertext should return ErrEmbeddingUnreachable, got %v", err)
	}
}

func TestDecryptTamperedNonce(t *testing.T) {
	key, _ := DeriveEmbeddingKey([32]byte{42}, "nonce-test")
	enc, _ := EncryptEmbedding(key, []byte("some data"))

	// Flip a bit in the nonce
	enc.Nonce[0] ^= 0xFF

	_, err := DecryptEmbedding(key, enc)
	if !errors.Is(err, ErrEmbeddingUnreachable) {
		t.Fatalf("tampered nonce should return ErrEmbeddingUnreachable, got %v", err)
	}
}

func TestDecryptTamperedTag(t *testing.T) {
	key, _ := DeriveEmbeddingKey([32]byte{42}, "tag-test")
	enc, _ := EncryptEmbedding(key, []byte("protected data"))

	// Flip a bit in the last byte (part of the 16-byte GCM tag)
	enc.Ciphertext[len(enc.Ciphertext)-1] ^= 0xFF

	_, err := DecryptEmbedding(key, enc)
	if !errors.Is(err, ErrEmbeddingUnreachable) {
		t.Fatalf("tampered tag should return ErrEmbeddingUnreachable, got %v", err)
	}
}

func TestDecryptNilInput(t *testing.T) {
	key, _ := DeriveEmbeddingKey([32]byte{1}, "nil-test")
	_, err := DecryptEmbedding(key, nil)
	if err == nil {
		t.Fatal("nil input should return error")
	}
}

func TestEncryptDifferentNoncesPerCall(t *testing.T) {
	key, _ := DeriveEmbeddingKey([32]byte{42}, "nonce-unique")
	plaintext := []byte("same plaintext")

	enc1, _ := EncryptEmbedding(key, plaintext)
	enc2, _ := EncryptEmbedding(key, plaintext)

	// Nonces should differ (random)
	if bytes.Equal(enc1.Nonce, enc2.Nonce) {
		t.Fatal("two encryptions should produce different nonces")
	}
	// Ciphertexts should also differ (different nonces → different output)
	if bytes.Equal(enc1.Ciphertext, enc2.Ciphertext) {
		t.Fatal("two encryptions should produce different ciphertexts")
	}

	// But both should decrypt to the same plaintext
	d1, _ := DecryptEmbedding(key, enc1)
	d2, _ := DecryptEmbedding(key, enc2)
	if !bytes.Equal(d1, d2) {
		t.Fatal("both should decrypt to same plaintext")
	}
}

func TestEncryptDecryptConcurrency(t *testing.T) {
	key, _ := DeriveEmbeddingKey([32]byte{99}, "concurrent")
	plaintext := []byte("concurrent encryption test data")

	var wg sync.WaitGroup
	errs := make(chan error, 100)

	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				enc, err := EncryptEmbedding(key, plaintext)
				if err != nil {
					errs <- err
					return
				}
				dec, err := DecryptEmbedding(key, enc)
				if err != nil {
					errs <- err
					return
				}
				if !bytes.Equal(dec, plaintext) {
					errs <- errors.New("round-trip failed")
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent error: %v", err)
	}
}

// ─── Deterministic encryption with fixed nonce ──────────────────────────────

func TestEncryptWithFixedNonceDeterminism(t *testing.T) {
	key, _ := DeriveEmbeddingKey([32]byte{42}, "fixed-nonce")
	nonce := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}
	plaintext := []byte("deterministic test")

	enc1, err := encryptEmbeddingWithNonce(key, plaintext, nonce)
	if err != nil {
		t.Fatal(err)
	}
	enc2, err := encryptEmbeddingWithNonce(key, plaintext, nonce)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(enc1.Ciphertext, enc2.Ciphertext) {
		t.Fatal("same key+nonce+plaintext should produce same ciphertext")
	}

	// Should still decrypt
	dec, err := DecryptEmbedding(key, enc1)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(dec, plaintext) {
		t.Fatal("round-trip with fixed nonce failed")
	}
}

func TestEncryptWithWrongNonceSize(t *testing.T) {
	key, _ := DeriveEmbeddingKey([32]byte{1}, "bad-nonce")
	_, err := encryptEmbeddingWithNonce(key, []byte("test"), []byte{1, 2, 3}) // wrong size
	if err == nil {
		t.Fatal("wrong nonce size should error")
	}
}

// ─── Integration: key derivation → encrypt → shred → decrypt fail ───────────

func TestForwardSecuritySimulation(t *testing.T) {
	// Simulate the forward-security property:
	// 1. Derive key from state A, encrypt
	// 2. Derive key from state B (different), try to decrypt → should fail
	stateA := [32]byte{1}
	stateB := [32]byte{2}
	memoryID := "memory-to-delete"
	plaintext := []byte("sensitive embedding data that will be shredded")

	// Encrypt with state A's key
	keyA, _ := DeriveEmbeddingKey(stateA, memoryID)
	enc, err := EncryptEmbedding(keyA, plaintext)
	if err != nil {
		t.Fatal(err)
	}

	// Verify state A's key can decrypt
	decrypted, err := DecryptEmbedding(keyA, enc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decrypted, plaintext) {
		t.Fatal("state A key should decrypt")
	}

	// Derive key from state B (simulating ratchet advance)
	keyB, _ := DeriveEmbeddingKey(stateB, memoryID)

	// State B's key should NOT decrypt state A's ciphertext
	_, err = DecryptEmbedding(keyB, enc)
	if !errors.Is(err, ErrEmbeddingUnreachable) {
		t.Fatalf("state B key should NOT decrypt state A ciphertext, got: %v", err)
	}

	// Zeroize state A's key (simulating shred)
	ZeroizeKey(&keyA)

	// Verify key A is zeroed
	for _, b := range keyA {
		if b != 0 {
			t.Fatal("key A should be zeroed after ZeroizeKey")
		}
	}

	// Zeroed key should NOT decrypt
	_, err = DecryptEmbedding(keyA, enc)
	if !errors.Is(err, ErrEmbeddingUnreachable) {
		t.Fatalf("zeroed key should NOT decrypt, got: %v", err)
	}
}

// ─── Round-trip with random data (stress test) ──────────────────────────────

func TestEncryptDecryptRandomStress(t *testing.T) {
	state := [32]byte{42}
	for i := 0; i < 100; i++ {
		memID := string(rune('A'+i%26)) + string(rune('0'+i%10))
		key, err := DeriveEmbeddingKey(state, memID)
		if err != nil {
			t.Fatal(err)
		}
		// Random plaintext of varying sizes
		size := 16 + i*100
		plaintext := make([]byte, size)
		rand.Read(plaintext)

		enc, err := EncryptEmbedding(key, plaintext)
		if err != nil {
			t.Fatalf("encrypt #%d: %v", i, err)
		}
		dec, err := DecryptEmbedding(key, enc)
		if err != nil {
			t.Fatalf("decrypt #%d: %v", i, err)
		}
		if !bytes.Equal(dec, plaintext) {
			t.Fatalf("round-trip #%d failed: size=%d", i, size)
		}
	}
}
