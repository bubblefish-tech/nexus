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

// Package fsutil provides filesystem utilities that abstract over OS-specific
// behaviour differences, particularly Windows mandatory file locking.
package fsutil

import (
	"os"
	"time"
)

// RobustRename calls os.Rename with retry on transient Windows file-locking
// errors (ERROR_ACCESS_DENIED, ERROR_SHARING_VIOLATION). On POSIX systems
// rename never fails from transient locks, so retries add zero overhead in the
// success path and fail immediately on the first error.
//
// The retry budget is ~155 ms (5 attempts: 5, 10, 20, 40, 80 ms). This gives
// external processes (Windows Defender, Search Indexer) time to release handles
// opened for scanning after a file write/close.
//
// Callers MUST fsync and close the source file before calling RobustRename.
func RobustRename(oldpath, newpath string) error {
	const maxAttempts = 5
	delay := 5 * time.Millisecond

	var err error
	for i := 0; i < maxAttempts; i++ {
		err = os.Rename(oldpath, newpath)
		if err == nil {
			return nil
		}
		if !isRetryableRenameErr(err) {
			return err
		}
		time.Sleep(delay)
		delay *= 2
	}
	return err
}
