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

//go:build windows

package fsutil

import (
	"errors"
	"os"
	"syscall"
)

// errSharingViolation is Windows ERROR_SHARING_VIOLATION (errno 32).
// Not exported by Go's syscall package, so defined here as a raw Errno.
const errSharingViolation = syscall.Errno(32)

// isRetryableRenameErr returns true if the error is a transient Windows
// file-locking error that may resolve if retried after a short delay.
//
// Windows Defender, Search Indexer, and backup software can briefly hold
// handles open on files that were just written/closed, causing os.Rename
// to fail with ERROR_ACCESS_DENIED (5) or ERROR_SHARING_VIOLATION (32).
func isRetryableRenameErr(err error) bool {
	var linkErr *os.LinkError
	if !errors.As(err, &linkErr) {
		return false
	}
	var errno syscall.Errno
	if !errors.As(linkErr.Err, &errno) {
		return false
	}
	return errno == syscall.ERROR_ACCESS_DENIED || errno == errSharingViolation
}
