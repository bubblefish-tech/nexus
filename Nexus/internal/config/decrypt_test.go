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

package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bubblefish-tech/nexus/internal/config"
	"github.com/bubblefish-tech/nexus/internal/crypto"
)

// buildMinimalConfigDir creates a minimal config directory suitable for
// LoadWithKey tests. admin_token may be plaintext or ENC:v1: encrypted.
func buildMinimalConfigDir(t *testing.T, adminToken string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sources"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "destinations"), 0700); err != nil {
		t.Fatal(err)
	}
	daemonTOML := "[daemon]\nadmin_token = \"" + adminToken + "\"\n"
	if err := os.WriteFile(filepath.Join(dir, "daemon.toml"), []byte(daemonTOML), 0600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLoadWithKey_NilMKM(t *testing.T) {
	t.Helper()
	dir := buildMinimalConfigDir(t, "bfn_admin_plaintoken")
	cfg, err := config.LoadWithKey(dir, nil, nil)
	if err != nil {
		t.Fatalf("LoadWithKey(nil mkm): %v", err)
	}
	if string(cfg.ResolvedAdminKey) != "bfn_admin_plaintoken" {
		t.Errorf("got %q, want bfn_admin_plaintoken", cfg.ResolvedAdminKey)
	}
}

func TestLoadWithKey_EncryptedAdminToken(t *testing.T) {
	t.Helper()
	saltPath := filepath.Join(t.TempDir(), "crypto.salt")
	mkm, err := crypto.NewMasterKeyManager("test-password", saltPath)
	if err != nil {
		t.Fatalf("NewMasterKeyManager: %v", err)
	}
	if !mkm.IsEnabled() {
		t.Fatal("expected mkm enabled")
	}

	configKey := mkm.SubKey("nexus-config-key-v1")
	encrypted, err := crypto.EncryptField("bfn_admin_secrettoken", configKey)
	if err != nil {
		t.Fatalf("EncryptField: %v", err)
	}

	dir := buildMinimalConfigDir(t, encrypted)
	cfg, err := config.LoadWithKey(dir, nil, mkm)
	if err != nil {
		t.Fatalf("LoadWithKey: %v", err)
	}
	if string(cfg.ResolvedAdminKey) != "bfn_admin_secrettoken" {
		t.Errorf("got %q, want bfn_admin_secrettoken", cfg.ResolvedAdminKey)
	}
}

func TestLoadWithKey_WrongKeyFails(t *testing.T) {
	t.Helper()
	saltPath1 := filepath.Join(t.TempDir(), "crypto.salt")
	mkmEncrypt, err := crypto.NewMasterKeyManager("password-A", saltPath1)
	if err != nil {
		t.Fatalf("NewMasterKeyManager A: %v", err)
	}
	encKey := mkmEncrypt.SubKey("nexus-config-key-v1")
	encrypted, err := crypto.EncryptField("bfn_admin_secrettoken", encKey)
	if err != nil {
		t.Fatalf("EncryptField: %v", err)
	}

	dir := buildMinimalConfigDir(t, encrypted)

	// Attempt to load with a different password — different sub-key, must fail.
	saltPath2 := filepath.Join(t.TempDir(), "crypto.salt")
	mkmWrong, err := crypto.NewMasterKeyManager("password-B", saltPath2)
	if err != nil {
		t.Fatalf("NewMasterKeyManager B: %v", err)
	}
	_, err = config.LoadWithKey(dir, nil, mkmWrong)
	if err == nil {
		t.Fatal("expected error when loading with wrong key, got nil")
	}
}
