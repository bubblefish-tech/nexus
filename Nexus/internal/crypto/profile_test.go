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
	"bytes"
	"crypto/rand"
	"io"
	"testing"

	nexuscrypto "github.com/bubblefish-tech/nexus/internal/crypto"
)

func profileUnderTest() nexuscrypto.CryptoProfile {
	return nexuscrypto.ActiveProfile
}

func TestHashRoundTrip(t *testing.T) {
	t.Helper()
	p := profileUnderTest()
	h1 := p.HashNew()
	h2 := p.HashNew()
	msg := []byte("hello nexus")
	h1.Write(msg)
	h2.Write(msg)
	if !bytes.Equal(h1.Sum(nil), h2.Sum(nil)) {
		t.Fatal("same input produced different hash outputs")
	}
}

func TestHashSize(t *testing.T) {
	t.Helper()
	p := profileUnderTest()
	h := p.HashNew()
	h.Write([]byte("test"))
	if got := len(h.Sum(nil)); got != p.HashSize() {
		t.Fatalf("HashSize()=%d but actual digest length=%d", p.HashSize(), got)
	}
}

func TestHMACDeterministic(t *testing.T) {
	t.Helper()
	p := profileUnderTest()
	key := []byte("secret-key")
	msg := []byte("hello")
	h1 := p.HMACNew(key)
	h1.Write(msg)
	h2 := p.HMACNew(key)
	h2.Write(msg)
	if !bytes.Equal(h1.Sum(nil), h2.Sum(nil)) {
		t.Fatal("HMAC is not deterministic")
	}
}

func TestHMACKeyedDiffers(t *testing.T) {
	t.Helper()
	p := profileUnderTest()
	msg := []byte("hello")
	h1 := p.HMACNew([]byte("key-a"))
	h1.Write(msg)
	h2 := p.HMACNew([]byte("key-b"))
	h2.Write(msg)
	if bytes.Equal(h1.Sum(nil), h2.Sum(nil)) {
		t.Fatal("different keys produced same HMAC output")
	}
}

func TestHKDFExtractDeterministic(t *testing.T) {
	t.Helper()
	p := profileUnderTest()
	prk1 := p.HKDFExtract([]byte("secret"), []byte("salt"))
	prk2 := p.HKDFExtract([]byte("secret"), []byte("salt"))
	if !bytes.Equal(prk1, prk2) {
		t.Fatal("HKDFExtract is not deterministic")
	}
}

func TestHKDFExpandLength(t *testing.T) {
	t.Helper()
	p := profileUnderTest()
	prk := p.HKDFExtract([]byte("secret"), nil)
	for _, length := range []int{16, 32, 64} {
		out, err := p.HKDFExpand(prk, []byte("info"), length)
		if err != nil {
			t.Fatalf("HKDFExpand(%d): %v", length, err)
		}
		if len(out) != length {
			t.Fatalf("HKDFExpand(%d) returned %d bytes", length, len(out))
		}
	}
}

func TestHKDFExpandDiffers(t *testing.T) {
	t.Helper()
	p := profileUnderTest()
	prk := p.HKDFExtract([]byte("secret"), nil)
	a, _ := p.HKDFExpand(prk, []byte("info-a"), 32)
	b, _ := p.HKDFExpand(prk, []byte("info-b"), 32)
	if bytes.Equal(a, b) {
		t.Fatal("different info labels produced identical HKDF output")
	}
}

func TestAEADRoundTrip(t *testing.T) {
	t.Helper()
	p := profileUnderTest()
	var key [32]byte
	if _, err := io.ReadFull(rand.Reader, key[:]); err != nil {
		t.Fatal(err)
	}
	aead, err := p.AEADNew(key)
	if err != nil {
		t.Fatal(err)
	}
	plaintext := []byte("memory content")
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		t.Fatal(err)
	}
	ciphertext := aead.Seal(nil, nonce, plaintext, nil)
	recovered, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		t.Fatalf("AEAD.Open: %v", err)
	}
	if !bytes.Equal(recovered, plaintext) {
		t.Fatal("decrypted content does not match original")
	}
}

func TestAEADWrongKeyFails(t *testing.T) {
	t.Helper()
	p := profileUnderTest()
	var key1, key2 [32]byte
	if _, err := io.ReadFull(rand.Reader, key1[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(rand.Reader, key2[:]); err != nil {
		t.Fatal(err)
	}
	a1, _ := p.AEADNew(key1)
	a2, _ := p.AEADNew(key2)
	nonce := make([]byte, a1.NonceSize())
	io.ReadFull(rand.Reader, nonce)
	ct := a1.Seal(nil, nonce, []byte("secret"), nil)
	if _, err := a2.Open(nil, nonce, ct, nil); err == nil {
		t.Fatal("expected decryption with wrong key to fail")
	}
}

func TestAEADADDMismatchFails(t *testing.T) {
	t.Helper()
	p := profileUnderTest()
	var key [32]byte
	io.ReadFull(rand.Reader, key[:])
	aead, _ := p.AEADNew(key)
	nonce := make([]byte, aead.NonceSize())
	io.ReadFull(rand.Reader, nonce)
	ct := aead.Seal(nil, nonce, []byte("secret"), []byte("aad-a"))
	if _, err := aead.Open(nil, nonce, ct, []byte("aad-b")); err == nil {
		t.Fatal("expected AAD mismatch to fail authentication")
	}
}

func TestClassicalProfileName(t *testing.T) {
	t.Helper()
	p := profileUnderTest()
	if p.Name() == "" {
		t.Fatal("profile name must not be empty")
	}
}

func TestActiveProfileMatchesClassical(t *testing.T) {
	t.Helper()
	classical := &nexuscrypto.ClassicalProfile{}
	active := nexuscrypto.ActiveProfile
	msg := []byte("same message")
	h1 := classical.HashNew()
	h1.Write(msg)
	h2 := active.HashNew()
	h2.Write(msg)
	if !bytes.Equal(h1.Sum(nil), h2.Sum(nil)) {
		t.Fatal("ActiveProfile does not match ClassicalProfile output")
	}
}
