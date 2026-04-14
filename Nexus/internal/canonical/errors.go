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

package canonical

import "errors"

var (
	// ErrDisabled is returned when canonicalization is not enabled.
	ErrDisabled = errors.New("canonical: disabled")

	// ErrInvalidCanonicalDim is returned when canonical_dim is out of range [64, 8192].
	ErrInvalidCanonicalDim = errors.New("canonical: canonical_dim must be between 64 and 8192")

	// ErrCanonicalDimNotPowerOfTwo is returned when canonical_dim is not a power of 2.
	ErrCanonicalDimNotPowerOfTwo = errors.New("canonical: canonical_dim must be a power of 2")

	// ErrWhiteningWarmupTooSmall is returned when whitening_warmup < 100.
	ErrWhiteningWarmupTooSmall = errors.New("canonical: whitening_warmup must be >= 100")

	// ErrZeroVector is returned when a zero-norm vector is passed to canonicalization.
	ErrZeroVector = errors.New("canonical: zero input vector")
)
