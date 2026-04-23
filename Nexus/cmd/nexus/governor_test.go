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

package main

import (
	"os"
	"runtime/debug"
	"testing"
)

func TestSetMemoryGovernor_SetsLimitWhenUnset(t *testing.T) {
	t.Helper()
	orig := os.Getenv("GOMEMLIMIT")
	os.Unsetenv("GOMEMLIMIT")
	defer func() {
		if orig != "" {
			os.Setenv("GOMEMLIMIT", orig)
		}
	}()

	setMemoryGovernor()

	limit := debug.SetMemoryLimit(-1)
	if limit <= 0 {
		t.Error("expected GOMEMLIMIT to be set to a positive value")
	}
}

func TestSetMemoryGovernor_NoOpWhenAlreadySet(t *testing.T) {
	t.Helper()
	os.Setenv("GOMEMLIMIT", "1GiB")
	defer os.Unsetenv("GOMEMLIMIT")

	before := debug.SetMemoryLimit(-1)
	setMemoryGovernor()
	after := debug.SetMemoryLimit(-1)

	if before != after {
		t.Errorf("GOMEMLIMIT changed despite env override: before=%d after=%d", before, after)
	}
}

func TestSetGodebugDefaults_SetsWhenEmpty(t *testing.T) {
	t.Helper()
	orig := os.Getenv("GODEBUG")
	os.Unsetenv("GODEBUG")
	defer func() {
		if orig != "" {
			os.Setenv("GODEBUG", orig)
		}
	}()

	setGodebugDefaults()

	got := os.Getenv("GODEBUG")
	if got != "madvdontneed=1" {
		t.Errorf("GODEBUG = %q, want %q", got, "madvdontneed=1")
	}
}
