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

package daemon

import (
	"fmt"
	"path/filepath"

	"github.com/gofrs/flock"
)

// AcquireLock acquires an exclusive file lock on <configDir>/nexus.lock.
// Returns the lock (caller must defer Release) or error if another instance holds it.
func AcquireLock(configDir string) (*flock.Flock, error) {
	lockPath := filepath.Join(configDir, "nexus.lock")
	fl := flock.New(lockPath)
	locked, err := fl.TryLock()
	if err != nil {
		return nil, fmt.Errorf("acquire lock on %s: %w", lockPath, err)
	}
	if !locked {
		return nil, fmt.Errorf("nexus is already running (lock held on %s)", lockPath)
	}
	return fl, nil
}
