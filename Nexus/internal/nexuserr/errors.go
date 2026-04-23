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

import "errors"

var (
	ErrDuplicateKey      = errors.New("duplicate key")
	ErrConnectionRefused = errors.New("connection refused")
	ErrTimeout           = errors.New("timeout")
	ErrCircuitOpen       = errors.New("circuit breaker open")
	ErrNotFound          = errors.New("not found")
	ErrPermissionDenied  = errors.New("permission denied")
	ErrQuarantined       = errors.New("content quarantined")
	ErrCapacityExceeded  = errors.New("capacity exceeded")
)

// IsInfrastructureError returns true if the error represents a real
// infrastructure failure (not an application-level error). Circuit breakers
// should only trip on infrastructure errors.
func IsInfrastructureError(err error) bool {
	return err != nil &&
		!errors.Is(err, ErrDuplicateKey) &&
		!errors.Is(err, ErrNotFound) &&
		!errors.Is(err, ErrQuarantined)
}
