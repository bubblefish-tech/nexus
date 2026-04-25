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

package nexuserr

import (
	"errors"
	"fmt"
	"testing"
)

func TestSentinelsAreDistinct(t *testing.T) {
	t.Helper()
	sentinels := []error{
		ErrDuplicateKey, ErrConnectionRefused, ErrTimeout, ErrCircuitOpen,
		ErrNotFound, ErrPermissionDenied, ErrQuarantined, ErrCapacityExceeded,
	}
	for i, a := range sentinels {
		for j, b := range sentinels {
			if i != j && errors.Is(a, b) {
				t.Errorf("sentinel %d (%v) matches sentinel %d (%v)", i, a, j, b)
			}
		}
	}
}

func TestWrappedDuplicateKey_MatchesViaErrorsIs(t *testing.T) {
	t.Helper()
	wrapped := fmt.Errorf("sqlite: %w: UNIQUE constraint failed", ErrDuplicateKey)
	if !errors.Is(wrapped, ErrDuplicateKey) {
		t.Error("wrapped ErrDuplicateKey should match via errors.Is")
	}
}

func TestWrappedConnectionRefused_MatchesViaErrorsIs(t *testing.T) {
	t.Helper()
	wrapped := fmt.Errorf("postgres: %w: dial tcp refused", ErrConnectionRefused)
	if !errors.Is(wrapped, ErrConnectionRefused) {
		t.Error("wrapped ErrConnectionRefused should match via errors.Is")
	}
}

func TestIsInfrastructureError_AppLevelErrorsNotCounted(t *testing.T) {
	t.Helper()
	cases := []struct {
		err  error
		want bool
	}{
		{ErrDuplicateKey, false},
		{ErrNotFound, false},
		{ErrQuarantined, false},
		{ErrConnectionRefused, true},
		{ErrTimeout, true},
		{ErrCircuitOpen, true},
		{nil, false},
	}
	for _, tc := range cases {
		got := IsInfrastructureError(tc.err)
		if got != tc.want {
			t.Errorf("IsInfrastructureError(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}
