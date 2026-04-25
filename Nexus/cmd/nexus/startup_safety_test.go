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
	"crypto/rand"
	"io"
	"math/big"
	"testing"
	"time"
)

func TestEntropyCheck_CompletesQuickly(t *testing.T) {
	t.Helper()
	t0 := time.Now()
	buf := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		t.Fatalf("crypto/rand failed: %v", err)
	}
	d := time.Since(t0)
	if d > time.Second {
		t.Errorf("entropy check too slow: %v (want <1s)", d)
	}
}

func TestStartupJitter_InRange(t *testing.T) {
	t.Helper()
	const iterations = 100
	for i := 0; i < iterations; i++ {
		n, err := rand.Int(rand.Reader, big_500ms())
		if err != nil {
			t.Fatalf("rand.Int failed: %v", err)
		}
		jitter := time.Duration(n.Int64())
		if jitter < 0 || jitter > 500*time.Millisecond {
			t.Errorf("jitter %v out of range [0, 500ms]", jitter)
		}
	}
}

func big_500ms() *big.Int {
	return big.NewInt(int64(500 * time.Millisecond))
}
