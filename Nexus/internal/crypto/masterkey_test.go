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
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/bubblefish-tech/nexus/internal/crypto"
)

func saltPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "crypto.salt")
}

func TestMasterKeyManager_Disabled_NoPassword(t *testing.T) {
	t.Helper()
	// Ensure env var is unset so disabled path is exercised.
	t.Setenv(crypto.EnvPassword, "")

	mgr, err := crypto.NewMasterKeyManager("", saltPath(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mgr.IsEnabled() {
		t.Fatal("expected IsEnabled() = false when no password provided")
	}
}

func TestMasterKeyManager_Enabled_ExplicitPassword(t *testing.T) {
	t.Helper()
	mgr, err := crypto.NewMasterKeyManager("correct-horse-battery-staple", saltPath(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !mgr.IsEnabled() {
		t.Fatal("expected IsEnabled() = true when password provided")
	}
}

func TestMasterKeyManager_EnvVarOverride(t *testing.T) {
	t.Helper()
	t.Setenv(crypto.EnvPassword, "env-password-secret")

	mgr, err := crypto.NewMasterKeyManager("", saltPath(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !mgr.IsEnabled() {
		t.Fatal("expected IsEnabled() = true when NEXUS_PASSWORD env var is set")
	}
}

func TestMasterKeyManager_RederiveSamePassword_SameKeys(t *testing.T) {
	t.Helper()
	sp := saltPath(t)
	const pw = "my-stable-password"

	mgr1, err := crypto.NewMasterKeyManager(pw, sp)
	if err != nil {
		t.Fatalf("first derive: %v", err)
	}

	mgr2, err := crypto.NewMasterKeyManager(pw, sp)
	if err != nil {
		t.Fatalf("second derive: %v", err)
	}

	for _, domain := range []string{
		"nexus-config-key-v1",
		"nexus-memory-key-v1",
		"nexus-audit-key-v1",
		"nexus-control-key-v1",
	} {
		k1 := mgr1.SubKey(domain)
		k2 := mgr2.SubKey(domain)
		if k1 != k2 {
			t.Errorf("domain %q: keys differ after re-derive with same password+salt", domain)
		}
	}
}

func TestMasterKeyManager_WrongPassword_DifferentKeys(t *testing.T) {
	t.Helper()
	sp := saltPath(t)

	mgr1, err := crypto.NewMasterKeyManager("password-A", sp)
	if err != nil {
		t.Fatalf("mgr1: %v", err)
	}

	mgr2, err := crypto.NewMasterKeyManager("password-B", sp)
	if err != nil {
		t.Fatalf("mgr2: %v", err)
	}

	const domain = "nexus-memory-key-v1"
	if mgr1.SubKey(domain) == mgr2.SubKey(domain) {
		t.Error("different passwords produced identical sub-key — collision is astronomically unlikely; something is wrong")
	}
}

func TestMasterKeyManager_SaltPersistence(t *testing.T) {
	t.Helper()
	sp := saltPath(t)
	const pw = "persistence-test"

	_, err := crypto.NewMasterKeyManager(pw, sp)
	if err != nil {
		t.Fatalf("first init: %v", err)
	}

	data, err := os.ReadFile(sp)
	if err != nil {
		t.Fatalf("read salt file: %v", err)
	}
	if len(data) != 32 {
		t.Fatalf("salt file length: got %d, want 32", len(data))
	}
}

func TestMasterKeyManager_SaltFilePermissions(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not enforce Unix file permission bits via os.WriteFile")
	}
	sp := saltPath(t)

	_, err := crypto.NewMasterKeyManager("perm-test", sp)
	if err != nil {
		t.Fatalf("init: %v", err)
	}

	info, err := os.Stat(sp)
	if err != nil {
		t.Fatalf("stat salt file: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0600 {
		t.Errorf("salt file mode: got %o, want 0600", mode)
	}
}

func TestMasterKeyManager_AllDomainSubKeysNonZero(t *testing.T) {
	t.Helper()
	mgr, err := crypto.NewMasterKeyManager("non-zero-test", saltPath(t))
	if err != nil {
		t.Fatalf("init: %v", err)
	}

	var zero [32]byte
	for _, domain := range []string{
		"nexus-config-key-v1",
		"nexus-memory-key-v1",
		"nexus-audit-key-v1",
		"nexus-control-key-v1",
	} {
		k := mgr.SubKey(domain)
		if k == zero {
			t.Errorf("domain %q: sub-key is all-zeros", domain)
		}
	}
}

func TestMasterKeyManager_DomainSubKeysDistinct(t *testing.T) {
	t.Helper()
	mgr, err := crypto.NewMasterKeyManager("distinct-domains", saltPath(t))
	if err != nil {
		t.Fatalf("init: %v", err)
	}

	domains := []string{
		"nexus-config-key-v1",
		"nexus-memory-key-v1",
		"nexus-audit-key-v1",
		"nexus-control-key-v1",
	}
	seen := make(map[[32]byte]string)
	for _, d := range domains {
		k := mgr.SubKey(d)
		if prev, ok := seen[k]; ok {
			t.Errorf("domain %q and domain %q produced the same sub-key", d, prev)
		}
		seen[k] = d
	}
}

func TestMasterKeyManager_NewSaltEachInstall(t *testing.T) {
	t.Helper()
	sp1 := filepath.Join(t.TempDir(), "salt1")
	sp2 := filepath.Join(t.TempDir(), "salt2")
	const pw = "same-password"

	mgr1, err := crypto.NewMasterKeyManager(pw, sp1)
	if err != nil {
		t.Fatalf("mgr1: %v", err)
	}
	mgr2, err := crypto.NewMasterKeyManager(pw, sp2)
	if err != nil {
		t.Fatalf("mgr2: %v", err)
	}

	const domain = "nexus-memory-key-v1"
	if mgr1.SubKey(domain) == mgr2.SubKey(domain) {
		t.Error("same password + different salts produced same sub-key — salt is not contributing to key derivation")
	}
}

func TestMasterKeyManager_InvalidSaltFile(t *testing.T) {
	t.Helper()
	sp := saltPath(t)
	if err := os.WriteFile(sp, []byte("tooshort"), 0600); err != nil {
		t.Fatalf("write bad salt: %v", err)
	}

	_, err := crypto.NewMasterKeyManager("password", sp)
	if err == nil {
		t.Fatal("expected error for invalid salt file length, got nil")
	}
}

func TestMasterKeyManager_SubKey_UnknownDomain_ReturnsZero(t *testing.T) {
	t.Helper()
	mgr, err := crypto.NewMasterKeyManager("test-pw", saltPath(t))
	if err != nil {
		t.Fatalf("init: %v", err)
	}

	var zero [32]byte
	k := mgr.SubKey("unknown-domain-v9999")
	if k != zero {
		t.Error("expected zero key for unknown domain")
	}
}

func TestMasterKeyManager_DisabledSubKey_ReturnsZero(t *testing.T) {
	t.Helper()
	t.Setenv(crypto.EnvPassword, "")
	mgr, err := crypto.NewMasterKeyManager("", saltPath(t))
	if err != nil {
		t.Fatalf("init: %v", err)
	}

	var zero [32]byte
	for _, d := range []string{"nexus-config-key-v1", "nexus-memory-key-v1"} {
		if mgr.SubKey(d) != zero {
			t.Errorf("disabled manager: SubKey(%q) should be zero", d)
		}
	}
}
