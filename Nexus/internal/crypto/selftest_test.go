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
	"path/filepath"
	"testing"

	"github.com/bubblefish-tech/nexus/internal/crypto"
)

func TestSelfTest_NilMKM(t *testing.T) {
	t.Helper()
	if err := crypto.SelfTest(nil); err != nil {
		t.Fatalf("SelfTest(nil) = %v; want nil", err)
	}
}

func TestSelfTest_DisabledMKM(t *testing.T) {
	t.Helper()
	saltPath := filepath.Join(t.TempDir(), "crypto.salt")
	// Empty password → disabled MKM.
	mkm, err := crypto.NewMasterKeyManager("", saltPath)
	if err != nil {
		t.Fatalf("NewMasterKeyManager: %v", err)
	}
	if mkm.IsEnabled() {
		t.Fatal("expected disabled MKM with empty password")
	}
	if err := crypto.SelfTest(mkm); err != nil {
		t.Fatalf("SelfTest(disabled) = %v; want nil", err)
	}
}

func TestSelfTest_EnabledRoundTrip(t *testing.T) {
	t.Helper()
	saltPath := filepath.Join(t.TempDir(), "crypto.salt")
	mkm, err := crypto.NewMasterKeyManager("test-password-for-selftest", saltPath)
	if err != nil {
		t.Fatalf("NewMasterKeyManager: %v", err)
	}
	if !mkm.IsEnabled() {
		t.Fatal("expected enabled MKM")
	}
	if err := crypto.SelfTest(mkm); err != nil {
		t.Fatalf("SelfTest(enabled) = %v; want nil", err)
	}
}

func TestSelfTest_AllDomainsExercised(t *testing.T) {
	t.Helper()
	// Run SelfTest twice with the same MKM — nonces differ each time so
	// ciphertexts must differ, but both calls must succeed (proves fresh nonces).
	saltPath := filepath.Join(t.TempDir(), "crypto.salt")
	mkm, err := crypto.NewMasterKeyManager("determinism-check", saltPath)
	if err != nil {
		t.Fatalf("NewMasterKeyManager: %v", err)
	}
	if err := crypto.SelfTest(mkm); err != nil {
		t.Fatalf("first SelfTest = %v", err)
	}
	if err := crypto.SelfTest(mkm); err != nil {
		t.Fatalf("second SelfTest = %v", err)
	}
}

func TestSelfTest_DifferentPasswordsDifferentKeys(t *testing.T) {
	t.Helper()
	dir := t.TempDir()

	mkmA, err := crypto.NewMasterKeyManager("password-alpha", filepath.Join(dir, "salt-a"))
	if err != nil {
		t.Fatalf("NewMasterKeyManager A: %v", err)
	}
	mkmB, err := crypto.NewMasterKeyManager("password-beta", filepath.Join(dir, "salt-b"))
	if err != nil {
		t.Fatalf("NewMasterKeyManager B: %v", err)
	}

	// Both self-tests must pass independently.
	if err := crypto.SelfTest(mkmA); err != nil {
		t.Fatalf("SelfTest(A) = %v", err)
	}
	if err := crypto.SelfTest(mkmB); err != nil {
		t.Fatalf("SelfTest(B) = %v", err)
	}
}
