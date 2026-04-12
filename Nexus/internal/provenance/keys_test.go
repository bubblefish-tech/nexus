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

package provenance

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BubbleFish-Nexus/internal/secrets"
)

func testSecretsDir(t *testing.T) *secrets.Dir {
	t.Helper()
	dir := t.TempDir()
	sd, err := secrets.Open(dir)
	if err != nil {
		t.Fatalf("secrets.Open: %v", err)
	}
	return sd
}

func TestGenerateKeyPair(t *testing.T) {
	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	if len(kp.PublicKey) != ed25519.PublicKeySize {
		t.Errorf("public key size = %d, want %d", len(kp.PublicKey), ed25519.PublicKeySize)
	}
	if len(kp.PrivateKey) != ed25519.PrivateKeySize {
		t.Errorf("private key size = %d, want %d", len(kp.PrivateKey), ed25519.PrivateKeySize)
	}
	if len(kp.KeyID) != fingerprintLen*2 {
		t.Errorf("key ID length = %d, want %d hex chars", len(kp.KeyID), fingerprintLen*2)
	}
}

func TestFingerprint_Deterministic(t *testing.T) {
	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	f1 := Fingerprint(kp.PublicKey)
	f2 := Fingerprint(kp.PublicKey)
	if f1 != f2 {
		t.Errorf("fingerprint not deterministic: %q != %q", f1, f2)
	}
}

func TestFingerprint_Unique(t *testing.T) {
	kp1, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	kp2, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	if kp1.KeyID == kp2.KeyID {
		t.Error("two distinct keys produced the same fingerprint")
	}
}

func TestKeyPairFromSeed(t *testing.T) {
	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	seed := kp.PrivateKey.Seed()

	restored, err := KeyPairFromSeed(seed)
	if err != nil {
		t.Fatalf("KeyPairFromSeed: %v", err)
	}
	if !kp.PublicKey.Equal(restored.PublicKey) {
		t.Error("restored public key does not match original")
	}
	if kp.KeyID != restored.KeyID {
		t.Error("restored key ID does not match original")
	}
}

func TestKeyPairFromSeed_InvalidSize(t *testing.T) {
	cases := []struct {
		name string
		seed []byte
	}{
		{"empty", nil},
		{"too short", make([]byte, 16)},
		{"too long", make([]byte, 64)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := KeyPairFromSeed(tc.seed)
			if err == nil {
				t.Error("expected error for invalid seed size")
			}
		})
	}
}

func TestLoadOrGenerateSourceKey(t *testing.T) {
	sd := testSecretsDir(t)

	// First call generates.
	kp1, err := LoadOrGenerateSourceKey(sd, "agent-a")
	if err != nil {
		t.Fatalf("first LoadOrGenerateSourceKey: %v", err)
	}

	// Second call loads the same key.
	kp2, err := LoadOrGenerateSourceKey(sd, "agent-a")
	if err != nil {
		t.Fatalf("second LoadOrGenerateSourceKey: %v", err)
	}
	if kp1.KeyID != kp2.KeyID {
		t.Errorf("second load returned different key: %q != %q", kp1.KeyID, kp2.KeyID)
	}

	// Different source gets a different key.
	kp3, err := LoadOrGenerateSourceKey(sd, "agent-b")
	if err != nil {
		t.Fatalf("LoadOrGenerateSourceKey agent-b: %v", err)
	}
	if kp1.KeyID == kp3.KeyID {
		t.Error("different sources produced the same key")
	}

	// Verify file exists on disk.
	keyPath := filepath.Join(sd.Path(), "sources", "agent-a.ed25519")
	data, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("read key file: %v", err)
	}
	if len(data) != ed25519SeedSize {
		t.Errorf("key file size = %d, want %d", len(data), ed25519SeedSize)
	}
}

func TestLoadSourceKey_NotFound(t *testing.T) {
	sd := testSecretsDir(t)
	_, err := LoadSourceKey(sd, "nonexistent")
	if err == nil {
		t.Error("expected error for missing key")
	}
}

func TestLoadOrGenerateDaemonKey(t *testing.T) {
	sd := testSecretsDir(t)

	kp1, err := LoadOrGenerateDaemonKey(sd)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	kp2, err := LoadOrGenerateDaemonKey(sd)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if kp1.KeyID != kp2.KeyID {
		t.Error("daemon key changed between calls")
	}
}

func TestRotateSourceKey(t *testing.T) {
	sd := testSecretsDir(t)

	// Generate initial key.
	oldKP, err := LoadOrGenerateSourceKey(sd, "rotate-test")
	if err != nil {
		t.Fatalf("initial key: %v", err)
	}

	// Rotate.
	newKP, err := RotateSourceKey(sd, "rotate-test")
	if err != nil {
		t.Fatalf("RotateSourceKey: %v", err)
	}
	if newKP.KeyID == oldKP.KeyID {
		t.Error("rotated key has same fingerprint as old key")
	}

	// Verify the stored key is now the new one.
	loaded, err := LoadSourceKey(sd, "rotate-test")
	if err != nil {
		t.Fatalf("load after rotate: %v", err)
	}
	if loaded.KeyID != newKP.KeyID {
		t.Error("stored key does not match rotated key")
	}

	// Verify rotation log exists and attestation is valid.
	logPath := filepath.Join(sd.Path(), "sources", "rotate-test.ed25519.rotation-log")
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read rotation log: %v", err)
	}
	parts := strings.SplitN(string(logData), "\n", 2)
	if len(parts) != 2 {
		t.Fatalf("rotation log has unexpected format: %d parts", len(parts))
	}

	var att rotationAttestation
	if err := json.Unmarshal([]byte(parts[0]), &att); err != nil {
		t.Fatalf("unmarshal attestation: %v", err)
	}
	if att.Event != "key_rotation" {
		t.Errorf("attestation event = %q, want %q", att.Event, "key_rotation")
	}
	if att.OldFingerprint != oldKP.KeyID {
		t.Errorf("old fingerprint = %q, want %q", att.OldFingerprint, oldKP.KeyID)
	}
	if att.NewFingerprint != newKP.KeyID {
		t.Errorf("new fingerprint = %q, want %q", att.NewFingerprint, newKP.KeyID)
	}

	// Verify signature.
	sigBytes, err := hex.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode signature hex: %v", err)
	}
	if !ed25519.Verify(newKP.PublicKey, []byte(parts[0]), sigBytes) {
		t.Error("rotation attestation signature invalid")
	}
}

func TestRotateSourceKey_NoExistingKey(t *testing.T) {
	sd := testSecretsDir(t)
	_, err := RotateSourceKey(sd, "no-such-source")
	if err == nil {
		t.Error("expected error when rotating nonexistent key")
	}
}
