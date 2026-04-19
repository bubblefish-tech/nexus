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

package secrets_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bubblefish-tech/nexus/internal/secrets"
)

func TestOpen_CreatesDirectory(t *testing.T) {
	t.Helper()
	base := t.TempDir()
	d, err := secrets.Open(base)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	want := filepath.Join(base, "secrets")
	if d.Path() != want {
		t.Errorf("Path() = %q, want %q", d.Path(), want)
	}
	info, err := os.Stat(want)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected directory")
	}
}

func TestOpen_Idempotent(t *testing.T) {
	t.Helper()
	base := t.TempDir()
	for i := 0; i < 3; i++ {
		if _, err := secrets.Open(base); err != nil {
			t.Fatalf("Open[%d]: %v", i, err)
		}
	}
}

func TestLoadOrGenerateLSHSeed_GeneratesAndPersists(t *testing.T) {
	t.Helper()
	base := t.TempDir()
	d, err := secrets.Open(base)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	seed1, err := d.LoadOrGenerateLSHSeed(0)
	if err != nil {
		t.Fatalf("first generate: %v", err)
	}
	if len(seed1) != 32 {
		t.Errorf("seed length = %d, want 32", len(seed1))
	}

	// Second call must return the same seed.
	seed2, err := d.LoadOrGenerateLSHSeed(0)
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	if string(seed1) != string(seed2) {
		t.Error("seed changed between calls")
	}
}

func TestLoadOrGenerateLSHSeed_PerTierDistinct(t *testing.T) {
	t.Helper()
	base := t.TempDir()
	d, err := secrets.Open(base)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	seen := map[string]int{}
	for tier := 0; tier <= 3; tier++ {
		s, err := d.LoadOrGenerateLSHSeed(tier)
		if err != nil {
			t.Fatalf("tier %d: %v", tier, err)
		}
		key := string(s)
		if prev, ok := seen[key]; ok {
			t.Errorf("tier %d and tier %d produced the same seed", tier, prev)
		}
		seen[key] = tier
	}
}

func TestLoadOrGenerateLSHSeed_InvalidTier(t *testing.T) {
	t.Helper()
	base := t.TempDir()
	d, err := secrets.Open(base)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	for _, tier := range []int{-1, 4, 100} {
		if _, err := d.LoadOrGenerateLSHSeed(tier); err == nil {
			t.Errorf("tier %d: expected error, got nil", tier)
		}
	}
}

func TestWriteReadSecret(t *testing.T) {
	t.Helper()
	base := t.TempDir()
	d, err := secrets.Open(base)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	data := []byte("super-secret-bytes")
	if err := d.WriteSecret("test.key", data); err != nil {
		t.Fatalf("WriteSecret: %v", err)
	}
	got, err := d.ReadSecret("test.key")
	if err != nil {
		t.Fatalf("ReadSecret: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("got %q, want %q", got, data)
	}
}

func TestWriteSecret_RejectsPathSeparator(t *testing.T) {
	t.Helper()
	base := t.TempDir()
	d, err := secrets.Open(base)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := d.WriteSecret("../escape", []byte("x")); err == nil {
		t.Error("expected error for path traversal, got nil")
	}
}

func TestReadSecret_MissingReturnsNotExist(t *testing.T) {
	t.Helper()
	base := t.TempDir()
	d, err := secrets.Open(base)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	_, err = d.ReadSecret("nothere.key")
	if !os.IsNotExist(err) {
		t.Errorf("expected os.ErrNotExist, got %v", err)
	}
}

func TestLoadOrGenerateAllLSHSeeds(t *testing.T) {
	t.Helper()
	base := t.TempDir()
	d, err := secrets.Open(base)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	seeds, err := d.LoadOrGenerateAllLSHSeeds()
	if err != nil {
		t.Fatalf("LoadOrGenerateAllLSHSeeds: %v", err)
	}
	// Ensure all 4 seeds are present and non-empty.
	for tier, s := range seeds {
		if len(s) != 32 {
			t.Errorf("tier %d: seed length %d, want 32", tier, len(s))
		}
	}
}
